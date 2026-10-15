package realtime

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// session is what the fake server saw on one connection.
type fakeSession struct {
	header http.Header
	query  url.Values
	mu     sync.Mutex
	chunks []chunk
}

func (s *fakeSession) received() []chunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]chunk(nil), s.chunks...)
}

type chunk struct {
	MessageType string `json:"message_type"`
	Audio       string `json:"audio_base_64"`
	Commit      bool   `json:"commit"`
	SampleRate  int    `json:"sample_rate"`
}

// fakeServer runs script for each connection, in order; extra connections
// reuse the last script. It records every session.
type fakeServer struct {
	t        *testing.T
	mu       sync.Mutex
	sessions []*fakeSession
	scripts  []func(ctx context.Context, c *websocket.Conn, s *fakeSession)
}

func (f *fakeServer) start() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		s := &fakeSession{header: r.Header.Clone(), query: r.URL.Query()}
		f.mu.Lock()
		f.sessions = append(f.sessions, s)
		script := f.scripts[min(len(f.sessions), len(f.scripts))-1]
		f.mu.Unlock()
		script(r.Context(), c, s)
	}))
	f.t.Cleanup(srv.Close)
	orig, origFlush, origBackoff := BaseURL, FlushTimeout, InitialBackoff
	BaseURL, FlushTimeout, InitialBackoff = "ws"+strings.TrimPrefix(srv.URL, "http"), 500*time.Millisecond, 10*time.Millisecond
	f.t.Cleanup(func() { BaseURL, FlushTimeout, InitialBackoff = orig, origFlush, origBackoff })
}

func (f *fakeServer) all() []*fakeSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeSession(nil), f.sessions...)
}

func send(ctx context.Context, c *websocket.Conn, msg map[string]any) {
	_ = wsjson.Write(ctx, c, msg)
}

// readChunk reads one client message into s and returns it.
func readChunk(ctx context.Context, c *websocket.Conn, s *fakeSession) (chunk, bool) {
	var ch chunk
	if err := wsjson.Read(ctx, c, &ch); err != nil {
		return ch, false
	}
	s.mu.Lock()
	s.chunks = append(s.chunks, ch)
	s.mu.Unlock()
	return ch, true
}

// collector gathers emitted events safely.
type collector struct {
	mu     sync.Mutex
	events []Event
}

func (c *collector) emit(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *collector) texts(k Kind) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.events {
		if e.Kind == k {
			out = append(out, e.Text)
		}
	}
	return out
}

func loudChunk() []byte {
	b := make([]byte, 64)
	for i := 0; i < len(b); i += 2 {
		b[i], b[i+1] = 0xff, 0x3f // 16383, little-endian
	}
	return b
}

func TestURL(t *testing.T) {
	u, err := url.Parse(URL(Options{Language: "fr", Keyterms: []string{"Acme", "Globex"}}))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/v1/speech-to-text/realtime" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"model_id": "scribe_v2_realtime", "audio_format": "pcm_16000", "commit_strategy": "vad",
		"language_code": "fr", "no_verbatim": "true",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if got := q["keyterms"]; strings.Join(got, ",") != "Acme,Globex" {
		t.Errorf("keyterms = %v", got)
	}

	q = mustQuery(t, URL(Options{Verbatim: true}))
	if q.Has("language_code") || q.Has("keyterms") || q.Has("no_verbatim") {
		t.Errorf("unset options must not appear: %v", q)
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestStreamTranscribesAndFlushes(t *testing.T) {
	f := &fakeServer{t: t}
	f.scripts = append(f.scripts, func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
		send(ctx, c, map[string]any{"message_type": "session_started", "session_id": "s1"})
		readChunk(ctx, c, s)
		send(ctx, c, map[string]any{"message_type": "partial_transcript", "text": "hello wor"})
		send(ctx, c, map[string]any{"message_type": "committed_transcript", "text": "hello world"})
		send(ctx, c, map[string]any{"message_type": "committed_transcript", "text": "   "}) // ignored
		for {
			ch, ok := readChunk(ctx, c, s)
			if !ok {
				return
			}
			if ch.Commit {
				send(ctx, c, map[string]any{"message_type": "committed_transcript", "text": "last words"})
			}
		}
	})
	f.start()

	audio := make(chan []byte, 4)
	audio <- []byte{1, 2, 3, 4}
	var got collector
	done := make(chan error, 1)
	go func() { done <- Stream(context.Background(), Options{APIKey: "k-123"}, audio, got.emit) }()

	waitFor(t, func() bool { return len(got.texts(Committed)) == 1 })
	close(audio)
	if err := <-done; err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if c := got.texts(Committed); strings.Join(c, "|") != "hello world|last words" {
		t.Errorf("committed = %q", c)
	}
	if p := got.texts(Partial); strings.Join(p, "|") != "hello wor" {
		t.Errorf("partials = %q", p)
	}
	s := f.all()[0]
	chunks := s.received()
	if s.header.Get("xi-api-key") != "k-123" {
		t.Errorf("xi-api-key header = %q", s.header.Get("xi-api-key"))
	}
	first := chunks[0]
	if first.MessageType != "input_audio_chunk" || first.Commit || first.SampleRate != 16000 {
		t.Errorf("first chunk = %+v", first)
	}
	if raw, _ := base64.StdEncoding.DecodeString(first.Audio); string(raw) != "\x01\x02\x03\x04" {
		t.Errorf("audio round-trip = %v", raw)
	}
	if last := chunks[len(chunks)-1]; !last.Commit || last.Audio != "" {
		t.Errorf("closing audio must send an empty committing chunk, got %+v", last)
	}
}

func TestStreamReconnectsAfterSessionLimit(t *testing.T) {
	f := &fakeServer{t: t}
	f.scripts = append(f.scripts,
		func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
			readChunk(ctx, c, s)
			send(ctx, c, map[string]any{"message_type": "partial_transcript", "text": "cut short"})
			send(ctx, c, map[string]any{"message_type": "session_time_limit_exceeded", "error": "limit"})
		},
		func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
			for {
				ch, ok := readChunk(ctx, c, s)
				if !ok {
					return
				}
				if string(mustDecode(t, ch.Audio)) == "second" {
					send(ctx, c, map[string]any{"message_type": "committed_transcript", "text": "after reconnect"})
				}
			}
		})
	f.start()

	audio := make(chan []byte, 4)
	audio <- []byte("first")
	var got collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Stream(ctx, Options{APIKey: "k"}, audio, got.emit) }()

	waitFor(t, func() bool { return len(got.texts(Reconnecting)) == 1 })
	audio <- []byte("second") // buffered while redialing, delivered to the new session
	waitFor(t, func() bool { return len(got.texts(Committed)) == 2 })
	if c := got.texts(Committed); strings.Join(c, "|") != "cut short|after reconnect" {
		t.Errorf("committed = %q: the words in progress when the session ended must be kept", c)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Stream after cancel = %v, want context.Canceled", err)
	}
	if n := len(f.all()); n != 2 {
		t.Errorf("connections = %d, want 2", n)
	}
}

