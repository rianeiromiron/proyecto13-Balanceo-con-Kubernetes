package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"loadbalancer-k8s/internal/db"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	canvasW = 1230
	canvasH = 630
)

// statusSnapshot es el reflejo local del estado que expone el generador vía
// GET /status.
type statusSnapshot struct {
	Running          bool
	RPM              int
	TotalEmitted     int
	GeneratorActive  bool
	LastServerID     string
	LastServerActive bool
	ProcessingMs     int
}

// App representa el estado del monitor gráfico Ebitengine
type App struct {
	controlURL string
	httpClient *http.Client

	btnStartStop   *Button
	btnDecRPM      *Button
	btnIncRPM      *Button
	btnDecSleep    *Button
	btnIncSleep    *Button
	btnDecReplicas *Button
	btnIncReplicas *Button

	statusMu sync.RWMutex
	status   statusSnapshot

	podsMu   sync.RWMutex
	podNames []string

	dbRecepcionCount int
	dbVentasCount    int
	servedByServer   map[string]int
	lastDBSync       time.Time
	lastDBRetry      time.Time
}

// NewApp inicializa la interfaz gráfica con sus controles.
// controlURL apunta a la API del generador (ej. http://localhost:9091).
func NewApp(controlURL string) *App {
	app := &App{
		controlURL:     strings.TrimSuffix(controlURL, "/"),
		httpClient:     &http.Client{Timeout: 2 * time.Second},
		servedByServer: make(map[string]int),
	}

	app.btnStartStop = NewButton(35, 528, 105, 42, "START", func() {
		go app.postControl("/toggle", nil)
	})

	app.btnDecRPM = NewButton(325, 534, 34, 30, "-", func() {
		go app.postControl("/rpm", map[string]any{"delta": -15})
	})
	app.btnIncRPM = NewButton(367, 534, 34, 30, "+", func() {
		go app.postControl("/rpm", map[string]any{"delta": 15})
	})

	app.btnDecSleep = NewButton(635, 534, 34, 30, "-", func() {
		go app.postControl("/sleep", map[string]any{"delta": -100})
	})
	app.btnIncSleep = NewButton(677, 534, 34, 30, "+", func() {
		go app.postControl("/sleep", map[string]any{"delta": 100})
	})

	app.btnDecReplicas = NewButton(865, 534, 34, 30, "-", func() {
		go app.postControl("/scale", map[string]any{"delta": -1})
	})
	app.btnIncReplicas = NewButton(907, 534, 34, 30, "+", func() {
		go app.postControl("/scale", map[string]any{"delta": 1})
	})

	go app.pollStatusLoop()
	go app.pollPodsLoop()

	return app
}

func (a *App) postControl(path string, body map[string]any) {
	var reqBody *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(b))
	} else {
		reqBody = strings.NewReader("")
	}
	resp, err := a.httpClient.Post(a.controlURL+path, "application/json", reqBody)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// pollStatusLoop consulta periódicamente al generador en segundo plano para
