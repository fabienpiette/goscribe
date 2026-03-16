package main

import (
	"os"
	"testing"
	"time"
)

func TestParseRedisAddr(t *testing.T) {
	tests := []struct {
		name     string
		redisURL string
		want     string
	}{
		{
			name:     "simple host:port",
			redisURL: "redis://redis:6379",
			want:     "redis:6379",
		},
		{
			name:     "host only",
			redisURL: "redis://redis",
			want:     "redis",
		},
		{
			name:     "with db number",
			redisURL: "redis://redis:6379/0",
			want:     "redis:6379",
		},
		{
			name:     "with credentials",
			redisURL: "redis://user:pass@redis:6379",
			want:     "redis:6379",
		},
		{
			name:     "TLS",
			redisURL: "rediss://redis:6379",
			want:     "redis:6379",
		},
		{
			name:     "invalid URL returns original",
			redisURL: "not-a-valid-url",
			want:     "not-a-valid-url",
		},
		{
			name:     "empty returns empty",
			redisURL: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRedisAddr(tt.redisURL)
			if got != tt.want {
				t.Errorf("parseRedisAddr(%q) = %q, want %q", tt.redisURL, got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	originalEnv := make(map[string]string)
	for _, key := range []string{"MODE", "PORT", "REDIS_URL", "OPENAI_API_KEY", "GEMINI_API_KEY", "RESULT_TTL_HOURS", "MAX_UPLOAD_MB", "SHUTDOWN_TIMEOUT_SECONDS"} {
		originalEnv[key] = os.Getenv(key)
		defer os.Setenv(key, originalEnv[key])
	}

	os.Setenv("MODE", "api")
	os.Setenv("PORT", "9999")
	os.Setenv("REDIS_URL", "redis://custom:6379")
	os.Setenv("OPENAI_API_KEY", "sk-test")
	os.Setenv("GEMINI_API_KEY", "gemini-test")
	os.Setenv("RESULT_TTL_HOURS", "48")
	os.Setenv("MAX_UPLOAD_MB", "50")
	os.Setenv("SHUTDOWN_TIMEOUT_SECONDS", "60")

	cfg := loadConfig()

	if cfg.mode != "api" {
		t.Errorf("mode = %q, want %q", cfg.mode, "api")
	}
	if cfg.port != "9999" {
		t.Errorf("port = %q, want %q", cfg.port, "9999")
	}
	if cfg.redisAddr != "custom:6379" {
		t.Errorf("redisAddr = %q, want %q", cfg.redisAddr, "custom:6379")
	}
	if cfg.openAIKey != "sk-test" {
		t.Errorf("openAIKey = %q, want %q", cfg.openAIKey, "sk-test")
	}
	if cfg.geminiKey != "gemini-test" {
		t.Errorf("geminiKey = %q, want %q", cfg.geminiKey, "gemini-test")
	}
	if cfg.resultTTL != 48*time.Hour {
		t.Errorf("resultTTL = %v, want %v", cfg.resultTTL, 48*time.Hour)
	}
	if cfg.maxUploadBytes != 50<<20 {
		t.Errorf("maxUploadBytes = %v, want %v", cfg.maxUploadBytes, 50<<20)
	}
	if cfg.shutdownTimeout != 60*time.Second {
		t.Errorf("shutdownTimeout = %v, want %v", cfg.shutdownTimeout, 60*time.Second)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	originalEnv := make(map[string]string)
	for _, key := range []string{"MODE", "PORT", "REDIS_URL", "OPENAI_API_KEY", "GEMINI_API_KEY", "RESULT_TTL_HOURS", "MAX_UPLOAD_MB", "SHUTDOWN_TIMEOUT_SECONDS"} {
		originalEnv[key] = os.Getenv(key)
		os.Unsetenv(key)
		defer os.Setenv(key, originalEnv[key])
	}

	cfg := loadConfig()

	if cfg.mode != "all" {
		t.Errorf("mode = %q, want %q", cfg.mode, "all")
	}
	if cfg.port != "8080" {
		t.Errorf("port = %q, want %q", cfg.port, "8080")
	}
	if cfg.resultTTL != 24*time.Hour {
		t.Errorf("resultTTL = %v, want %v", cfg.resultTTL, 24*time.Hour)
	}
	if cfg.maxUploadBytes != 100<<20 {
		t.Errorf("maxUploadBytes = %v, want %v", cfg.maxUploadBytes, 100<<20)
	}
}

func TestLoadConfigWithAllVars(t *testing.T) {
	originalEnv := make(map[string]string)
	for _, key := range []string{"MODE", "PORT", "REDIS_URL", "OPENAI_API_KEY", "GEMINI_API_KEY", "GEMINI_MODEL", "GOSCRIBE_PROVIDER", "RESULT_TTL_HOURS", "MAX_UPLOAD_MB", "SHUTDOWN_TIMEOUT_SECONDS", "UPLOADS_DIR"} {
		originalEnv[key] = os.Getenv(key)
		os.Unsetenv(key)
		defer os.Setenv(key, originalEnv[key])
	}

	os.Setenv("MODE", "worker")
	os.Setenv("PORT", "3000")
	os.Setenv("REDIS_URL", "redis://myredis:6379/1")
	os.Setenv("OPENAI_API_KEY", "sk-key123")
	os.Setenv("GEMINI_API_KEY", "gemini-key456")
	os.Setenv("GEMINI_MODEL", "gemini-1.5-pro")
	os.Setenv("GOSCRIBE_PROVIDER", "gemini")
	os.Setenv("RESULT_TTL_HOURS", "12")
	os.Setenv("MAX_UPLOAD_MB", "25")
	os.Setenv("SHUTDOWN_TIMEOUT_SECONDS", "15")
	os.Setenv("UPLOADS_DIR", "/custom/uploads")

	cfg := loadConfig()

	if cfg.mode != "worker" {
		t.Errorf("mode = %q, want worker", cfg.mode)
	}
	if cfg.port != "3000" {
		t.Errorf("port = %q, want 3000", cfg.port)
	}
	if cfg.redisAddr != "myredis:6379" {
		t.Errorf("redisAddr = %q, want myredis:6379", cfg.redisAddr)
	}
	if cfg.openAIKey != "sk-key123" {
		t.Errorf("openAIKey = %q, want sk-key123", cfg.openAIKey)
	}
	if cfg.geminiKey != "gemini-key456" {
		t.Errorf("geminiKey = %q, want gemini-key456", cfg.geminiKey)
	}
	if cfg.geminiModel != "gemini-1.5-pro" {
		t.Errorf("geminiModel = %q, want gemini-1.5-pro", cfg.geminiModel)
	}
	if cfg.provider != "gemini" {
		t.Errorf("provider = %q, want gemini", cfg.provider)
	}
	if cfg.resultTTL != 12*time.Hour {
		t.Errorf("resultTTL = %v, want 12h", cfg.resultTTL)
	}
	if cfg.maxUploadBytes != 25<<20 {
		t.Errorf("maxUploadBytes = %v, want 25MB", cfg.maxUploadBytes)
	}
	if cfg.shutdownTimeout != 15*time.Second {
		t.Errorf("shutdownTimeout = %v, want 15s", cfg.shutdownTimeout)
	}
	if cfg.uploadsDir != "/custom/uploads" {
		t.Errorf("uploadsDir = %q, want /custom/uploads", cfg.uploadsDir)
	}
}
