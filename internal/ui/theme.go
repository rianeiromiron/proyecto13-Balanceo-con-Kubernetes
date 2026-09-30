package ui

import "image/color"

var (
	// Fondo general de la ventana
	ColorBackground = color.RGBA{R: 24, G: 26, B: 37, A: 255}
	ColorHeaderBg   = color.RGBA{R: 18, G: 19, B: 28, A: 255}
	ColorFooterBg   = color.RGBA{R: 18, G: 19, B: 28, A: 255}

	// Cajas en estado inactivo / vacío
	ColorBoxEmptyBg     = color.RGBA{R: 35, G: 38, B: 54, A: 255}
	ColorBoxEmptyBorder = color.RGBA{R: 80, G: 88, B: 118, A: 255}

	// Estados activos requeridos
	ColorGeneratorActive = color.RGBA{R: 46, G: 204, B: 113, A: 255} // Verde
	ColorBalancerActive  = color.RGBA{R: 241, G: 196, B: 15, A: 255} // Amarillo
	ColorServerActive    = color.RGBA{R: 52, G: 152, B: 219, A: 255} // Azul

	// Flechas
	ColorArrowIdle         = color.RGBA{R: 65, G: 72, B: 95, A: 255}
	ColorArrowGenActive    = color.RGBA{R: 46, G: 255, B: 140, A: 255} // Flecha Verde brillante
	ColorArrowServerActive = color.RGBA{R: 255, G: 215, B: 0, A: 255}  // Flecha Amarilla brillante hacia el Servidor

	// Textos
	ColorTextPrimary = color.RGBA{R: 240, G: 244, B: 255, A: 255}
	ColorTextMuted   = color.RGBA{R: 150, G: 158, B: 185, A: 255}
	ColorTextDark    = color.RGBA{R: 15, G: 15, B: 20, A: 255}

	// Botones estándar
	ColorBtnNormal = color.RGBA{R: 48, G: 54, B: 76, A: 255}
	ColorBtnHover  = color.RGBA{R: 68, G: 77, B: 108, A: 255}
	ColorBtnText   = color.RGBA{R: 255, G: 255, B: 255, A: 255}

	// Botón Start / Stop
	ColorBtnStart      = color.RGBA{R: 39, G: 174, B: 96, A: 255}
	ColorBtnStartHover = color.RGBA{R: 46, G: 204, B: 113, A: 255}
	ColorBtnStop       = color.RGBA{R: 192, G: 57, B: 43, A: 255}
	ColorBtnStopHover  = color.RGBA{R: 231, G: 76, B: 60, A: 255}
)
