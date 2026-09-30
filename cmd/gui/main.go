package main

import (
	"log"
	"os"

	"loadbalancer-k8s/internal/db"
	"loadbalancer-k8s/internal/ui"

	"github.com/hajimehoshi/ebiten/v2"
)

func main() {
	// 1. Conectar a PostgreSQL (Kubernetes, publicado vía NodePort en el host)
	dbConn, err := db.Connect("")
	if err != nil {
		log.Printf("[ADVERTENCIA BD] No se pudo conectar a PostgreSQL en el arranque: %v", err)
	} else {
		defer dbConn.Close()
		log.Printf("[BD] Conectado exitosamente a PostgreSQL")
	}

	// 2. URL de la API de control del generador (Pod en Kubernetes, NodePort)
	controlURL := os.Getenv("GENERATOR_CONTROL_URL")
	if controlURL == "" {
		controlURL = "http://localhost:9091"
	}

	// 3. Configuración de la ventana del monitor gráfico
	ebiten.SetWindowSize(1230, 630)
	ebiten.SetWindowTitle("Monitor de Balanceador de Carga - Kubernetes + PostgreSQL")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)

	// 4. Crear la aplicación gráfica
	app := ui.NewApp(controlURL)

	// 5. Iniciar el bucle gráfico (60 FPS)
	if err := ebiten.RunGame(app); err != nil {
		log.Fatalf("Error en la ejecución del monitor gráfico: %v", err)
	}
}
