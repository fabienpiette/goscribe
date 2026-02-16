package util

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGetMimeType(t *testing.T) {
	tests := []struct {
		name     string
		ext      string
		expected string
	}{
		{"MP3 file", ".mp3", "audio/mp3"},
		{"WAV file", ".wav", "audio/wav"},
		{"M4A file", ".m4a", "audio/mp4"},
		{"OGG file", ".ogg", "audio/ogg"},
		{"FLAC file", ".flac", "audio/flac"},
		{"AAC file", ".aac", "audio/aac"},
		{"AIFF file", ".aiff", "audio/aiff"},
		{"WebM file", ".webm", "audio/webm"},
		{"MPEG file", ".mpeg", "audio/mpeg"},
		{"Uppercase extension", ".MP3", "audio/mp3"},
		{"Unknown extension", ".xyz", ""},
		{"Empty extension", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMimeType(tt.ext)
			if got != tt.expected {
				t.Errorf("GetMimeType(%q) = %q, want %q", tt.ext, got, tt.expected)
			}
		})
	}
}

func TestGetModelContextLimit(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected int
	}{
		{"GPT-4", "gpt-4", 6000},
		{"GPT-4-turbo", "gpt-4-turbo", 100000},
		{"GPT-4o", "gpt-4o", 100000},
		{"GPT-3.5-turbo", "gpt-3.5-turbo", 12000},
		{"Gemini 2.0 flash", "gemini-2.0-flash", 900000},
		{"Gemini 1.5 pro", "gemini-1.5-pro", 900000},
		{"Gemini 1.5 flash", "gemini-1.5-flash", 900000},
		{"Gemini 1.0 pro", "gemini-1.0-pro", 28000},
		{"Unknown Gemini model", "gemini-unknown", 28000},
		{"Unknown model", "unknown-model", 6000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetModelContextLimit(tt.model)
			if got != tt.expected {
				t.Errorf("GetModelContextLimit(%q) = %d, want %d", tt.model, got, tt.expected)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"zero max len", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestSplitIntoSentences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single sentence", "Hello world.", []string{"Hello world."}},
		{"multiple sentences", "First. Second! Third?", []string{"First.", "Second!", "Third?"}},
		{"no punctuation", "just some text", []string{"just some text"}},
		{"trailing text after sentence", "Hello. world", []string{"Hello.", "world"}},
		{"empty string", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitIntoSentences(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitIntoSentences(%q) returned %d sentences, want %d: %v", tt.input, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("sentence[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{5, 3, 5},
		{3, 5, 5},
		{5, 5, 5},
		{-1, -5, -1},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d,%d", tt.a, tt.b), func(t *testing.T) {
			if got := Max(tt.a, tt.b); got != tt.want {
				t.Errorf("Max(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestShellescape(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Simple string", "hello", "'hello'"},
		{"String with space", "hello world", "'hello world'"},
		{"String with single quote", "it's", "'it'\\''s'"},
		{"String with special chars", "test$file", "'test$file'"},
		{"Empty string", "", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Shellescape(tt.input)
			if got != tt.expected {
				t.Errorf("Shellescape(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetFileSize(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		fileSize int64
		wantErr  bool
	}{
		{"Empty file", 0, false},
		{"Small file (1KB)", 1024, false},
		{"Medium file (1MB)", 1024 * 1024, false},
		{"Large file (26MB)", 26 * 1024 * 1024, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, "test.dat")
			f, err := os.Create(testFile)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			if tt.fileSize > 0 {
				if err := f.Truncate(tt.fileSize); err != nil {
					t.Fatalf("Failed to truncate file: %v", err)
				}
			}
			f.Close()

			size, err := GetFileSize(testFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetFileSize() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && size != tt.fileSize {
				t.Errorf("GetFileSize() = %d, want %d", size, tt.fileSize)
			}

			os.Remove(testFile)
		})
	}
}

func TestGetFileSizeNonExistent(t *testing.T) {
	_, err := GetFileSize("/non/existent/path/file.mp3")
	if err == nil {
		t.Error("GetFileSize() expected error for non-existent file, got nil")
	}
}

func TestParseRateLimitWaitTime(t *testing.T) {
	tests := []struct {
		name        string
		errorBody   string
		expectedMin float64
		expectedMax float64
	}{
		{
			name:        "Standard rate limit message",
			errorBody:   "Rate limit exceeded. Please try again in 9.798s",
			expectedMin: 9.0,
			expectedMax: 10.0,
		},
		{
			name:        "Short wait time",
			errorBody:   "Please try again in 1.5s",
			expectedMin: 1.0,
			expectedMax: 2.0,
		},
		{
			name:        "No wait time in message",
			errorBody:   "Some other error message",
			expectedMin: 10.0,
			expectedMax: 10.0,
		},
		{
			name:        "Empty message",
			errorBody:   "",
			expectedMin: 10.0,
			expectedMax: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := ParseRateLimitWaitTime(tt.errorBody)
			seconds := duration.Seconds()

			if seconds < tt.expectedMin || seconds > tt.expectedMax {
				t.Errorf("ParseRateLimitWaitTime(%q) = %.2f seconds, want between %.2f and %.2f",
					tt.errorBody, seconds, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}
