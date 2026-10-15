// Package realtime streams audio to the ElevenLabs realtime speech-to-text API
// (Scribe v2 Realtime) over a WebSocket and reports partial and committed
// transcripts. It keeps one transcription going for as long as audio flows:
// it redials after session limits and network errors on its own.
package realtime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	Model      = "scribe_v2_realtime"
	SampleRate = 16000
)

// Overridable for tests.
var (
	BaseURL        = "wss://api.elevenlabs.io"
	FlushTimeout   = 3 * time.Second  // wait for the last transcript after the audio ends
	InitialBackoff = time.Second      // first wait before redialing after an error
	MaxBackoff     = 30 * time.Second // longest wait between redials
)

const (
	dialTimeout  = 10 * time.Second
	writeTimeout = 5 * time.Second
	// A session that lasted this long was healthy: the next error starts the
	// backoff from the beginning again.
	healthySession = time.Minute
	// loudPeak is the 16-bit sample level that counts as sound after the API
	// closed a session for lack of audio (about -30 dBFS).
	loudPeak = 1000
)

type Options struct {
	APIKey   string
	Language string   // e.g. "fr"; empty = auto-detect
	Keyterms []string // names and jargon to favor
	Verbatim bool     // keep filler words
}

type Kind int

const (
	Partial      Kind = iota // interim text of the current utterance; may change
	Committed                // final text of an utterance
	Reconnecting             // the session ended and a new one is being opened
	Warning                  // a non-fatal message from the API
)

type Event struct {
	Kind Kind
	Text string
}

// FatalError is an API error that retrying cannot fix (bad key, no quota...).
type FatalError struct {
	Type    string
	Message string
}

func (e *FatalError) Error() string {
	if e.Message == "" {
		return e.Type
	}
	return e.Type + ": " + e.Message
}

// URL is the WebSocket address with the session settings as query parameters.
func URL(o Options) string {
	q := url.Values{}
	q.Set("model_id", Model)
	q.Set("audio_format", fmt.Sprintf("pcm_%d", SampleRate))
	q.Set("commit_strategy", "vad")
	if o.Language != "" {
		q.Set("language_code", o.Language)
	}
	for _, k := range o.Keyterms {
		q.Add("keyterms", k)
	}
	if !o.Verbatim {
		q.Set("no_verbatim", "true")
	}
	return BaseURL + "/v1/speech-to-text/realtime?" + q.Encode()
}

// Stream sends 16 kHz mono s16le audio chunks to the API and calls emit for
// each transcript event, redialing as needed. It returns nil once audio is
// closed and the last transcript is in, ctx.Err() when ctx is cancelled, or a
// *FatalError. emit is called from Stream's goroutine.
func Stream(ctx context.Context, o Options, audio <-chan []byte, emit func(Event)) error {
	backoff := InitialBackoff
	var first []byte // chunk to send as soon as the next session opens
	for {
		start := time.Now()
		end, err := session(ctx, o, audio, first, emit)
		first = nil
		switch end {
		case endDone:
			return nil
		case endCancelled:
			return ctx.Err()
		case endFatal:
			return err
		case endSessionLimit:
			emit(Event{Kind: Reconnecting, Text: "session limit reached"})
			backoff = InitialBackoff
			continue
		case endInactive:
			// The API closes sessions that hear nothing; open a new one only when
			// there is sound again, so a muted side doesn't churn connections.
			emit(Event{Kind: Reconnecting, Text: "no sound, waiting"})
			for first == nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case b, ok := <-audio:
					if !ok {
						return nil
					}
					if isLoud(b) {
						first = b
					}
				}
			}
			backoff = InitialBackoff
			continue
		}
		// endRetry
		if time.Since(start) >= healthySession {
			backoff = InitialBackoff
		}
		emit(Event{Kind: Reconnecting, Text: fmt.Sprintf("%v; retrying in %s", err, backoff)})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, MaxBackoff)
	}
}

type ending int

const (
	endDone ending = iota
	endCancelled
	endFatal
	endSessionLimit
	endInactive
	endRetry
)

type message struct {
	MessageType string `json:"message_type"`
	Text        string `json:"text"`
	Warning     string `json:"warning"`
	Error       string `json:"error"`
}

type chunkMessage struct {
	MessageType string `json:"message_type"`
	Audio       string `json:"audio_base_64"`
	Commit      bool   `json:"commit"`
	SampleRate  int    `json:"sample_rate"`
}

