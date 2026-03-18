package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"goscribe/internal/worker"
	"goscribe/pkg/config"
	"goscribe/pkg/lyrics"
)

type mockTranscriber struct {
	transcript  string
	processed   map[string]string
	selected    []string
	err         error
	validation  *lyrics.LyricsValidation
	validateErr error
}

func (m *mockTranscriber) TranscribeAudio(audioPath, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (string, error) {
	return m.transcript, m.err
}

func (m *mockTranscriber) ProcessChunked(transcript string, action *config.PostAction, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.processed[action.ID], nil
}

func (m *mockTranscriber) SelectBestActions(transcript string, actions []config.PostAction, prov, openaiKey, geminiKey, geminiModel string) ([]string, error) {
	return m.selected, m.err
}

func (m *mockTranscriber) ValidateLyrics(transcript, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (*lyrics.LyricsValidation, error) {
	return m.validation, m.validateErr
}

func makeTask(t *testing.T, payload worker.ProcessPayload) *asynq.Task {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return asynq.NewTask(worker.TaskTypeProcess, b)
}

func newTestRDB(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		rdb.Close()
		mr.Close()
	})
	return rdb
}

func TestProcessTask_TranscriptMode(t *testing.T) {
	rdb := newTestRDB(t)
	action := config.PostAction{ID: "test-action", Name: "Test", Prompt: "Summarize", Type: "openai", Model: "gpt-3.5-turbo"}
	tr := &mockTranscriber{
		transcript: "hello world",
		processed:  map[string]string{"test-action": "summary text"},
	}
	proc := worker.NewProcessor(worker.Config{
		Transcriber:    tr,
		RDB:            rdb,
		Provider:       "openai",
		ResultTTL:      time.Hour,
		PostActions:    []config.PostAction{action},
		EnableFallback: true,
	})

	jobID := "job-transcript-test"
	initial := worker.JobResult{JobID: jobID, Status: worker.StatusQueued, CreatedAt: time.Now()}
	b, _ := json.Marshal(initial)
	rdb.Set(context.Background(), worker.ResultKeyPrefix+jobID, b, time.Hour)

	task := makeTask(t, worker.ProcessPayload{
		JobID:      jobID,
		Transcript: "hello world",
		Actions:    "test-action",
		Provider:   "openai",
	})

	if err := proc.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	val, err := rdb.Get(context.Background(), worker.ResultKeyPrefix+jobID).Result()
	if err != nil {
		t.Fatalf("get result from Redis: %v", err)
	}
	var result worker.JobResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Status != worker.StatusCompleted {
		t.Errorf("status: got %q, want %q", result.Status, worker.StatusCompleted)
	}
	if result.Results["test-action"] != "summary text" {
		t.Errorf("result: got %q, want %q", result.Results["test-action"], "summary text")
	}
}

func TestProcessTask_DeletesTempFile(t *testing.T) {
	rdb := newTestRDB(t)

	tmpFile := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(tmpFile, []byte("fake audio"), 0644); err != nil {
		t.Fatal(err)
	}

	tr := &mockTranscriber{transcript: "transcribed"}
	proc := worker.NewProcessor(worker.Config{
		Transcriber:    tr,
		RDB:            rdb,
		Provider:       "openai",
		ResultTTL:      time.Hour,
		EnableFallback: true,
	})

	jobID := "job-file-cleanup"
	initial := worker.JobResult{JobID: jobID, Status: worker.StatusQueued, CreatedAt: time.Now()}
	b, _ := json.Marshal(initial)
	rdb.Set(context.Background(), worker.ResultKeyPrefix+jobID, b, time.Hour)

	task := makeTask(t, worker.ProcessPayload{JobID: jobID, AudioPath: tmpFile, Provider: "openai"})

	if err := proc.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("expected temp audio file to be deleted after processing")
	}
}

func TestProcessTask_WebhookBlockedBySSRF(t *testing.T) {
	rdb := newTestRDB(t)

	webhookCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := &mockTranscriber{transcript: "hello"}
	proc := worker.NewProcessor(worker.Config{
		Transcriber:    tr,
		RDB:            rdb,
		Provider:       "openai",
		ResultTTL:      time.Hour,
		EnableFallback: true,
	})

	jobID := "job-webhook-blocked"
	initial := worker.JobResult{JobID: jobID, Status: worker.StatusQueued, CreatedAt: time.Now()}
	b, _ := json.Marshal(initial)
	rdb.Set(context.Background(), worker.ResultKeyPrefix+jobID, b, time.Hour)

	task := makeTask(t, worker.ProcessPayload{
		JobID:      jobID,
		Transcript: "hello",
		Provider:   "openai",
		WebhookURL: srv.URL,
	})

	if err := proc.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if webhookCalled {
		t.Error("webhook should be blocked by SSRF protection for localhost")
	}
}

