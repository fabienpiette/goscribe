//go:build !unix

package util

import "os/exec"

// OwnProcessGroup is a no-op outside Unix.
func OwnProcessGroup(cmd *exec.Cmd) {}

// KillProcessGroup kills cmd (no process groups outside Unix).
func KillProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
