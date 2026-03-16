package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"goscribe/internal/worker"
	"goscribe/pkg/config"
)

type mockTranscriber struct {
	transcript string
	processed  map[string]string
	selected   []string
	err        error
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

func makeTask(t *testing.T, payload worker.ProcessPayload) *asynq.Task {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return asynq.NewTask(worker.TaskTypeProcess, b)
}

func skipIfNoRedis(t *testing.T, rdb *redis.Client) {
	t.Helper()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available at localhost:6379: %v", err)
	}
}

func newTestRDB(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	skipIfNoRedis(t, rdb)
	t.Cleanup(func() {
		rdb.FlushDB(context.Background())
		rdb.Close()
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
		Transcriber: tr,
		RDB:         rdb,
		Provider:    "openai",
		ResultTTL:   time.Hour,
		PostActions: []config.PostAction{action},
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
		Transcriber: tr,
		RDB:         rdb,
		Provider:    "openai",
		ResultTTL:   time.Hour,
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

func TestProcessTask_FiresWebhook(t *testing.T) {
	rdb := newTestRDB(t)

	webhookCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := &mockTranscriber{transcript: "hello"}
	proc := worker.NewProcessor(worker.Config{
		Transcriber: tr,
		RDB:         rdb,
		Provider:    "openai",
		ResultTTL:   time.Hour,
	})

	jobID := "job-webhook-test"
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
	if !webhookCalled {
		t.Error("expected webhook to be called after job completion")
	}
}
