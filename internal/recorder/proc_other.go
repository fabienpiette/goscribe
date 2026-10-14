//go:build !unix

package recorder

import "os/exec"

// Recording needs PipeWire/PulseAudio, so other platforms only need to build:
// no process group, and kill stops ffmpeg itself.
func isolate(cmd *exec.Cmd) {}

func kill(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
}
