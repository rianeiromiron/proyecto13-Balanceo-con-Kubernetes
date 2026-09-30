package ui

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/basicfont"
)

var defaultFont = basicfont.Face7x13

// DrawBox dibuja un cuadro con fondo, borde y texto centrado opcional
func DrawBox(dst *ebiten.Image, x, y, w, h float32, bgColor, borderColor color.Color, borderWidth float32) {
	// Fondo
	vector.DrawFilledRect(dst, x, y, w, h, bgColor, true)
	// Borde
	if borderWidth > 0 {
		vector.StrokeRect(dst, x, y, w, h, borderWidth, borderColor, true)
	}
}

// DrawText dibuja texto en la posición especificada
func DrawText(dst *ebiten.Image, str string, x, y int, clr color.Color) {
	text.Draw(dst, str, defaultFont, x, y, clr)
}

// MeasureText calcula el ancho aproximado del texto
func MeasureText(str string) int {
	return len(str) * 7
}

// DrawArrow dibuja una flecha direccional desde (x1, y1) hasta (x2, y2)
func DrawArrow(dst *ebiten.Image, x1, y1, x2, y2 float32, thickness float32, clr color.Color, headSize float32) {
	// Línea principal
	vector.StrokeLine(dst, x1, y1, x2, y2, thickness, clr, true)

	// Cálculo del ángulo para la punta de flecha
	dx := float64(x2 - x1)
	dy := float64(y2 - y1)
	angle := math.Atan2(dy, dx)

	// Ángulos de las aletas de la punta (30 grados hacia atrás)
	wingAngle := math.Pi / 6.0
	leftAngle := angle + math.Pi - wingAngle
	rightAngle := angle + math.Pi + wingAngle

	lx := x2 + float32(math.Cos(leftAngle))*headSize
	ly := y2 + float32(math.Sin(leftAngle))*headSize

	rx := x2 + float32(math.Cos(rightAngle))*headSize
	ry := y2 + float32(math.Sin(rightAngle))*headSize

	// Dibujar aletas de la flecha
	vector.StrokeLine(dst, x2, y2, lx, ly, thickness+1, clr, true)
	vector.StrokeLine(dst, x2, y2, rx, ry, thickness+1, clr, true)
}
