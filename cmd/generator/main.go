package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"loadbalancer-k8s/internal/db"
)

// generatorState reproduce el patrón de core/generator.go de proyecto11 /
// proyecto12 (ticker + restartChan + mutex), controlado por una API HTTP.
type generatorState struct {
	mu              sync.RWMutex
	rpm             int
	isRunning       bool
	totalEmitted    int
	restartChan     chan struct{}
	genActiveUntil  time.Time
	lastServerID    string
	lastActiveUntil time.Time
	processingMs    int
}

func newGeneratorState(initialRPM, initialProcessingMs int) *generatorState {
	return &generatorState{
		rpm:          initialRPM,
		processingMs: initialProcessingMs,
		restartChan:  make(chan struct{}, 1),
	}
}

func (g *generatorState) kick() {
	select {
	case g.restartChan <- struct{}{}:
	default:
	}
}

func (g *generatorState) toggle() bool {
	g.mu.Lock()
	g.isRunning = !g.isRunning
	newState := g.isRunning
	g.mu.Unlock()
	g.kick()
	return newState
}

func (g *generatorState) setRPM(rpm int) int {
	if rpm < 10 {
		rpm = 10
	}
	if rpm > 600 {
		rpm = 600
	}
	g.mu.Lock()
	g.rpm = rpm
	g.mu.Unlock()
	g.kick()
	return rpm
}

func (g *generatorState) setProcessingMs(ms int) int {
	if ms < minProcessingMs {
		ms = minProcessingMs
	}
	if ms > maxProcessingMs {
		ms = maxProcessingMs
	}
	g.mu.Lock()
	g.processingMs = ms
	g.mu.Unlock()
	return ms
}

func (g *generatorState) snapshot() (running bool, rpm int, totalEmitted int, generatorActive bool, lastServerID string, serverActive bool, processingMs int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now()
	return g.isRunning, g.rpm, g.totalEmitted, now.Before(g.genActiveUntil), g.lastServerID, g.lastServerID != "" && now.Before(g.lastActiveUntil), g.processingMs
}

var backendURL string

// backendHTTPClient llama al Service "backend". Kubernetes balancea POR
// CONEXIÓN TCP (DNAT en el SYN), no por request HTTP - si este cliente
// reutilizara conexiones (comportamiento por defecto de http.Client), casi
// todas las solicitudes terminarían yendo siempre al mismo Pod (o a 1-2) y
// el balanceo se vería roto aunque el Service esté bien configurado. Por
// eso se fuerza una conexión nueva en cada solicitud.
var backendHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true,
	},
}

// inFlight limita las solicitudes concurrentes hacia el backend. Evita que
// un RPM excesivo amontone goroutines sin control mientras los Pods
// procesan en serie (mismo "descarte por saturación" que proyecto12).
var inFlight = make(chan struct{}, 6)

const (
	minProcessingMs = 100
	maxProcessingMs = 3000
)

