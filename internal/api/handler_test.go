package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"goscribe/internal/api"
	"goscribe/internal/worker"
	"goscribe/pkg/config"
)

type mockEnqueuer struct {
	enqueued []*asynq.Task
	err      error
}

func (m *mockEnqueuer) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	m.enqueued = append(m.enqueued, task)
	return &asynq.TaskInfo{ID: "mock-task-id"}, m.err
}

func skipIfNoRedis(t *testing.T, rdb *redis.Client) {
	t.Helper()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
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

func newTestHandler(t *testing.T, enq api.JobEnqueuer, rdb *redis.Client, actions []config.PostAction) *api.Handler {
	t.Helper()
	return api.NewHandler(api.HandlerConfig{
		Enqueuer:        enq,
		RDB:             rdb,
		PostActions:     actions,
		ResultTTL:       time.Hour,
		MaxUploadBytes:  10 << 20,
		UploadsDir:      t.TempDir(),
		DefaultProvider: "openai",
	})
}

func multipartForm(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	w.Close()
	return body, w.FormDataContentType()
}

func TestSubmitJob_TranscriptText(t *testing.T) {
	rdb := newTestRDB(t)
	enq := &mockEnqueuer{}
	h := newTestHandler(t, enq, rdb, nil)

	body, ct := multipartForm(t, map[string]string{
		"transcript": "hello world",
		"actions":    "test-action",
	})
	req := httptest.NewRequest(http.MethodPost, "/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h.SubmitJob(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202; body: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["job_id"] == "" {
		t.Error("expected non-empty job_id in response")
	}
	if resp["status"] != worker.StatusQueued {
		t.Errorf("status: got %q, want %q", resp["status"], worker.StatusQueued)
	}
	if len(enq.enqueued) != 1 {
		t.Errorf("expected 1 enqueued task, got %d", len(enq.enqueued))
	}
}

func TestSubmitJob_MissingBothInputs(t *testing.T) {
	rdb := newTestRDB(t)
	h := newTestHandler(t, &mockEnqueuer{}, rdb, nil)

	body, ct := multipartForm(t, map[string]string{})
	req := httptest.NewRequest(http.MethodPost, "/jobs", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()

	h.SubmitJob(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	rdb := newTestRDB(t)
	h := newTestHandler(t, &mockEnqueuer{}, rdb, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	req := httptest.NewRequest(http.MethodGet, "/jobs/nonexistent", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.GetJob(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}

func TestListActions(t *testing.T) {
	actions := []config.PostAction{{ID: "action-1", Name: "Action One", Description: "desc"}}
	h := newTestHandler(t, &mockEnqueuer{}, nil, actions)

	req := httptest.NewRequest(http.MethodGet, "/actions", nil)
	rec := httptest.NewRecorder()
	h.ListActions(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	var got []config.PostAction
	json.NewDecoder(rec.Body).Decode(&got)
	if len(got) != 1 || got[0].ID != "action-1" {
		t.Errorf("unexpected actions: %+v", got)
	}
}

func TestHealth(t *testing.T) {
	h := newTestHandler(t, &mockEnqueuer{}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.Health(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

func TestRouter_Routes(t *testing.T) {
	h := newTestHandler(t, &mockEnqueuer{}, nil, nil)
	router := api.NewRouter(h)

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/actions", http.StatusOK},
		{http.MethodGet, "/unknown", http.StatusNotFound},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s: got %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}
