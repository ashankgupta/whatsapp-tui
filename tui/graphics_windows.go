//go:build windows

package tui

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procReadConsoleInputW = kernel32.NewProc("ReadConsoleInputW")
)

// inputRecord mirrors the Win32 INPUT_RECORD structure for key events.
type inputRecord struct {
	eventType uint16
	_         uint16
	keyDown   int32
	repeat    uint16
	vkCode    uint16
	scanCode  uint16
	char      uint16
	ctrlState uint32
}

var probe struct {
	once         sync.Once
	sixel        bool
	cellW, cellH int
}

// probeTerminal asks the console for its device attributes and cell size in
// one round trip. Windows Terminal 1.22+ advertises Sixel (attribute 4);
// older versions and the classic console do not, so they keep half-blocks.
func probeTerminal() {
	probe.once.Do(func() {
		wt := os.Getenv("WT_SESSION") != ""
		reply, ok := queryConsole("\x1b[16t\x1b[c", 700*time.Millisecond)
		if ok {
			probe.sixel = hasSixelAttribute(reply)
		} else {
			// No answer: assume a current Windows Terminal, which supports Sixel.
			probe.sixel = wt
		}
		if !probe.sixel {
			return
		}
		// Windows Terminal measures Sixel images against a fixed 10x20 virtual
		// cell and scales them to the real font, so ignore its font size.
		probe.cellW, probe.cellH = 10, 20
		if !wt {
			if w, h, ok := parseCellSize(reply); ok {
				probe.cellW, probe.cellH = w, h
			}
		}
	})
}

// queryConsole writes a query and collects the terminal's reply from the
// console input buffer until the Device Attributes answer arrives. It reads
// input records directly, so focus or mouse events can never block it.
func queryConsole(query string, timeout time.Duration) (string, bool) {
	in := windows.Handle(os.Stdin.Fd())
	out := windows.Handle(os.Stdout.Fd())

	var inMode, outMode uint32
	if windows.GetConsoleMode(in, &inMode) != nil || windows.GetConsoleMode(out, &outMode) != nil {
		return "", false
	}
	defer windows.SetConsoleMode(in, inMode)
	defer windows.SetConsoleMode(out, outMode)

	raw := inMode &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT)
	if windows.SetConsoleMode(in, raw|windows.ENABLE_VIRTUAL_TERMINAL_INPUT) != nil {
		return "", false
	}
	if windows.SetConsoleMode(out, outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
		return "", false
	}
	if _, err := os.Stdout.WriteString(query); err != nil {
		return "", false
	}

	var reply strings.Builder
	records := make([]inputRecord, 32)
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return reply.String(), false
		}
		ev, err := windows.WaitForSingleObject(in, uint32(remaining.Milliseconds())+1)
		if err != nil || ev != windows.WAIT_OBJECT_0 {
			return reply.String(), false
		}
		var n uint32
		r, _, _ := procReadConsoleInputW.Call(uintptr(in), uintptr(unsafe.Pointer(&records[0])), uintptr(len(records)), uintptr(unsafe.Pointer(&n)))
		if r == 0 {
			return reply.String(), false
		}
		for _, rec := range records[:n] {
			if rec.eventType == windows.KEY_EVENT && rec.keyDown != 0 && rec.char != 0 {
				reply.WriteRune(rune(rec.char))
			}
		}
		if s := reply.String(); strings.Contains(s, "\x1b[?") && strings.HasSuffix(strings.TrimRight(s, "\r\n"), "c") {
			return s, true
		}
	}
}

// hasSixelAttribute reports whether a Device Attributes reply lists Sixel.
func hasSixelAttribute(reply string) bool {
	start := strings.LastIndex(reply, "\x1b[?")
	if start < 0 {
		return false
	}
	params := strings.TrimSuffix(strings.TrimRight(reply[start+3:], "\r\n"), "c")
	for _, p := range strings.Split(params, ";") {
		if p == "4" {
			return true
		}
	}
	return false
}

// parseCellSize extracts a "CSI 6 ; height ; width t" cell size report.
func parseCellSize(reply string) (int, int, bool) {
	start := strings.Index(reply, "\x1b[6;")
	if start < 0 {
		return 0, 0, false
	}
	end := strings.IndexByte(reply[start:], 't')
	if end < 0 {
		return 0, 0, false
	}
	parts := strings.Split(reply[start+4:start+end], ";")
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	w, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// cellPixelSize returns the cell size Sixel images are measured against.
func cellPixelSize() (int, int) {
	probeTerminal()
	return probe.cellW, probe.cellH
}

// terminalSupportsSixel reports whether the console can draw Sixel images.
func terminalSupportsSixel() bool {
	probeTerminal()
	return probe.sixel
}
