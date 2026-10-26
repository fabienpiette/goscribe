//go:build unix && !linux

package util

import (
	"os/exec"
	"syscall"
)

// OwnProcessGroup starts cmd in a process group of its own, so a Ctrl+C in
// the terminal doesn't reach it (no Pdeathsig outside Linux).
func OwnProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// KillProcessGroup kills cmd and everything it started (its process group,
// see OwnProcessGroup).
func KillProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
