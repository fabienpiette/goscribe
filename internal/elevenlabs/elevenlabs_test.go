package elevenlabs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	got, err := Transcribe(writeAudio(t), "secret", "", "fr")
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
	if fields["model_id"] != DefaultModel || fields["diarize"] != "true" || fields["language_code"] != "fr" {
		t.Errorf("fields = %v", fields)
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

	_, err := Transcribe(writeAudio(t), "bad", "", "")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %v, want status and body", err)
	}
}

func TestTranscribeMissingKey(t *testing.T) {
	if _, err := Transcribe(writeAudio(t), "", "", ""); err == nil {
		t.Error("expected error without API key")
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
					{Text: "Hello", Type: "word", SpeakerID: "speaker_0"},
					{Text: " ", Type: "spacing", SpeakerID: "speaker_0"},
					{Text: "there.", Type: "word", SpeakerID: "speaker_0"},
					{Text: " ", Type: "spacing", SpeakerID: "speaker_0"},
					{Text: "Hi!", Type: "word", SpeakerID: "speaker_1"},
					{Text: "(laughs)", Type: "audio_event", SpeakerID: "speaker_1"},
					{Text: " ", Type: "spacing", SpeakerID: "speaker_0"},
					{Text: "Bye.", Type: "word", SpeakerID: "speaker_0"},
				},
			},
			want: "speaker_0: Hello there.\n\nspeaker_1: Hi!\n\nspeaker_0: Bye.",
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
