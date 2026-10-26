//go:build unix

package whisperlocal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakePython stands in for the model server: it logs each start and its
// process group, answers "ready", then one text line per request.
func fakePython(t *testing.T, perRequest string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	path := filepath.Join(dir, "python")
	script := `#!/bin/sh
echo start >> "$FAKE_DIR/starts"
ps -o pgid= -p $$ | tr -d ' ' > "$FAKE_DIR/pgid"
echo "loading model" >&2
echo '{"ready": true}'
while read -r line; do
  n=$(( $(cat "$FAKE_DIR/n" 2>/dev/null || echo 0) + 1 )); echo $n > "$FAKE_DIR/n"
  ` + perRequest + `
done
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	orig := Python
	Python = path
	t.Cleanup(func() { Python = orig })
	return dir
}

func starts(t *testing.T, dir string) int {
	data, _ := os.ReadFile(filepath.Join(dir, "starts"))
	return strings.Count(string(data), "start")
}

// The model loads once for many segments, in its own process group.
func TestServerLoadsOnce(t *testing.T) {
	dir := fakePython(t, `echo "{\"text\": \"  segment $n \"}"`)
	s := NewServer("base", "fr")
	defer s.Close()
	for i := 1; i <= 3; i++ {
		got, err := s.Transcribe("seg.wav")
		if err != nil {
			t.Fatal(err)
		}
		if got != "segment "+strconv.Itoa(i) {
			t.Errorf("transcript %d = %q", i, got)
		}
	}
	if n := starts(t, dir); n != 1 {
		t.Errorf("model loaded %d times, want once", n)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "pgid"))
	if pgid, _ := strconv.Atoi(strings.TrimSpace(string(data))); pgid == 0 || pgid == syscall.Getpgrp() {
		t.Errorf("server process group = %d, want its own", pgid)
	}
}

// An error answer is returned; a crash or a hang restarts the server on the
// next segment.
func TestServerErrorsAndRestarts(t *testing.T) {
	dir := fakePython(t, `if [ $n -eq 1 ]; then echo '{"error": "bad audio"}'; elif [ $n -eq 2 ]; then exit 1; else echo '{"text": "ok"}'; fi`)
	s := NewServer("base", "")
	defer s.Close()
	if _, err := s.Transcribe("a.wav"); err == nil || !strings.Contains(err.Error(), "bad audio") {
		t.Errorf("err = %v, want the server's error", err)
	}
	if _, err := s.Transcribe("b.wav"); err == nil || !strings.Contains(err.Error(), "loading model") {
		t.Errorf("err = %v, want the crash with the end of its output", err)
	}
	if got, err := s.Transcribe("c.wav"); err != nil || got != "ok" {
		t.Errorf("after a crash: %q, %v", got, err)
	}
	if n := starts(t, dir); n != 2 {
		t.Errorf("starts = %d, want a restart after the crash", n)
	}

	fakePython(t, `sleep 5`)
	orig := ServerTimeout
	ServerTimeout = 200 * time.Millisecond
	t.Cleanup(func() { ServerTimeout = orig })
	s2 := NewServer("base", "")
	defer s2.Close()
	start := time.Now()
	if _, err := s2.Transcribe("d.wav"); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v, want a timeout", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("a hung server must not block past the timeout")
	}
}

// Without the Python of whisper-ctranslate2, there is no server.
func TestServerAvailable(t *testing.T) {
	orig, origBin := Python, Binary
	Python, Binary = "", "goscribe-no-such-whisper"
	t.Cleanup(func() { Python, Binary = orig, origBin })
	if ServerAvailable() {
		t.Error("no server without whisper-ctranslate2")
	}
	dir := fakeBinary(t, "true")
	Binary = filepath.Join(dir, "fake-whisper") // shebang: #!/bin/sh
	if !ServerAvailable() {
		t.Error("the shebang interpreter of whisper-ctranslate2 is the server's Python")
	}
}

// Warm loads the model ahead of the first segment, once.
func TestServerWarm(t *testing.T) {
	dir := fakePython(t, `echo '{"text": "ok"}'`)
	s := NewServer("base", "")
	defer s.Close()
	s.Warm()
	if n := starts(t, dir); n != 1 {
		t.Fatalf("starts after Warm = %d, want 1", n)
	}
	s.Warm()
	if got, err := s.Transcribe("a.wav"); err != nil || got != "ok" {
		t.Fatalf("%q, %v", got, err)
	}
	if n := starts(t, dir); n != 1 {
		t.Errorf("starts = %d, want the warm server reused", n)
	}
}
