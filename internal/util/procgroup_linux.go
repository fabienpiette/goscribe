//go:build linux

package util

import (
	"os/exec"
	"syscall"
)

// OwnProcessGroup starts cmd in a process group of its own, so a Ctrl+C in
// the terminal (sent to the whole foreground group) doesn't reach it: the
// caller decides when it stops. Pdeathsig kills it if goscribe dies first, so
// nothing keeps running in the background; it fires when the thread that
// started the child exits, so the caller keeps its goroutine on one thread
// (runtime.LockOSThread) until cmd ends.
func OwnProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// KillProcessGroup kills cmd and everything it started (its process group,
// see OwnProcessGroup).
func KillProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
