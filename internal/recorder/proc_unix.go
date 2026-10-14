//go:build unix && !linux

package recorder

import (
	"os/exec"
	"syscall"
)

// isolate runs cmd in its own process group, so a terminal Ctrl+C reaches
// goscribe only and never interrupts ffmpeg mid-write.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// kill stops cmd and anything it started (its whole process group).
func kill(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