// no bloquear el bucle de renderizado a 60 FPS con latencia de red.
func (a *App) pollStatusLoop() {
	for {
		resp, err := a.httpClient.Get(a.controlURL + "/status")
		if err == nil {
			var s statusSnapshot
			var raw struct {
				Running          bool   `json:"running"`
				RPM              int    `json:"rpm"`
				TotalEmitted     int    `json:"totalEmitted"`
				GeneratorActive  bool   `json:"generatorActive"`
				LastServerID     string `json:"lastServerId"`
				LastServerActive bool   `json:"lastServerActive"`
				ProcessingMs     int    `json:"processingMs"`
			}
			if json.NewDecoder(resp.Body).Decode(&raw) == nil {
				s = statusSnapshot(raw)
				a.statusMu.Lock()
				a.status = s
				a.statusMu.Unlock()
			}
			resp.Body.Close()
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// pollPodsLoop consulta la lista de Pods backend con menor frecuencia que
// el status (es una llamada más pesada: el generador consulta a la API de
// Kubernetes en cada request a /pods).
func (a *App) pollPodsLoop() {
	for {
		resp, err := a.httpClient.Get(a.controlURL + "/pods")
		if err == nil {
			var raw struct {
				Pods []string `json:"pods"`
			}
			if json.NewDecoder(resp.Body).Decode(&raw) == nil {
				sort.Strings(raw.Pods)
				a.podsMu.Lock()
				a.podNames = raw.Pods
				a.podsMu.Unlock()
			}
			resp.Body.Close()
		}
		time.Sleep(700 * time.Millisecond)
	}
}

func (a *App) getStatus() statusSnapshot {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

func (a *App) getPodNames() []string {
	a.podsMu.RLock()
	defer a.podsMu.RUnlock()
	out := make([]string, len(a.podNames))
	copy(out, a.podNames)
	return out
}

// Update maneja la lógica de entrada y sincronización de métricas con PostgreSQL
func (a *App) Update() error {
	// Reintentar conexión si estuviera desconectado
	if db.DB == nil && time.Since(a.lastDBRetry) > 2*time.Second {
		a.lastDBRetry = time.Now()
		_, _ = db.Connect("")
	}

	status := a.getStatus()

	if status.Running {
		a.btnStartStop.Label = "STOP"
		a.btnStartStop.CustomBgColor = ColorBtnStop
		a.btnStartStop.CustomHoverColor = ColorBtnStopHover
	} else {
		a.btnStartStop.Label = "START"
		a.btnStartStop.CustomBgColor = ColorBtnStart
		a.btnStartStop.CustomHoverColor = ColorBtnStartHover
	}

	a.btnStartStop.Update()
	a.btnDecRPM.Update()
	a.btnIncRPM.Update()
	a.btnDecSleep.Update()
	a.btnIncSleep.Update()
	a.btnDecReplicas.Update()
	a.btnIncReplicas.Update()

	// Sincronizar conteos reales desde la base de datos cada 200ms
	if time.Since(a.lastDBSync) > 200*time.Millisecond {
		a.dbRecepcionCount, a.dbVentasCount = db.GetCounts()
		for _, name := range a.getPodNames() {
			a.servedByServer[name] = db.ServedByServer(name)
		}
		a.lastDBSync = time.Now()
	}

	return nil
}

// Draw renderiza todos los elementos gráficos en cada fotograma
func (a *App) Draw(screen *ebiten.Image) {
	screen.Fill(ColorBackground)

	status := a.getStatus()
	pods := a.getPodNames()

	a.drawHeader(screen)
	a.drawDiagram(screen, status, pods)
	a.drawBottomPanel(screen, status, len(pods))
}

// drawHeader dibuja el banner superior de la aplicación
func (a *App) drawHeader(screen *ebiten.Image) {
	vector.DrawFilledRect(screen, 0, 0, canvasW, 55, ColorHeaderBg, false)
	vector.StrokeLine(screen, 0, 55, canvasW, 55, 1.5, ColorBoxEmptyBorder, false)

	DrawText(screen, "MONITOR DE BALANCEADOR DE CARGA CON KUBERNETES", 30, 24, ColorTextPrimary)
	DrawText(screen, "Service de Kubernetes | Pods Go | Render 60 FPS", 30, 44, ColorTextMuted)

	// Indicador de conexión a Base de Datos
	boxX := float32(canvasW) - 280 - 30
	if db.DB != nil {
		DrawBox(screen, boxX, 12, 280, 30, ColorBoxEmptyBg, ColorGeneratorActive, 1.5)
		DrawText(screen, "POSTGRES: CONECTADO (Kubernetes)", int(boxX)+12, 32, ColorGeneratorActive)
	} else {
		DrawBox(screen, boxX, 12, 280, 30, ColorBoxEmptyBg, ColorArrowServerActive, 1.5)
		DrawText(screen, "POSTGRES: DESCONECTADO", int(boxX)+30, 32, ColorArrowServerActive)
	}
}

// shortPodLabel devuelve el sufijo legible de un nombre de Pod
// (ej. "backend-7f9c8d6b5-x2kpq" -> "x2kpq").
func shortPodLabel(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) == 0 {
		return podName
	}
	return parts[len(parts)-1]
}

// drawDiagram dibuja el generador, el Service de Kubernetes (balanceador),
// flechas y una caja por cada Pod backend actualmente Ready.
func (a *App) drawDiagram(screen *ebiten.Image, status statusSnapshot, pods []string) {
	// 1. GENERADOR DE SOLICITUDES
	genX, genY, genW, genH := float32(60), float32(200), float32(160), float32(140)
	genActive := status.GeneratorActive
	isRunning := status.Running

	if genActive {
		DrawBox(screen, genX, genY, genW, genH, ColorGeneratorActive, ColorArrowGenActive, 3)
		DrawText(screen, "GENERADOR", int(genX)+36, int(genY)+45, ColorTextDark)
		DrawText(screen, "[ EMITIENDO ]", int(genX)+25, int(genY)+75, ColorTextDark)
		DrawText(screen, fmt.Sprintf("Emitidas: %d", status.TotalEmitted), int(genX)+22, int(genY)+105, ColorTextDark)
	} else {
		DrawBox(screen, genX, genY, genW, genH, ColorBoxEmptyBg, ColorBoxEmptyBorder, 2)
		DrawText(screen, "GENERADOR", int(genX)+36, int(genY)+45, ColorTextPrimary)
		if !isRunning {
			DrawText(screen, "( Pulsa START )", int(genX)+20, int(genY)+75, ColorArrowServerActive)
		} else {
			DrawText(screen, "( Vacio / Espera )", int(genX)+14, int(genY)+75, ColorTextMuted)
		}
		DrawText(screen, fmt.Sprintf("Emitidas: %d", status.TotalEmitted), int(genX)+22, int(genY)+105, ColorTextMuted)
	}

	// 2. FLECHA: GENERADOR -> SERVICE
	lbX, lbY, lbW, lbH := float32(410), float32(200), float32(160), float32(140)
	arrowGenStart := genX + genW
	arrowGenEnd := lbX
	arrowGenY := genY + genH/2

	if genActive {
		DrawArrow(screen, arrowGenStart, arrowGenY, arrowGenEnd, arrowGenY, 4, ColorArrowGenActive, 14)
	} else {
		DrawArrow(screen, arrowGenStart, arrowGenY, arrowGenEnd, arrowGenY, 2, ColorArrowIdle, 10)
	}

	// 3. SERVICE DE KUBERNETES (BALANCEADOR) — activo mientras el generador
	// está despachando, ya que kube-proxy elige el Pod destino de forma
	// síncrona dentro de esa misma conexión TCP.
	lbActive := genActive

	if lbActive {
		DrawBox(screen, lbX, lbY, lbW, lbH, ColorBalancerActive, ColorArrowServerActive, 3)
		DrawText(screen, "SERVICE K8s", int(lbX)+36, int(lbY)+40, ColorTextDark)
		DrawText(screen, "[ kube-proxy ]", int(lbX)+22, int(lbY)+68, ColorTextDark)
		DrawText(screen, "-> Registra en BD", int(lbX)+16, int(lbY)+92, ColorTextDark)
		DrawText(screen, fmt.Sprintf("Despachos: %d", a.dbRecepcionCount), int(lbX)+20, int(lbY)+118, ColorTextDark)
	} else {
		DrawBox(screen, lbX, lbY, lbW, lbH, ColorBoxEmptyBg, ColorBoxEmptyBorder, 2)
		DrawText(screen, "SERVICE K8s", int(lbX)+36, int(lbY)+40, ColorTextPrimary)
		DrawText(screen, "( Vacio / Espera )", int(lbX)+14, int(lbY)+68, ColorTextMuted)
		DrawText(screen, "-> Registra en BD", int(lbX)+16, int(lbY)+92, ColorTextMuted)
		DrawText(screen, fmt.Sprintf("Despachos: %d", a.dbRecepcionCount), int(lbX)+20, int(lbY)+118, ColorTextMuted)
	}

	// 4. PODS BACKEND (dinámicos, según réplicas actuales) Y FLECHAS
	// SERVICE -> POD
	nServers := len(pods)
	lbExitX := lbX + lbW
	lbExitY := lbY + lbH/2

	if nServers == 0 {
		DrawBox(screen, 740, lbY, 260, lbH, ColorBoxEmptyBg, ColorBoxEmptyBorder, 1.5)
		DrawText(screen, "Esperando pods listos...", 760, int(lbY)+75, ColorTextMuted)
		return
	}

	serverW := float32(240)
	maxServerH := float32(60)
	minServerH := float32(24)
	gap := float32(10)
	diagramTop := float32(90)
	diagramBottom := float32(490)
	availableH := diagramBottom - diagramTop

	serverH := maxServerH
	totalServersH := float32(nServers)*serverH + float32(nServers-1)*gap
	if totalServersH > availableH {
		serverH = (availableH - float32(nServers-1)*gap) / float32(nServers)
		if serverH < minServerH {
			serverH = minServerH
		}
		totalServersH = float32(nServers)*serverH + float32(nServers-1)*gap
	}

	centerDiagramY := lbY + lbH/2
	startServerY := centerDiagramY - totalServersH/2
	serverX := float32(740)

	for i, podName := range pods {
		sy := startServerY + float32(i)*(serverH+gap)
		targetArrowY := sy + serverH/2

		isArrowOn := status.LastServerActive && status.LastServerID == podName

		if isArrowOn {
			DrawArrow(screen, lbExitX, lbExitY, serverX, targetArrowY, 3.5, ColorArrowServerActive, 13)
		} else {
			DrawArrow(screen, lbExitX, lbExitY, serverX, targetArrowY, 1.8, ColorArrowIdle, 9)
		}

		served := a.servedByServer[podName]
		label := shortPodLabel(podName)

		if isArrowOn {
			DrawBox(screen, serverX, sy, serverW, serverH, ColorServerActive, ColorArrowGenActive, 2.5)
			DrawText(screen, fmt.Sprintf("POD %s [PROCESANDO]", label), int(serverX)+8, int(sy)+int(serverH/2), ColorTextDark)
		} else {
			DrawBox(screen, serverX, sy, serverW, serverH, ColorBoxEmptyBg, ColorBoxEmptyBorder, 1.5)
			DrawText(screen, fmt.Sprintf("POD %s (%d)", label, served), int(serverX)+8, int(sy)+int(serverH/2), ColorTextPrimary)
		}
	}
}

// drawBottomPanel dibuja los controles interactivos y las métricas
func (a *App) drawBottomPanel(screen *ebiten.Image, status statusSnapshot, replicaCount int) {
	panelY := float32(495)
	panelH := float32(130)

	vector.DrawFilledRect(screen, 0, panelY, canvasW, panelH, ColorFooterBg, false)
	vector.StrokeLine(screen, 0, panelY, canvasW, panelY, 1.5, ColorBoxEmptyBorder, false)

	// 1. Botón START / STOP
	DrawText(screen, "ESTADO SISTEMA", 35, int(panelY)+24, ColorTextMuted)
	a.btnStartStop.Draw(screen)

	vector.StrokeLine(screen, 160, panelY+15, 160, panelY+panelH-15, 1, ColorBoxEmptyBorder, false)

	// 2. Control de Tasa (RPM)
	DrawText(screen, "SOLICITUDES POR MINUTO", 180, int(panelY)+24, ColorTextMuted)
	DrawText(screen, fmt.Sprintf("%d RPM", status.RPM), 180, int(panelY)+55, ColorGeneratorActive)
	a.btnDecRPM.Draw(screen)
	a.btnIncRPM.Draw(screen)

	vector.StrokeLine(screen, 430, panelY+15, 430, panelY+panelH-15, 1, ColorBoxEmptyBorder, false)

	// 3. Tiempo de proceso simulado por Pod
	DrawText(screen, "TIEMPO DE PROCESO x POD", 450, int(panelY)+24, ColorTextMuted)
	DrawText(screen, fmt.Sprintf("%d ms", status.ProcessingMs), 450, int(panelY)+55, ColorServerActive)
	a.btnDecSleep.Draw(screen)
	a.btnIncSleep.Draw(screen)

	vector.StrokeLine(screen, 730, panelY+15, 730, panelY+panelH-15, 1, ColorBoxEmptyBorder, false)

	// 4. Réplicas del Deployment (escalado real vía la API de Kubernetes)
	DrawText(screen, "REPLICAS BACKEND (K8s)", 750, int(panelY)+24, ColorTextMuted)
	DrawText(screen, fmt.Sprintf("%d pods", replicaCount), 750, int(panelY)+55, ColorBalancerActive)
	a.btnDecReplicas.Draw(screen)
	a.btnIncReplicas.Draw(screen)

	vector.StrokeLine(screen, 950, panelY+15, 950, panelY+panelH-15, 1, ColorBoxEmptyBorder, false)

	// 5. Métricas Globales y Conteo Real en Base de Datos
	DrawText(screen, "REGISTROS REALES EN POSTGRES", 970, int(panelY)+24, ColorTextMuted)
	DrawText(screen, fmt.Sprintf("Generadas:    %d", status.TotalEmitted), 970, int(panelY)+44, ColorTextPrimary)
	DrawText(screen, fmt.Sprintf("K8s Logs BD:  %d filas", a.dbRecepcionCount), 970, int(panelY)+60, ColorGeneratorActive)
	DrawText(screen, fmt.Sprintf("Ventas en BD: %d filas", a.dbVentasCount), 970, int(panelY)+76, ColorServerActive)

	// Leyenda y estado
	statusMsg := "ESTADO: DETENIDO (Presione START para comenzar a emitir)"
	statusColor := ColorArrowServerActive
	if status.Running {
		statusMsg = "ESTADO: EN EJECUCION (Kubernetes balanceando, guardando en PostgreSQL)"
		statusColor = ColorGeneratorActive
	}
	DrawText(screen, statusMsg, 35, int(panelY)+105, statusColor)
}

// Layout define el tamaño del lienzo interno
func (a *App) Layout(outsideWidth, outsideHeight int) (int, int) {
	return canvasW, canvasH
}
