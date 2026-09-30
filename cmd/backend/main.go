package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"loadbalancer-k8s/internal/db"
)

const (
	minSleepMs = 100
	maxSleepMs = 3000
)

var (
	sampleClientes = []string{
		"Carlos Gomez", "Maria Rodriguez", "Juan Martinez", "Laura Sanchez",
		"David Torres", "Elena Fernandez", "Pedro Ramirez", "Sofia Morales",
	}

	sampleProductos = []struct {
		nombre string
		precio float64
	}{
		{"Laptop Pro 16\"", 1450.00},
		{"Monitor 4K 27\"", 420.00},
		{"Teclado Mecanico RGB", 115.00},
		{"Mouse Ergonomico Inalambrico", 65.50},
		{"Auriculares Noise-Cancelling", 199.99},
		{"Disco SSD NVMe 2TB", 149.00},
	}

	sampleMetodos = []string{"Tarjeta de Credito", "Transferencia SPEI", "PayPal", "Efectivo"}
)

// busySlot simula "un servidor, una tarea a la vez", igual que en proyecto12.
var busySlot sync.Mutex

// currentSleepMs es el tiempo de procesamiento simulado, ajustable en vivo
// por el generador (POST /sleep).
var currentSleepMs int64

func main() {
	// En Kubernetes la identidad del backend es el nombre de su propio Pod
	// (Kubernetes fija el hostname del contenedor = nombre del Pod), no un
	// SERVER_ID fijo como en proyecto12 - los Pods de un Deployment son
	// intercambiables y efímeros.
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("no se pudo obtener el hostname del pod: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	initialSleep, err := strconv.Atoi(os.Getenv("PROCESSING_MS"))
	if err != nil || initialSleep <= 0 {
		initialSleep = 700
	}
	atomic.StoreInt64(&currentSleepMs, int64(clampSleep(initialSleep)))

	if _, err := db.Connect(""); err != nil {
		log.Printf("[backend %s] advertencia: no se pudo conectar a Postgres en el arranque: %v", hostname, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		handleProcess(w, r, hostname)
	})
	mux.HandleFunc("/sleep", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Ms int `json:"ms"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				atomic.StoreInt64(&currentSleepMs, int64(clampSleep(body.Ms)))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ms": atomic.LoadInt64(&currentSleepMs)})
	})

	log.Printf("[backend %s] escuchando en :%s", hostname, port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("[backend %s] error en ListenAndServe: %v", hostname, err)
	}
}

func clampSleep(ms int) int {
	if ms < minSleepMs {
		return minSleepMs
	}
	if ms > maxSleepMs {
		return maxSleepMs
	}
	return ms
}

func handleProcess(w http.ResponseWriter, r *http.Request, serverID string) {
	requestID, err := strconv.Atoi(r.Header.Get("X-Request-Id"))
	if err != nil {
		http.Error(w, "falta o es inválido el header X-Request-Id", http.StatusBadRequest)
		return
	}

	// Solo una solicitud a la vez por backend, como el worker serial original.
	busySlot.Lock()
	defer busySlot.Unlock()

	cliente := sampleClientes[rand.Intn(len(sampleClientes))]
	prod := sampleProductos[rand.Intn(len(sampleProductos))]
	cantidad := rand.Intn(3) + 1
	metodo := sampleMetodos[rand.Intn(len(sampleMetodos))]
	total := float64(cantidad) * prod.precio

	sale := db.Sale{
		Cliente:        cliente,
		Producto:       prod.nombre,
		Cantidad:       cantidad,
		PrecioUnitario: prod.precio,
		Total:          total,
		MetodoPago:     metodo,
		FechaHora:      time.Now(),
	}

	// Simula el tiempo de procesamiento, ajustable en vivo desde la GUI.
	time.Sleep(time.Duration(atomic.LoadInt64(&currentSleepMs)) * time.Millisecond)

	if err := db.InsertSale(requestID, serverID, sale); err != nil {
		http.Error(w, "error al registrar la venta", http.StatusInternalServerError)
		return
	}

	w.Header().Set("X-Server-Id", serverID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"server_id": serverID,
		"request":   requestID,
		"total":     fmt.Sprintf("%.2f", total),
	})
}
