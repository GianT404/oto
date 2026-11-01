package ui

import (
	"image/color"

	"oto/internal/tui/render"
)

func renderBox(w int, title, content string, borderColor color.Color) string {
	return render.Box(w, title, content, borderColor, PanelTitleStyle)
}
