//go:build unix

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"goscribe/internal/assistant"
	"goscribe/internal/realtime"
	"goscribe/internal/recorder"
)

func assist(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runAssist(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestAssistName(t *testing.T) {
	if got := assistName(time.Date(2026, 10, 15, 9, 5, 0, 0, time.Local)); got != "assist-2026-10-15_09-05-00.md" {
		t.Errorf("assistName = %q", got)
	}
}

func TestAssistUsageErrors(t *testing.T) {
	fakeAudio(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "el-key")
	fakeClaude(t, `{"summary":"s"}`)
	dir := t.TempDir()
	existing := filepath.Join(dir, "notes.md")
	mustWrite(t, existing, []byte("x"), 0644)

	for name, c := range map[string]struct {
		args []string
		env  string
		want string
	}{
		"exists":        {[]string{"-provider", "claude", "-o", existing}, "el-key", "already exists"},
		"no folder":     {[]string{"-provider", "claude", "-o", filepath.Join(dir, "nope", "n.md")}, "el-key", "folder does not exist"},
		"no el key":     {[]string{"-provider", "claude"}, "", "ElevenLabs API key is required"},
		"bad provider":  {[]string{"-provider", "mistral"}, "el-key", "invalid provider"},
		"no openai key": {[]string{"-provider", "openai"}, "el-key", "needs an OpenAI key"},
		"bad context":   {[]string{"-provider", "claude", "-context", filepath.Join(dir, "missing.md")}, "el-key", "context:"},
		"bad interval":  {[]string{"-provider", "claude", "-interval", "-5s"}, "el-key", "-interval must be positive"},
		"bad source":    {[]string{"-provider", "claude", "-source", "radio"}, "el-key", "source"},
		"extra arg":     {[]string{"meeting.mp3"}, "el-key", "unexpected argument"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ELEVENLABS_API_KEY", c.env)
			_, stderr, code := assist(t, c.args...)
			if code != 2 || !strings.Contains(stderr, c.want) {
				t.Errorf("exit = %d, stderr = %q, want %q", code, stderr, c.want)
			}
		})
	}
}

func TestAssistListSources(t *testing.T) {
	fakeAudio(t)
	stdout, _, code := assist(t, "-list-sources")
	if code != 0 || !strings.Contains(stdout, "* usb-mic") {
		t.Errorf("exit = %d, stdout:\n%s", code, stdout)
	}
}

// syncBuf is safe to read while runAssist writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// End to end: fake pactl/ffmpeg capture both sides, a fake ElevenLabs server
// transcribes each session, a fake claude answers, and Ctrl+C ends the
// meeting with the final notes.
func TestAssistEndToEnd(t *testing.T) {
	fakeAudio(t)
	guardSignals(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "el-key")

	// A live device: some PCM, then waiting until killed.
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	mustWrite(t, ffmpeg, []byte("#!/bin/sh\nhead -c 12800 /dev/zero\nsleep 30\n"), 0755)
	recorder.Binary = ffmpeg

	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		mu.Lock()
		keys = append(keys, r.Header.Get("xi-api-key"))
		n := len(keys)
		mu.Unlock()
		ctx := r.Context()
		said := []string{
			"we ship the migration on monday unless the rollback plan is not ready by friday",
			"I will write the rollback runbook today and share it with the whole team tonight",
		}[(n-1)%2]
		var msg map[string]any
		if wsjson.Read(ctx, c, &msg) != nil {
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{"message_type": "committed_transcript", "text": said})
		for wsjson.Read(ctx, c, &msg) == nil {
		}
	}))
	defer srv.Close()
	origURL, origFlush := realtime.BaseURL, realtime.FlushTimeout
	realtime.BaseURL, realtime.FlushTimeout = "ws"+strings.TrimPrefix(srv.URL, "http"), 200*time.Millisecond
	defer func() { realtime.BaseURL, realtime.FlushTimeout = origURL, origFlush }()
	origTick := assistant.Tick
	assistant.Tick = 10 * time.Millisecond
	defer func() { assistant.Tick = origTick }()

	fakeClaude(t, `{"summary":"Shipping Monday; runbook by tonight.","decisions":["Ship on Monday"],"open_questions":[],"action_items":["me: write the rollback runbook"],"suggestions":[{"type":"question","text":"Who reviews the runbook?","why":"nobody named"}]}`)

	notes := filepath.Join(t.TempDir(), "standup.md")
	ctxFile := filepath.Join(t.TempDir(), "agenda.md")
	mustWrite(t, ctxFile, []byte("# Agenda\nMigration go/no-go."), 0644)

	out := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		done <- runAssist([]string{"-provider", "claude", "-context", ctxFile, "-o", notes, "-interval", "20ms"}, out, &syncBuf{})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "Ask: Who reviews the runbook?") {
		if time.Now().After(deadline) {
			t.Fatalf("no suggestion in time; output:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("assist did not stop after Ctrl+C:\n%s", out.String())
	}

	feed := out.String()
	for _, want := range []string{
		"Assist me+others · claude, updates every ≥20ms · context: 1 file(s), 1 KB → " + notes,
		"we ship the migration on monday", "I will write the rollback runbook",
		"✅ ", "Decision: Ship on Monday",
		"✓ Final notes written to " + notes,
	} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%s", want, feed)
		}
	}
	// Both sides connect at once, so which one gets which sentence varies;
	// each speaker must still get exactly one line.
	if strings.Count(feed, ":00 me     ") != 1 || strings.Count(feed, ":00 others ") != 1 {
		t.Errorf("one line per speaker expected:\n%s", feed)
	}
	b, _ := os.ReadFile(notes)
	for _, want := range []string{"## Final summary", "- me: write the rollback runbook", "❓ Ask: Who reviews the runbook?"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("notes missing %q:\n%s", want, b)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] != "el-key" {
		t.Errorf("one realtime session per side with the key; got %q", keys)
	}
}

func mustWrite(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}
