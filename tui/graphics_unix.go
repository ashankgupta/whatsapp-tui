//go:build linux || darwin || freebsd || netbsd || openbsd

package tui

import (
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// cellPixelSize returns the size of one terminal cell in pixels, or zeros
// when the terminal does not report its pixel dimensions.
func cellPixelSize() (int, int) {
	for _, f := range []*os.File{os.Stdout, os.Stdin} {
		ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
		if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
			continue
		}
		return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
	}
	return 0, 0
}

// terminalSupportsSixel sends a Primary Device Attributes query and checks
// whether the reply advertises Sixel graphics (attribute 4). tmux answers
// for the outer terminal, so this also works inside tmux.
func terminalSupportsSixel() bool {
	in := int(os.Stdin.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	state, err := term.MakeRaw(in)
	if err != nil {
		return false
	}
	defer term.Restore(in, state)

	if _, err := os.Stdout.WriteString("\x1b[c"); err != nil {
		return false
	}

	var reply []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		fds := []unix.PollFd{{Fd: int32(in), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(remaining.Milliseconds())+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return false
		}
		read, err := unix.Read(in, buf)
		if err != nil || read <= 0 {
			return false
		}
		reply = append(reply, buf[:read]...)

		start := strings.Index(string(reply), "\x1b[?")
		if start < 0 {
			continue
		}
		end := strings.IndexByte(string(reply[start:]), 'c')
		if end < 0 {
			continue
		}
		params := string(reply[start+3 : start+end])
		for _, p := range strings.Split(params, ";") {
			if p == "4" {
				return true
			}
		}
		return false
	}
}
