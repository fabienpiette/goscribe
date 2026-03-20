package worker

import (
	"time"

	"goscribe/pkg/lyrics"
)

const TaskTypeProcess = "goscribe:process"

const ResultKeyPrefix = "goscribe:result:"

const (
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

type ProcessPayload struct {
	JobID      string `json:"job_id"`
	AudioPath  string `json:"audio_path,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Actions    string `json:"actions,omitempty"`
	Provider   string `json:"provider"`
	WebhookURL string `json:"webhook_url,omitempty"`
	Song       bool   `json:"song,omitempty"`
}

type JobResult struct {
	JobID            string                   `json:"job_id"`
	Status           string                   `json:"status"`
	Step             string                   `json:"step,omitempty"` // progress hint: "vocals_extracting", "transcribing", "validating"
	ProviderUsed     string                   `json:"provider_used,omitempty"`
	Transcript       string                   `json:"transcript,omitempty"`
	Results          map[string]string        `json:"results,omitempty"`
	Error            string                   `json:"error,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	CompletedAt      *time.Time               `json:"completed_at,omitempty"`
	LyricsValidation *lyrics.LyricsValidation `json:"lyrics_validation,omitempty"`
}
