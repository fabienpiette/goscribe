//go:build !unix

package util

import "os/exec"

// OwnProcessGroup is a no-op outside Unix.
func OwnProcessGroup(cmd *exec.Cmd) {}
