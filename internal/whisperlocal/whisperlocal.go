// Package whisperlocal transcribes audio on the local machine with faster-whisper,
// through the whisper-ctranslate2 CLI. No audio leaves the machine.
package whisperlocal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"goscribe/internal/util"
)

// Binary is the whisper-ctranslate2 executable. Overridable for tests.
var Binary = "whisper-ctranslate2"

const DefaultModel = "small"

// Transcribe transcribes a recording, showing whisper's model download and
// progress bar.
func Transcribe(audioPath, model, language string) (string, error) {
	return run(audioPath, model, language, false)
}

// TranscribeLive transcribes a short segment during a live session: whisper's
// output stays off the terminal (its end goes in the error, if any), and it
// runs in its own process group, so a Ctrl+C meant for goscribe doesn't kill
// it mid-segment.
func TranscribeLive(audioPath, model, language string) (string, error) {
	return run(audioPath, model, language, true)
}

func run(audioPath, model, language string, live bool) (string, error) {
	bin, err := exec.LookPath(Binary)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH (install with: uv tool install whisper-ctranslate2)", Binary)
	}

	if model == "" {
		model = DefaultModel
	}

	outDir, err := os.MkdirTemp("", "goscribe-whisper-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	args := []string{
		audioPath,
		"--model", model,
		"--device", "auto",
		"--compute_type", "auto",
		"--vad_filter", "True",
		"--output_format", "txt",
		"--output_dir", outDir,
		"--verbose", "False",
	}
	if language != "" {
		args = append(args, "--language", language)
	}

	cmd := exec.Command(bin, args...)
	var output bytes.Buffer
	if live {
		cmd.Stdout, cmd.Stderr = &output, &output
		util.OwnProcessGroup(cmd)
		runtime.LockOSThread() // see OwnProcessGroup
		defer runtime.UnlockOSThread()
	} else {
		// Let the model download and progress bar show through.
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		detail := ""
		if tail := lastLines(output.String(), 3); tail != "" {
			detail = ": " + tail
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%s exited with code %d%s", Binary, exitErr.ExitCode(), detail)
		}
		return "", fmt.Errorf("running %s: %w%s", Binary, err, detail)
	}

	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	data, err := os.ReadFile(filepath.Join(outDir, base+".txt"))
	if err != nil {
		return "", fmt.Errorf("reading %s output: %w", Binary, err)
	}

	return strings.TrimSpace(string(data)), nil
}

// lastLines joins the last n non-empty lines of s.
func lastLines(s string, n int) string {
	var kept []string
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0 && len(kept) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			kept = append([]string{l}, kept...)
		}
	}
	return strings.Join(kept, " | ")
}
