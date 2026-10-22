package elevenlabs

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func at(seconds float64) *float64 { return &seconds }

func overrideBaseURL(t *testing.T, url string) {
	t.Helper()
	orig := BaseURL
	BaseURL = url
	t.Cleanup(func() { BaseURL = orig })
}

func writeAudio(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meeting.mp3")
	if err := os.WriteFile(path, []byte("fake audio"), 0644); err != nil {
		t.Fatalf("writing audio: %v", err)
	}
	return path
}

func TestTranscribeSendsRequest(t *testing.T) {
	var gotKey, gotPath string
	fields := map[string]string{}
	var gotFile string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("xi-api-key")
		gotPath = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parsing multipart: %v", err)
		}
		for k, v := range r.MultipartForm.Value {
			fields[k] = v[0]
		}
		f, hdr, err := r.FormFile("file")
		if err == nil {
			data, _ := io.ReadAll(f)
			gotFile = hdr.Filename + ":" + string(data)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "plain text"})
	}))
	defer server.Close()
	overrideBaseURL(t, server.URL)

	got, err := Transcribe(writeAudio(t), Options{APIKey: "secret", Language: "fr"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain text" {
		t.Errorf("transcript = %q, want %q", got, "plain text")
	}
	if gotKey != "secret" {
		t.Errorf("xi-api-key = %q", gotKey)
	}
	if gotPath != "/v1/speech-to-text" {
		t.Errorf("path = %q", gotPath)
	}
	want := map[string]string{
		"model_id": "scribe_v2", "diarize": "true", "language_code": "fr",
		"no_verbatim": "true", "tag_audio_events": "false", "timestamps_granularity": "word",
	}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("field %s = %q, want %q (all: %v)", k, fields[k], v, fields)
		}
	}
	for _, unset := range []string{"num_speakers", "keyterms"} {
		if _, ok := fields[unset]; ok {
			t.Errorf("field %s should not be sent by default", unset)
		}
	}
	if gotFile != "meeting.mp3:fake audio" {
		t.Errorf("file = %q", gotFile)
	}
}

func TestTranscribeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid api key"}`))
	}))
	defer server.Close()
	overrideBaseURL(t, server.URL)

	_, err := Transcribe(writeAudio(t), Options{APIKey: "bad"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %v, want status and body", err)
	}
}

func TestTranscribeMissingKey(t *testing.T) {
	if _, err := Transcribe(writeAudio(t), Options{}); err == nil {
		t.Error("expected error without API key")
	}
}

func TestFormFields(t *testing.T) {
	values := func(o Options) map[string][]string {
		m := map[string][]string{}
		for _, f := range formFields(o) {
			m[f[0]] = append(m[f[0]], f[1])
		}
		return m
	}

	got := values(Options{Keyterms: []string{"Acme", " Globex ", ""}, NumSpeakers: 4})
	if strings.Join(got["keyterms"], "|") != "Acme|Globex" {
		t.Errorf("keyterms = %v, want one trimmed field per non-empty term", got["keyterms"])
	}
	if strings.Join(got["num_speakers"], "") != "4" {
		t.Errorf("num_speakers = %v", got["num_speakers"])
	}

	if _, ok := values(Options{Verbatim: true})["no_verbatim"]; ok {
		t.Error("no_verbatim sent although Verbatim is set")
	}
	v1 := values(Options{Model: "scribe_v1"})
	if _, ok := v1["no_verbatim"]; ok {
		t.Error("no_verbatim sent to scribe_v1, which rejects it")
	}
	if v1["model_id"][0] != "scribe_v1" {
		t.Errorf("model_id = %v", v1["model_id"])
	}
}

func TestFormatTimestamp(t *testing.T) {
	for in, want := range map[float64]string{0: "00:00:00", 61.9: "00:01:01", 3725.2: "01:02:05"} {
		if got := formatTimestamp(in); got != want {
			t.Errorf("formatTimestamp(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTranscript(t *testing.T) {
	tests := []struct {
		name string
		resp transcriptionResponse
		want string
	}{
		{
			name: "groups consecutive words by speaker",
			resp: transcriptionResponse{
				Text: "Hello there. Hi! (laughs) Bye.",
				Words: []word{
					{Text: "Hello", Type: "word", Start: at(1.5), SpeakerID: "speaker_0"},
					{Text: " ", Type: "spacing", Start: at(1.9), SpeakerID: "speaker_0"},
					{Text: "there.", Type: "word", Start: at(2), SpeakerID: "speaker_0"},
					{Text: " ", Type: "spacing", Start: at(2.4), SpeakerID: "speaker_0"},
					{Text: "Hi!", Type: "word", Start: at(65), SpeakerID: "speaker_1"},
					{Text: "(laughs)", Type: "audio_event", Start: at(66), SpeakerID: "speaker_1"},
					{Text: " ", Type: "spacing", Start: at(3699), SpeakerID: "speaker_0"},
					{Text: "Bye.", Type: "word", Start: at(3700), SpeakerID: "speaker_0"},
				},
			},
			// A turn's timestamp is its first word's, not the leading spacing's.
			want: "[00:00:01] speaker_0: Hello there.\n\n[00:01:05] speaker_1: Hi!\n\n[01:01:40] speaker_0: Bye.",
		},
		{
			name: "no timestamp when start times are missing",
			resp: transcriptionResponse{
				Words: []word{{Text: "Hello", Type: "word", SpeakerID: "speaker_0"}},
			},
			want: "speaker_0: Hello",
		},
		{
			name: "falls back to text without speakers",
			resp: transcriptionResponse{
				Text:  " just text ",
				Words: []word{{Text: "just", Type: "word"}},
			},
			want: "just text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTranscript(tt.resp); got != tt.want {
				t.Errorf("formatTranscript() = %q, want %q", got, tt.want)
			}
		})
	}
}

// An exhausted quota is recognizable, so callers can fall back.
func TestTranscribeQuotaExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":{"status":"quota_exceeded","message":"You have exceeded your quota."}}`))
	}))
	defer server.Close()
	overrideBaseURL(t, server.URL)

	_, err := Transcribe(writeAudio(t), Options{APIKey: "k"})
	if !errors.Is(err, ErrQuota) || !strings.Contains(err.Error(), "exceeded your quota") {
		t.Errorf("err = %v, want ErrQuota with the API message", err)
	}
	// Other errors are not quota errors.
	if errors.Is(errors.New("ElevenLabs API error (status 401): invalid api key"), ErrQuota) {
		t.Error("only quota errors match ErrQuota")
	}
}
