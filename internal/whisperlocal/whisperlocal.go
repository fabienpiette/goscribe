// Package whisperlocal transcribes audio on the local machine with faster-whisper,
// through the whisper-ctranslate2 CLI. No audio leaves the machine.
package whisperlocal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Binary is the whisper-ctranslate2 executable. Overridable for tests.
var Binary = "whisper-ctranslate2"

const DefaultModel = "small"

func Transcribe(audioPath, model, language string) (string, error) {
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
	// Let the model download and progress bar show through.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%s exited with code %d", Binary, exitErr.ExitCode())
		}
		return "", fmt.Errorf("running %s: %w", Binary, err)
	}

	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	data, err := os.ReadFile(filepath.Join(outDir, base+".txt"))
	if err != nil {
		return "", fmt.Errorf("reading %s output: %w", Binary, err)
	}

	return strings.TrimSpace(string(data)), nil
}
