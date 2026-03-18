package worker_test

import (
	"encoding/json"
	"testing"
	"time"

	"goscribe/internal/worker"
	"goscribe/pkg/lyrics"
)

func TestProcessPayloadRoundtrip(t *testing.T) {
	payload := worker.ProcessPayload{
		JobID:      "test-123",
		AudioPath:  "/tmp/audio.mp3",
		Actions:    "openai-meeting-summary",
		Provider:   "openai",
		WebhookURL: "https://example.com/cb",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got worker.ProcessPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.JobID != payload.JobID {
		t.Errorf("JobID: got %q, want %q", got.JobID, payload.JobID)
	}
	if got.WebhookURL != payload.WebhookURL {
		t.Errorf("WebhookURL: got %q, want %q", got.WebhookURL, payload.WebhookURL)
	}
}

func TestStatusConstants(t *testing.T) {
	for _, s := range []string{
		worker.StatusQueued,
		worker.StatusProcessing,
		worker.StatusCompleted,
		worker.StatusFailed,
	} {
		if s == "" {
			t.Error("status constant must not be empty string")
		}
	}
}

func TestJobResultJSON(t *testing.T) {
	now := time.Now()
	r := worker.JobResult{
		JobID:      "abc",
		Status:     worker.StatusCompleted,
		Transcript: "hello world",
		Results:    map[string]string{"action-1": "summary"},
		CreatedAt:  now,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(b) == 0 {
		t.Error("expected non-empty JSON")
	}
}

func TestProcessPayload_SongField(t *testing.T) {
	p := worker.ProcessPayload{JobID: "x", Song: true}
	b, _ := json.Marshal(p)
	var got worker.ProcessPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Song {
		t.Error("Song field lost in JSON round-trip")
	}
}

func TestJobResult_LyricsValidation(t *testing.T) {
	lv := &lyrics.LyricsValidation{CoherenceScore: 80, Confidence: 0.9}
	r := worker.JobResult{JobID: "x", LyricsValidation: lv}
	b, _ := json.Marshal(r)
	var got worker.JobResult
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.LyricsValidation == nil {
		t.Fatal("LyricsValidation is nil after round-trip")
	}
	if got.LyricsValidation.CoherenceScore != 80 {
		t.Errorf("CoherenceScore: got %v, want 80", got.LyricsValidation.CoherenceScore)
	}
}
