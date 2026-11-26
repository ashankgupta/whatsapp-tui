//go:build !windows

package audio

import "os/exec"

func hideWindow(*exec.Cmd) {}