func TestProcessTask_SongMode_Success(t *testing.T) {
	rdb := newTestRDB(t)
	canned := &lyrics.LyricsValidation{CoherenceScore: 85, Confidence: 0.9, IsPlausibleSong: true}

	fakeVocals := filepath.Join(t.TempDir(), "vocals.wav")
	if err := os.WriteFile(fakeVocals, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	cleanupCalled := false

	tr := &mockTranscriber{
		transcript: "I will always love you",
		validation: canned,
		processed:  map[string]string{},
	}
	proc := worker.NewProcessor(worker.Config{
		Transcriber:    tr,
		RDB:            rdb,
		Provider:       "openai",
		ResultTTL:      time.Hour,
		EnableFallback: true,
		VocalExtractor: func(audioPath string) (string, func(), error) {
			return fakeVocals, func() { cleanupCalled = true }, nil
		},
	})

	jobID := "song-job-1"
	initial := worker.JobResult{JobID: jobID, Status: worker.StatusQueued, CreatedAt: time.Now()}
	b, _ := json.Marshal(initial)
	rdb.Set(context.Background(), worker.ResultKeyPrefix+jobID, b, time.Hour)

	audioFile := filepath.Join(t.TempDir(), "song.mp3")
	os.WriteFile(audioFile, []byte("audio"), 0644)

	task := makeTask(t, worker.ProcessPayload{
		JobID:     jobID,
		AudioPath: audioFile,
		Provider:  "openai",
		Song:      true,
	})

	if err := proc.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}
	if !cleanupCalled {
		t.Error("VocalExtractor cleanup was not called")
	}

	val, _ := rdb.Get(context.Background(), worker.ResultKeyPrefix+jobID).Result()
	var result worker.JobResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		t.Fatal(err)
	}

	if result.Status != worker.StatusCompleted {
		t.Errorf("status: got %q, want completed", result.Status)
	}
	if result.LyricsValidation == nil {
		t.Fatal("LyricsValidation is nil")
	}
	if result.LyricsValidation.CoherenceScore != 85 {
		t.Errorf("CoherenceScore: got %v, want 85", result.LyricsValidation.CoherenceScore)
	}
}

func TestProcessTask_SongMode_NoAudioPath(t *testing.T) {
	rdb := newTestRDB(t)
	tr := &mockTranscriber{}
	proc := worker.NewProcessor(worker.Config{
		Transcriber: tr, RDB: rdb, Provider: "openai", ResultTTL: time.Hour,
	})

	jobID := "song-no-audio"
	initial := worker.JobResult{JobID: jobID, Status: worker.StatusQueued}
	b, _ := json.Marshal(initial)
	rdb.Set(context.Background(), worker.ResultKeyPrefix+jobID, b, time.Hour)

	task := makeTask(t, worker.ProcessPayload{
		JobID:    jobID,
		Provider: "openai",
		Song:     true,
	})

	proc.ProcessTask(context.Background(), task)

	val, _ := rdb.Get(context.Background(), worker.ResultKeyPrefix+jobID).Result()
	var result worker.JobResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != worker.StatusFailed {
		t.Errorf("status: got %q, want failed", result.Status)
	}
}

func TestProcessTask_SongMode_ValidationError_JobSucceeds(t *testing.T) {
	rdb := newTestRDB(t)
	fakeVocals := filepath.Join(t.TempDir(), "vocals.wav")
	if err := os.WriteFile(fakeVocals, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	tr := &mockTranscriber{
		transcript:  "some lyrics",
		validateErr: fmt.Errorf("API down"),
		processed:   map[string]string{},
	}
	proc := worker.NewProcessor(worker.Config{
		Transcriber: tr, RDB: rdb, Provider: "openai", ResultTTL: time.Hour,
		VocalExtractor: func(s string) (string, func(), error) {
			return fakeVocals, func() {}, nil
		},
	})

	jobID := "song-val-err"
	initial := worker.JobResult{JobID: jobID, Status: worker.StatusQueued}
	b, _ := json.Marshal(initial)
	rdb.Set(context.Background(), worker.ResultKeyPrefix+jobID, b, time.Hour)

	audioFile := filepath.Join(t.TempDir(), "song.mp3")
	os.WriteFile(audioFile, []byte("audio"), 0644)
	task := makeTask(t, worker.ProcessPayload{
		JobID: jobID, AudioPath: audioFile, Provider: "openai", Song: true,
	})

	if err := proc.ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask: %v", err)
	}

	val, _ := rdb.Get(context.Background(), worker.ResultKeyPrefix+jobID).Result()
	var result worker.JobResult
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != worker.StatusCompleted {
		t.Errorf("status: got %q, want completed", result.Status)
	}
	if result.LyricsValidation != nil {
		t.Error("expected nil LyricsValidation when validation returns error")
	}
}
