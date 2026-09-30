package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// Button representa un botón gráfico clickeable
type Button struct {
	X                float32
	Y                float32
	W                float32
	H                float32
	Label            string
	OnClick          func()
	CustomBgColor    color.Color
	CustomHoverColor color.Color
	CustomTextColor  color.Color
}

// NewButton crea un nuevo botón interactivo
func NewButton(x, y, w, h float32, label string, onClick func()) *Button {
	return &Button{
		X:       x,
		Y:       y,
		W:       w,
		H:       h,
		Label:   label,
		OnClick: onClick,
	}
}

// Contains verifica si el punto (px, py) está dentro del botón
func (b *Button) Contains(px, py int) bool {
	fpx := float32(px)
	fpy := float32(py)
	return fpx >= b.X && fpx <= b.X+b.W && fpy >= b.Y && fpy <= b.Y+b.H
}

// Update procesa eventos de mouse para el botón
func (b *Button) Update() {
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		mx, my := ebiten.CursorPosition()
		if b.Contains(mx, my) && b.OnClick != nil {
			b.OnClick()
		}
	}
}

// Draw dibuja el botón con efectos hover y colores personalizables
func (b *Button) Draw(dst *ebiten.Image) {
	mx, my := ebiten.CursorPosition()
	isHovered := b.Contains(mx, my)

	var bgColor color.Color = ColorBtnNormal
	if b.CustomBgColor != nil {
		bgColor = b.CustomBgColor
	}

	if isHovered {
		if b.CustomHoverColor != nil {
			bgColor = b.CustomHoverColor
		} else {
			bgColor = ColorBtnHover
		}
	}

	var textColor color.Color = ColorBtnText
	if b.CustomTextColor != nil {
		textColor = b.CustomTextColor
	}

	DrawBox(dst, b.X, b.Y, b.W, b.H, bgColor, ColorBoxEmptyBorder, 1.5)

	// Texto centrado
	textW := MeasureText(b.Label)
	tx := int(b.X + (b.W-float32(textW))/2)
	ty := int(b.Y + b.H/2 + 4)
	DrawText(dst, b.Label, tx, ty, textColor)
}
