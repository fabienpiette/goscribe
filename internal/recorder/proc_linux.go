//go:build linux

package recorder

import (
	"os/exec"
	"syscall"
)

// isolate runs cmd in its own process group, so a terminal Ctrl+C reaches
// goscribe only and never interrupts ffmpeg mid-write. Pdeathsig makes the
// kernel send ffmpeg SIGTERM (on which it finalizes the file) if goscribe dies
// without its stop path, so the mic is never left recording in the background.
// Pdeathsig fires when the thread that started the child exits, so Record
// keeps its goroutine locked to that thread.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

// kill stops cmd and anything it started (its whole process group).
func kill(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