func TestStreamFatalErrorStops(t *testing.T) {
	f := &fakeServer{t: t}
	f.scripts = append(f.scripts, func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
		send(ctx, c, map[string]any{"message_type": "auth_error", "error": "invalid api key"})
		time.Sleep(200 * time.Millisecond)
	})
	f.start()

	audio := make(chan []byte)
	err := Stream(context.Background(), Options{APIKey: "bad"}, audio, (&collector{}).emit)
	var fatal *FatalError
	if !errors.As(err, &fatal) || fatal.Type != "auth_error" || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("err = %v, want FatalError auth_error", err)
	}
	if n := len(f.all()); n != 1 {
		t.Errorf("a fatal error must not redial: %d connections", n)
	}
}

func TestStreamWaitsForSoundAfterInactivity(t *testing.T) {
	f := &fakeServer{t: t}
	f.scripts = append(f.scripts,
		func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
			send(ctx, c, map[string]any{"message_type": "insufficient_audio_activity", "error": "silence"})
		},
		func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
			for {
				if _, ok := readChunk(ctx, c, s); !ok {
					return
				}
			}
		})
	f.start()

	audio := make(chan []byte, 8)
	var got collector
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Stream(ctx, Options{APIKey: "k"}, audio, got.emit) }()

	waitFor(t, func() bool { return len(f.all()) == 1 })
	audio <- make([]byte, 64) // silence: no redial
	time.Sleep(150 * time.Millisecond)
	if n := len(f.all()); n != 1 {
		t.Fatalf("silence must not redial: %d connections", n)
	}
	audio <- loudChunk()
	waitFor(t, func() bool {
		s := f.all()
		return len(s) == 2 && len(s[1].received()) == 1
	})
	cancel()
	<-done
}

func TestStreamCancelReturnsPromptly(t *testing.T) {
	f := &fakeServer{t: t}
	f.scripts = append(f.scripts, func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
		<-ctx.Done()
	})
	f.start()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Stream(ctx, Options{APIKey: "k"}, make(chan []byte), (&collector{}).emit) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return after cancel")
	}
}

func TestIsLoud(t *testing.T) {
	if isLoud(make([]byte, 64)) {
		t.Error("silence is not loud")
	}
	if !isLoud(loudChunk()) {
		t.Error("half-scale tone is loud")
	}
	neg := []byte{0x01, 0xc0} // -16383
	if !isLoud(neg) {
		t.Error("negative samples count too")
	}
}

func mustDecode(t *testing.T, s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Seen on the real API: committing right after speech can return an empty
// transcript while the words are still being processed. The last partial must
// not be lost.
func TestStreamFlushKeepsLastPartial(t *testing.T) {
	f := &fakeServer{t: t}
	f.scripts = append(f.scripts, func(ctx context.Context, c *websocket.Conn, s *fakeSession) {
		readChunk(ctx, c, s)
		send(ctx, c, map[string]any{"message_type": "partial_transcript", "text": "and the last"})
		send(ctx, c, map[string]any{"message_type": "partial_transcript", "text": "and the last words"})
		for {
			ch, ok := readChunk(ctx, c, s)
			if !ok {
				return
			}
			if ch.Commit {
				send(ctx, c, map[string]any{"message_type": "committed_transcript", "text": ""})
			}
		}
	})
	f.start()

	audio := make(chan []byte, 1)
	audio <- []byte{1, 2}
	var got collector
	done := make(chan error, 1)
	go func() { done <- Stream(context.Background(), Options{APIKey: "k"}, audio, got.emit) }()
	waitFor(t, func() bool { return len(got.texts(Partial)) == 2 })
	close(audio)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if c := got.texts(Committed); strings.Join(c, "|") != "and the last words" {
		t.Errorf("committed = %q, want the last partial", c)
	}
}
