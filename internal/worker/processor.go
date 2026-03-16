package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"goscribe/internal/provider"
	"goscribe/pkg/config"
)

var webhookClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: dialContextWithValidation,
	},
}

func dialContextWithValidation(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return nil, fmt.Errorf("refused: %s resolves to private IP", host)
		}
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	resolvedAddr := net.JoinHostPort(ips[0].String(), port)
	return dialer.DialContext(ctx, network, resolvedAddr)
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return isPrivateIPv4(ipv4)
	}
	if len(ip) == 16 {
		return isPrivateIPv6(ip)
	}
	return false
}

func isPrivateIPv4(ip net.IP) bool {
	if ip[0] == 10 {
		return true
	}
	if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
		return true
	}
	if ip[0] == 192 && ip[1] == 168 {
		return true
	}
	return false
}

func isPrivateIPv6(ip net.IP) bool {
	if len(ip) != 16 {
		return false
	}
	if ip[0] == 0xfe && ip[1] == 0x80 {
		return true
	}
	if ip[0] == 0xfc || ip[0] == 0xfd {
		return true
	}
	if ip[0] == 0x00 && ip[1] == 0x00 && ip[2] == 0x00 && ip[3] == 0x00 && ip[4] == 0x00 && ip[5] == 0x00 && ip[6] == 0x00 && ip[7] == 0x00 {
		if ip[8] == 0x00 && ip[9] == 0x00 && ip[10] == 0x00 && ip[11] == 0x00 && ip[12] == 0x00 && ip[13] == 0x00 && ip[14] == 0x00 && ip[15] == 0x01 {
			return true
		}
	}
	return false
}

type Transcriber interface {
	TranscribeAudio(audioPath, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (string, error)
	ProcessChunked(transcript string, action *config.PostAction, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (string, error)
	SelectBestActions(transcript string, actions []config.PostAction, prov, openaiKey, geminiKey, geminiModel string) ([]string, error)
}

type RealTranscriber struct{}

func (RealTranscriber) TranscribeAudio(audioPath, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (string, error) {
	return provider.TranscribeAudio(audioPath, prov, openaiKey, geminiKey, geminiModel, fallback)
}

func (RealTranscriber) ProcessChunked(transcript string, action *config.PostAction, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (string, error) {
	return provider.ProcessChunked(transcript, action, prov, openaiKey, geminiKey, geminiModel, fallback)
}

func (RealTranscriber) SelectBestActions(transcript string, actions []config.PostAction, prov, openaiKey, geminiKey, geminiModel string) ([]string, error) {
	return provider.SelectBestActions(transcript, actions, prov, openaiKey, geminiKey, geminiModel)
}

type Config struct {
	Transcriber Transcriber
	RDB         *redis.Client
	OpenAIKey   string
	GeminiKey   string
	GeminiModel string
	Provider    string
	ResultTTL   time.Duration
	PostActions []config.PostAction
}

type Processor struct {
	cfg Config
}

func NewProcessor(cfg Config) *Processor {
	if cfg.Transcriber == nil {
		cfg.Transcriber = RealTranscriber{}
	}
	if cfg.GeminiModel == "" {
		cfg.GeminiModel = "gemini-2.0-flash"
	}
	return &Processor{cfg: cfg}
}

func (p *Processor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload ProcessPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	p.updateStatus(ctx, payload.JobID, StatusProcessing)

	if payload.AudioPath != "" {
		defer os.Remove(payload.AudioPath)
	}

	transcript := payload.Transcript
	if payload.AudioPath != "" && transcript == "" {
		var err error
		transcript, err = p.cfg.Transcriber.TranscribeAudio(
			payload.AudioPath, payload.Provider,
			p.cfg.OpenAIKey, p.cfg.GeminiKey, p.cfg.GeminiModel, true,
		)
		if err != nil {
			return p.failJob(ctx, payload, fmt.Sprintf("transcription failed: %v", err))
		}
	}

	actionIDs := p.resolveActions(ctx, transcript, payload)

	results := make(map[string]string)
	for _, id := range actionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		action := config.FindAction(p.cfg.PostActions, id)
		if action == nil {
			continue
		}
		out, err := p.cfg.Transcriber.ProcessChunked(
			transcript, action, payload.Provider,
			p.cfg.OpenAIKey, p.cfg.GeminiKey, p.cfg.GeminiModel, true,
		)
		if err != nil {
			results[id] = fmt.Sprintf("error: %v", err)
		} else {
			results[id] = out
		}
	}

	now := time.Now()
	result := JobResult{
		JobID:        payload.JobID,
		Status:       StatusCompleted,
		ProviderUsed: payload.Provider,
		Transcript:   transcript,
		Results:      results,
		CompletedAt:  &now,
	}
	if existing := p.loadResult(ctx, payload.JobID); existing != nil {
		result.CreatedAt = existing.CreatedAt
	}

	if err := p.saveResult(ctx, result); err != nil {
		return err
	}
	if payload.WebhookURL != "" {
		p.fireWebhook(payload.WebhookURL, result)
	}
	return nil
}

func (p *Processor) resolveActions(ctx context.Context, transcript string, payload ProcessPayload) []string {
	if payload.Actions == "auto" {
		selected, err := p.cfg.Transcriber.SelectBestActions(
			transcript, p.cfg.PostActions, payload.Provider,
			p.cfg.OpenAIKey, p.cfg.GeminiKey, p.cfg.GeminiModel,
		)
		if err != nil {
			return nil
		}
		return selected
	}
	if payload.Actions == "" {
		return nil
	}
	return strings.Split(payload.Actions, ",")
}

func (p *Processor) failJob(ctx context.Context, payload ProcessPayload, errMsg string) error {
	now := time.Now()
	result := JobResult{
		JobID:       payload.JobID,
		Status:      StatusFailed,
		Error:       errMsg,
		CompletedAt: &now,
	}
	if existing := p.loadResult(ctx, payload.JobID); existing != nil {
		result.CreatedAt = existing.CreatedAt
	}
	_ = p.saveResult(ctx, result)
	if payload.WebhookURL != "" {
		p.fireWebhook(payload.WebhookURL, result)
	}
	return errors.New(errMsg)
}

func (p *Processor) updateStatus(ctx context.Context, jobID, status string) {
	existing := p.loadResult(ctx, jobID)
	if existing == nil {
		return
	}
	existing.Status = status
	_ = p.saveResult(ctx, *existing)
}

func (p *Processor) loadResult(ctx context.Context, jobID string) *JobResult {
	val, err := p.cfg.RDB.Get(ctx, ResultKeyPrefix+jobID).Result()
	if err != nil {
		return nil
	}
	var r JobResult
	if err := json.Unmarshal([]byte(val), &r); err != nil {
		return nil
	}
	return &r
}

func (p *Processor) saveResult(ctx context.Context, r JobResult) error {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return p.cfg.RDB.Set(ctx, ResultKeyPrefix+r.JobID, b, p.cfg.ResultTTL).Err()
}

func (p *Processor) fireWebhook(rawURL string, result JobResult) {
	if !isAllowedWebhookURL(rawURL) {
		return
	}
	b, err := json.Marshal(result)
	if err != nil {
		return
	}
	resp, err := webhookClient.Post(rawURL, "application/json", bytes.NewReader(b))
	if err != nil {
		return
	}
	resp.Body.Close()
}

func isAllowedWebhookURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return true
}