// session runs one WebSocket connection until it ends, and says how.
func session(ctx context.Context, o Options, audio <-chan []byte, first []byte, emit func(Event)) (end ending, err error) {
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	c, resp, err := websocket.Dial(dctx, URL(o), &websocket.DialOptions{
		HTTPHeader: http.Header{"xi-api-key": {o.APIKey}},
	})
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return endCancelled, ctx.Err()
		}
		if resp != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return endFatal, &FatalError{Type: "auth_error", Message: resp.Status}
		}
		return endRetry, fmt.Errorf("connecting: %w", err)
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(1 << 20)

	rctx, stopReading := context.WithCancel(ctx)
	defer stopReading()
	msgs := make(chan message, 16)
	readErr := make(chan error, 1)
	go func() {
		for {
			var m message
			if err := wsjson.Read(rctx, c, &m); err != nil {
				readErr <- err
				return
			}
			select {
			case msgs <- m:
			case <-rctx.Done():
				return
			}
		}
	}()

	sendChunk := func(b []byte, commit bool) error {
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		return wsjson.Write(wctx, c, chunkMessage{
			MessageType: "input_audio_chunk",
			Audio:       base64.StdEncoding.EncodeToString(b),
			Commit:      commit,
			SampleRate:  SampleRate,
		})
	}
	closeNormally := func() { _ = c.Close(websocket.StatusNormalClosure, "") }

	if first != nil {
		if err := sendChunk(first, false); err != nil {
			return endRetry, fmt.Errorf("sending audio: %w", err)
		}
	}

	in := audio
	var flushed <-chan time.Time // set once the audio has ended
	// partial is the latest uncommitted text. If the audio ends before the API
	// commits it (it can answer the final commit with an empty transcript while
	// still processing), it becomes the last line instead of being lost.
	partial := ""
	defer func() {
		// A session cut short (limit, network) also keeps its last words.
		if partial != "" && (end == endRetry || end == endSessionLimit || end == endInactive) {
			emit(Event{Kind: Committed, Text: partial})
		}
	}()
	finish := func() (ending, error) {
		if partial != "" {
			emit(Event{Kind: Committed, Text: partial})
			partial = ""
		}
		closeNormally()
		return endDone, nil
	}
	for {
		select {
		case <-ctx.Done():
			closeNormally()
			return endCancelled, ctx.Err()

		case b, ok := <-in:
			if !ok {
				in = nil
				flushed = time.After(FlushTimeout)
				// Commit what the API has heard so far, then wait for its text.
				if err := sendChunk(nil, true); err != nil {
					return finish()
				}
				continue
			}
			if err := sendChunk(b, false); err != nil {
				if ctx.Err() != nil {
					return endCancelled, ctx.Err()
				}
				return endRetry, fmt.Errorf("sending audio: %w", err)
			}

		case m := <-msgs:
			switch m.MessageType {
			case "session_started":
			case "partial_transcript":
				if t := strings.TrimSpace(m.Text); t != "" {
					partial = t
					emit(Event{Kind: Partial, Text: t})
				}
			case "committed_transcript", "committed_transcript_with_timestamps":
				// The API also sends empty commits, e.g. after long silences.
				if t := strings.TrimSpace(m.Text); t != "" {
					partial = ""
					emit(Event{Kind: Committed, Text: t})
					if flushed != nil {
						closeNormally()
						return endDone, nil
					}
				}
			case "warning":
				emit(Event{Kind: Warning, Text: m.Warning})
			default:
				if end, err := classify(m); end != endDone {
					closeNormally()
					return end, err
				}
			}

		case err := <-readErr:
			if flushed != nil {
				return finish()
			}
			if ctx.Err() != nil {
				return endCancelled, ctx.Err()
			}
			return endRetry, fmt.Errorf("connection lost: %w", err)

		case <-flushed:
			return finish()
		}
	}
}

// classify maps an API message that isn't a transcript to how the session
// ends; endDone means "not an error, keep going".
func classify(m message) (ending, error) {
	detail := m.Error
	switch m.MessageType {
	case "auth_error", "quota_exceeded", "unaccepted_terms", "input_error", "invalid_request", "chunk_size_exceeded":
		return endFatal, &FatalError{Type: m.MessageType, Message: detail}
	case "session_time_limit_exceeded":
		return endSessionLimit, nil
	case "insufficient_audio_activity":
		return endInactive, nil
	case "commit_throttled":
		return endDone, nil
	}
	if detail == "" && !strings.Contains(m.MessageType, "error") && !strings.HasSuffix(m.MessageType, "_exceeded") &&
		m.MessageType != "rate_limited" && m.MessageType != "queue_overflow" && m.MessageType != "resource_exhausted" {
		return endDone, nil // unknown informational message
	}
	if detail == "" {
		detail = m.MessageType
	}
	return endRetry, errors.New(m.MessageType + ": " + detail)
}

// isLoud reports whether a chunk of 16-bit little-endian samples has a peak
// at or above loudPeak.
func isLoud(pcm []byte) bool {
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8)
		if s >= loudPeak || s <= -loudPeak {
			return true
		}
	}
	return false
}
