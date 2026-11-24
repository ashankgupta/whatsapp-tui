package tui

import (
	"os"
	"strings"
)

// graphicsMode selects how inline images are drawn.
type graphicsMode int

const (
	// graphicsBlocks draws images with truecolor half-block characters,
	// which works in any modern terminal but is low resolution.
	graphicsBlocks graphicsMode = iota
	// graphicsSixel draws real pixels using the Sixel graphics protocol.
	graphicsSixel
)

// graphics holds the terminal's image capabilities. It is set once by
// DetectGraphics before the program starts and the cell size is refreshed on
// resize from the Bubble Tea event loop.
var graphics struct {
	mode  graphicsMode
	cellW int
	cellH int
}

// DetectGraphics probes the terminal for Sixel support and its cell size in
// pixels. It must run before the Bubble Tea program takes over the terminal.
//
// WHATSAPP_TUI_IMAGES=blocks forces half-block previews and
// WHATSAPP_TUI_IMAGES=sixel skips the capability query.
func DetectGraphics() {
	graphics.mode = graphicsBlocks
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WHATSAPP_TUI_IMAGES"))) {
	case "blocks", "off", "none":
		return
	case "sixel":
		if !refreshCellSize() {
			// VT340 cell size, which Windows Terminal also uses for Sixel.
			graphics.cellW, graphics.cellH = 10, 20
		}
		graphics.mode = graphicsSixel
		return
	}
	if !refreshCellSize() {
		return
	}
	if terminalSupportsSixel() {
		graphics.mode = graphicsSixel
	}
}

// refreshCellSize updates the cell size in pixels and reports whether it is
// known. It keeps the previous size when the terminal does not report one.
func refreshCellSize() bool {
	w, h := cellPixelSize()
	if w <= 0 || h <= 0 {
		return graphics.cellW > 0 && graphics.cellH > 0
	}
	graphics.cellW, graphics.cellH = w, h
	return true
}
