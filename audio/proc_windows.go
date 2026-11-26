package audio

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps ffmpeg from opening a console window or writing into the
// app's console on Windows.
func hideWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