func main() {
	backendURL = os.Getenv("BACKEND_URL")
	if backendURL == "" {
		backendURL = "http://backend:8081/process"
	}

	controlPort := os.Getenv("CONTROL_PORT")
	if controlPort == "" {
		controlPort = "9090"
	}

	initialProcessingMs, err := strconv.Atoi(os.Getenv("PROCESSING_MS"))
	if err != nil || initialProcessingMs <= 0 {
		initialProcessingMs = 700
	}

	if err := initK8sClient(); err != nil {
		log.Fatalf("[generator] no se pudo inicializar el cliente de Kubernetes: %v", err)
	}

	if _, err := db.Connect(""); err != nil {
		log.Printf("[generator] advertencia: no se pudo conectar a Postgres en el arranque: %v", err)
	}

	gen := newGeneratorState(120, initialProcessingMs)
	go runLoop(gen)

	mux := http.NewServeMux()
	mux.HandleFunc("/toggle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		running := gen.toggle()
		writeJSON(w, map[string]any{"running": running})
	})
	mux.HandleFunc("/rpm", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Delta int `json:"delta"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, currentRPM, _, _, _, _, _ := gen.snapshot()
		newRPM := gen.setRPM(currentRPM + body.Delta)
		writeJSON(w, map[string]any{"rpm": newRPM})
	})
	mux.HandleFunc("/sleep", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Delta int `json:"delta"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _, _, _, _, _, currentMs := gen.snapshot()
		newMs := gen.setProcessingMs(currentMs + body.Delta)
		broadcastSleepToPods(newMs)
		writeJSON(w, map[string]any{"processingMs": newMs})
	})
	mux.HandleFunc("/scale", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Delta int `json:"delta"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		current, err := getReplicas()
		if err != nil {
			log.Printf("[generator] error leyendo réplicas actuales: %v", err)
			http.Error(w, "error consultando Kubernetes", http.StatusBadGateway)
			return
		}
		newCount, err := setReplicas(current + body.Delta)
		if err != nil {
			log.Printf("[generator] error escalando deployment: %v", err)
			http.Error(w, "error escalando en Kubernetes", http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"replicas": newCount})
	})
	mux.HandleFunc("/pods", func(w http.ResponseWriter, r *http.Request) {
		pods, err := listBackendPods()
		if err != nil {
			log.Printf("[generator] error listando pods: %v", err)
			http.Error(w, "error consultando Kubernetes", http.StatusBadGateway)
			return
		}
		names := make([]string, 0, len(pods))
		for _, p := range pods {
			names = append(names, p.Name)
		}
		sort.Strings(names)
		writeJSON(w, map[string]any{"pods": names})
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		running, rpm, totalEmitted, generatorActive, lastServerID, serverActive, processingMs := gen.snapshot()
		writeJSON(w, map[string]any{
			"running":          running,
			"rpm":              rpm,
			"totalEmitted":     totalEmitted,
			"generatorActive":  generatorActive,
			"lastServerId":     lastServerID,
			"lastServerActive": serverActive,
			"processingMs":     processingMs,
		})
	})

	log.Printf("[generator] API de control escuchando en :%s (objetivo backend: %s)", controlPort, backendURL)
	if err := http.ListenAndServe(":"+controlPort, corsMiddleware(mux)); err != nil {
		log.Fatalf("[generator] error en ListenAndServe: %v", err)
	}
}

// runLoop emite solicitudes hacia el backend a la tasa RPM configurada.
func runLoop(g *generatorState) {
	for {
		g.mu.RLock()
		running := g.isRunning
		currentRPM := g.rpm
		g.mu.RUnlock()

		if !running {
			<-g.restartChan
			continue
		}

		interval := time.Minute / time.Duration(currentRPM)
		ticker := time.NewTicker(interval)

		doneLoop := false
		for !doneLoop {
			select {
			case <-g.restartChan:
				ticker.Stop()
				doneLoop = true
			case <-ticker.C:
				g.mu.RLock()
				stillRunning := g.isRunning
				g.mu.RUnlock()

				if stillRunning {
					select {
					case inFlight <- struct{}{}:
						go func() {
							defer func() { <-inFlight }()
							emitRequest(g)
						}()
					default:
						log.Printf("[generator] descarte por saturación: los backends no dan abasto al RPM configurado (%d)", currentRPM)
					}
				} else {
					ticker.Stop()
					doneLoop = true
				}
			}
		}
	}
}

func emitRequest(g *generatorState) {
	g.mu.Lock()
	g.totalEmitted++
	requestID := g.totalEmitted
	g.genActiveUntil = time.Now().Add(500 * time.Millisecond)
	g.mu.Unlock()

	req, err := http.NewRequest(http.MethodPost, backendURL, nil)
	if err != nil {
		log.Printf("[generator] error creando solicitud #%d: %v", requestID, err)
		return
	}
	req.Header.Set("X-Request-Id", strconv.Itoa(requestID))

	resp, err := backendHTTPClient.Do(req)
	if err != nil {
		log.Printf("[generator] error enviando solicitud #%d al backend: %v", requestID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[generator] backend respondió %d para solicitud #%d", resp.StatusCode, requestID)
		return
	}

	serverID := resp.Header.Get("X-Server-Id")
	if serverID == "" {
		log.Printf("[generator] respuesta sin header X-Server-Id para solicitud #%d", requestID)
		return
	}

	g.mu.Lock()
	g.lastServerID = serverID
	g.lastActiveUntil = time.Now().Add(500 * time.Millisecond)
	g.mu.Unlock()

	if err := db.LogReception(requestID, "Generador-HTTP", "k8s-service", serverID); err != nil {
		log.Printf("[generator] error registrando recepción #%d: %v", requestID, err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// corsMiddleware permite que la GUI nativa (proceso distinto en el host)
// pueda hacer fetch a esta API de control sin bloqueos de origen.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}
