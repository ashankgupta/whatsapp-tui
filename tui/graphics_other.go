//go:build !(linux || darwin || freebsd || netbsd || openbsd || windows)

package tui

// cellPixelSize is not available on this platform.
func cellPixelSize() (int, int) { return 0, 0 }

// terminalSupportsSixel is not probed on this platform; set
// WHATSAPP_TUI_IMAGES=sixel to enable Sixel images manually.
func terminalSupportsSixel() bool { return false }
