package whisperlocal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"goscribe/internal/util"
)

// Python is the interpreter that runs the model server. Empty means the one
// of whisper-ctranslate2 (its shebang), which has faster-whisper installed.
// Overridable for tests.
var Python = ""

// ServerTimeout bounds one segment's transcription; a server that takes
// longer is restarted. Overridable for tests.
var ServerTimeout = 2 * time.Minute

// readyTimeout bounds the model load (a first run downloads the model).
const readyTimeout = 10 * time.Minute

// serverScript loads the model once, then answers one JSON line per request
// line {"path": ...}: {"text": ...} or {"error": ...}.
const serverScript = `
import json, sys
from faster_whisper import WhisperModel
model = WhisperModel(sys.argv[1], device="auto", compute_type="auto")
language = sys.argv[2] or None
print(json.dumps({"ready": True}), flush=True)
for line in sys.stdin:
    try:
        path = json.loads(line)["path"]
        segments, _ = model.transcribe(path, language=language, vad_filter=True)
        print(json.dumps({"text": " ".join(s.text.strip() for s in segments)}), flush=True)
    except Exception as e:
        print(json.dumps({"error": str(e)}), flush=True)
`

// pythonPath finds the server's interpreter.
func pythonPath() (string, error) {
	if Python != "" {
		return Python, nil
	}
	bin, err := exec.LookPath(Binary)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH", Binary)
	}
	f, err := os.Open(bin)
	if err != nil {
		return "", err
	}
	defer f.Close()
	first, _ := bufio.NewReader(f).ReadString('\n')
	if !strings.HasPrefix(first, "#!") {
		return "", fmt.Errorf("%s has no interpreter line", bin)
	}
	fields := strings.Fields(strings.TrimPrefix(first, "#!"))
	if len(fields) == 0 {
		return "", fmt.Errorf("%s has an empty interpreter line", bin)
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return "", err
	}
	return fields[0], nil
}

// ServerAvailable reports whether a model server can be started.
func ServerAvailable() bool {
	_, err := pythonPath()
	return err == nil
}

// Server keeps one faster-whisper model loaded for many short segments: a
// model load takes seconds, a 20 s segment about 2 s with the base model. It
// starts on the first Transcribe, runs in its own process group (a terminal
// Ctrl+C doesn't reach it; it dies with goscribe on Linux), and restarts on
// the next call after a crash or a hang. Calls are serialized.
type Server struct {
	model, language string

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	exited chan struct{}
	stderr *tail
}

func NewServer(model, language string) *Server {
	return &Server{model: model, language: language}
}

// Transcribe returns the text of one audio file.
func (s *Server) Transcribe(path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil {
		if err := s.start(); err != nil {
			return "", err
		}
	}
	req, _ := json.Marshal(map[string]string{"path": path})
	if _, err := s.stdin.Write(append(req, '\n')); err != nil {
		return "", s.fail(fmt.Errorf("sending to the model server: %w", err))
	}
	var answer struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	line, err := s.read(ServerTimeout)
	if err != nil {
		return "", s.fail(err)
	}
	if err := json.Unmarshal([]byte(line), &answer); err != nil {
		return "", s.fail(fmt.Errorf("model server: unexpected answer %q", line))
	}
	if answer.Error != "" {
		return "", fmt.Errorf("model server: %s", answer.Error)
	}
	return strings.Join(strings.Fields(answer.Text), " "), nil
}

// Warm starts the server, loading the model, if it isn't running yet; errors
// are left for the next Transcribe, which retries.
func (s *Server) Warm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil {
		_ = s.start()
	}
}

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
}

func (s *Server) start() error {
	py, err := pythonPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(py, "-I", "-c", serverScript, s.model, s.language)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	s.stderr = &tail{}
	cmd.Stderr = s.stderr
	util.OwnProcessGroup(cmd)

	started := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		// Pdeathsig (Linux) is tied to the starting thread: keep it for the
		// server's whole life.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			close(exited)
			return
		}
		started <- nil
		_ = cmd.Wait()
		close(exited)
	}()
	if err := <-started; err != nil {
		return fmt.Errorf("starting the model server: %w", err)
	}
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	s.cmd, s.stdin, s.lines, s.exited = cmd, stdin, lines, exited

	line, err := s.read(readyTimeout)
	if err != nil {
		return s.fail(fmt.Errorf("loading the %s model: %w", s.model, err))
	}
	if !strings.Contains(line, `"ready"`) {
		return s.fail(fmt.Errorf("model server: unexpected first line %q", line))
	}
	return nil
}

// read waits for the server's next line.
func (s *Server) read(timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line, ok := <-s.lines:
		if !ok {
			return "", fmt.Errorf("the model server stopped: %s", s.stderr.last())
		}
		return line, nil
	case <-timer.C:
		return "", fmt.Errorf("the model server did not answer in %s", timeout)
	}
}

// fail stops the server, so the next call starts a new one, and returns err.
func (s *Server) fail(err error) error {
	s.stop()
	return err
}

func (s *Server) stop() {
	if s.cmd == nil {
		return
	}
	_ = s.stdin.Close()
	util.KillProcessGroup(s.cmd)
	select {
	case <-s.exited:
	case <-time.After(5 * time.Second):
	}
	s.cmd = nil
}

// tail keeps the end of the server's error output, for error messages.
type tail struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if t.buf.Len() > 8<<10 {
		b := t.buf.Bytes()
		t.buf = *bytes.NewBuffer(append([]byte(nil), b[len(b)-4<<10:]...))
	}
	return len(p), nil
}

func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return lastLines(t.buf.String(), 3)
}
