//go:build unix

package whisperlocal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// Live transcription keeps whisper's output off the terminal and runs it in
// its own process group, so Ctrl+C in the terminal doesn't kill it.
func TestTranscribeLive(t *testing.T) {
	dir := fakeBinary(t, `ps -o pgid= -p $$ | tr -d ' ' > "$PGID_FILE"
echo "Detected language 'English' with probability 0.59"
echo " 92%|████▉ | 9.6/10.4 [00:02<00:00]" >&2
`+writeOutput)
	t.Setenv("ARGS_FILE", filepath.Join(dir, "args"))
	pgidFile := filepath.Join(dir, "pgid")
	t.Setenv("PGID_FILE", pgidFile)

	stdout, stderr := captureOutput(t)
	got, err := TranscribeLive(filepath.Join(dir, "segment.wav"), "base", "en")
	out, errOut := stdout(), stderr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello from whisper" {
		t.Errorf("transcript = %q", got)
	}
	if strings.Contains(out, "Detected language") || strings.Contains(errOut, "9.6/10.4") {
		t.Errorf("whisper's output reached the terminal: stdout %q, stderr %q", out, errOut)
	}
	data, _ := os.ReadFile(pgidFile)
	pgid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pgid == 0 || pgid == syscall.Getpgrp() {
		t.Errorf("whisper's process group = %d, want its own (ours is %d)", pgid, syscall.Getpgrp())
	}
}

// A failure keeps the end of whisper's output in the error.
func TestTranscribeLiveError(t *testing.T) {
	fakeBinary(t, `echo "loading model..."; echo "RuntimeError: out of memory" >&2; exit 1`)
	_, err := TranscribeLive("x.wav", "base", "")
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Errorf("err = %v, want the end of whisper's output", err)
	}
}

// captureOutput redirects os.Stdout and os.Stderr for the test.
func captureOutput(t *testing.T) (stdout, stderr func() string) {
	t.Helper()
	read := func(f **os.File) func() string {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		orig := *f
		*f = w
		done := make(chan string, 1)
		go func() {
			b := make([]byte, 1<<16)
			n, _ := r.Read(b)
			done <- string(b[:n])
		}()
		return func() string {
			*f = orig
			w.Close()
			return <-done
		}
	}
	return read(&os.Stdout), read(&os.Stderr)
}
