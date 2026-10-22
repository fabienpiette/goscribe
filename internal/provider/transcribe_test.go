package provider

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/internal/elevenlabs"
	"goscribe/internal/whisperlocal"
)

func quotaServer(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":{"status":"quota_exceeded","message":"You have exceeded your quota."}}`))
	}))
	t.Cleanup(server.Close)
	orig := elevenlabs.BaseURL
	elevenlabs.BaseURL = server.URL
	t.Cleanup(func() { elevenlabs.BaseURL = orig })
}

func audio(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meeting.mp3")
	if err := os.WriteFile(path, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// When ElevenLabs is out of quota, the local engine transcribes instead.
func TestTranscribeFallsBackToLocalOnQuota(t *testing.T) {
	quotaServer(t)
	bin := filepath.Join(t.TempDir(), "fake-whisper")
	script := `#!/bin/sh
audio="$1"
while [ $# -gt 0 ]; do [ "$1" = "--output_dir" ] && out="$2"; shift; done
base=$(basename "$audio"); echo "local words" > "$out/${base%.*}.txt"
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	orig := whisperlocal.Binary
	whisperlocal.Binary = bin
	t.Cleanup(func() { whisperlocal.Binary = orig })
	var notice bytes.Buffer
	origOut := Notices
	Notices = &notice
	t.Cleanup(func() { Notices = origOut })

	got, err := Transcribe(audio(t), TranscribeOptions{Transcriber: "elevenlabs", ElevenLabs: elevenlabs.Options{APIKey: "k"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "local words" {
		t.Errorf("transcript = %q, want the local engine's", got)
	}
	if !strings.Contains(notice.String(), "ElevenLabs quota exceeded: transcribing locally") {
		t.Errorf("notice = %q", notice.String())
	}
}

// Without the local engine, both causes are reported.
func TestTranscribeQuotaWithoutLocalEngine(t *testing.T) {
	quotaServer(t)
	orig := whisperlocal.Binary
	whisperlocal.Binary = "goscribe-no-such-whisper"
	t.Cleanup(func() { whisperlocal.Binary = orig })
	Notices = &bytes.Buffer{}
	_, err := Transcribe(audio(t), TranscribeOptions{Transcriber: "elevenlabs", ElevenLabs: elevenlabs.Options{APIKey: "k"}})
	if err == nil || !strings.Contains(err.Error(), "exceeded your quota") || !strings.Contains(err.Error(), "goscribe-no-such-whisper") {
		t.Errorf("err = %v, want the quota error and the local engine's", err)
	}
}
