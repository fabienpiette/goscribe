package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"goscribe/internal/worker"
	"goscribe/pkg/config"
)

type JobEnqueuer interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

type HandlerConfig struct {
	Enqueuer        JobEnqueuer
	RDB             *redis.Client
	PostActions     []config.PostAction
	ResultTTL       time.Duration
	MaxUploadBytes  int64
	UploadsDir      string
	DefaultProvider string
}

type Handler struct {
	cfg HandlerConfig
}

func NewHandler(cfg HandlerConfig) *Handler {
	return &Handler{cfg: cfg}
}

type submitResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) SubmitJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(h.cfg.MaxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "parsing form: "+err.Error())
		return
	}

	transcript := r.FormValue("transcript")
	actions := r.FormValue("actions")
	provider := r.FormValue("provider")
	webhookURL := r.FormValue("webhook_url")

	if provider == "" {
		provider = h.cfg.DefaultProvider
	}

	var audioPath string
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		if header.Size > h.cfg.MaxUploadBytes {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("file exceeds %d MB limit", h.cfg.MaxUploadBytes>>20))
			return
		}
		audioPath, err = h.saveUpload(file, header.Filename)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "saving upload: "+err.Error())
			return
		}
	}

	if audioPath == "" && transcript == "" {
		writeError(w, http.StatusBadRequest, "provide either a file or transcript text")
		return
	}

	jobID := uuid.New().String()
	initial := worker.JobResult{
		JobID:     jobID,
		Status:    worker.StatusQueued,
		CreatedAt: time.Now(),
	}
	if err := h.saveResult(r.Context(), initial); err != nil {
		writeError(w, http.StatusInternalServerError, "storing job: "+err.Error())
		return
	}

	payload := worker.ProcessPayload{
		JobID:      jobID,
		AudioPath:  audioPath,
		Transcript: transcript,
		Actions:    actions,
		Provider:   provider,
		WebhookURL: webhookURL,
	}
	b, _ := json.Marshal(payload)
	task := asynq.NewTask(worker.TaskTypeProcess, b)
	if _, err := h.cfg.Enqueuer.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueuing job: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, submitResponse{JobID: jobID, Status: worker.StatusQueued})
}

func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	result, err := h.loadResult(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListActions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.cfg.PostActions)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) saveUpload(file io.Reader, originalName string) (string, error) {
	if err := os.MkdirAll(h.cfg.UploadsDir, 0755); err != nil {
		return "", err
	}
	ext := filepath.Ext(originalName)
	dst := filepath.Join(h.cfg.UploadsDir, uuid.New().String()+ext)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, file); err != nil {
		return "", err
	}
	return dst, nil
}

func (h *Handler) saveResult(ctx context.Context, r worker.JobResult) error {
	b, _ := json.Marshal(r)
	return h.cfg.RDB.Set(ctx, worker.ResultKeyPrefix+r.JobID, b, h.cfg.ResultTTL).Err()
}

func (h *Handler) loadResult(ctx context.Context, jobID string) (*worker.JobResult, error) {
	val, err := h.cfg.RDB.Get(ctx, worker.ResultKeyPrefix+jobID).Result()
	if err != nil {
		return nil, err
	}
	var r worker.JobResult
	if err := json.Unmarshal([]byte(val), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
