package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/internal/openai"
	"goscribe/pkg/config"
	"goscribe/pkg/lyrics"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	_ = json.NewEncoder(w).Encode(v)
}

// openAISuccessHandler returns an httptest handler that responds with the given text.
// Uses a local response struct matching the OpenAI chat completion shape.
func openAISuccessHandler(text string) http.HandlerFunc {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type resp struct {
		Choices []struct {
			Message msg `json:"message"`
		} `json:"choices"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		out := resp{
			Choices: []struct {
				Message msg `json:"message"`
			}{
				{Message: msg{Role: "assistant", Content: text}},
			},
		}
		writeJSON(w, out)
	}
}

func overrideOpenAIBaseURL(t *testing.T, url string) {
	t.Helper()
	orig := openai.BaseURL
	openai.BaseURL = url
	t.Cleanup(func() { openai.BaseURL = orig })
}

func TestMultipleActions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"Single action", "action1", []string{"action1"}},
		{"Two actions", "action1,action2", []string{"action1", "action2"}},
		{"Three actions", "action1,action2,action3", []string{"action1", "action2", "action3"}},
		{"Actions with spaces", "action1, action2, action3", []string{"action1", "action2", "action3"}},
		{"Actions with extra spaces", "action1 , action2 , action3", []string{"action1", "action2", "action3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actionIDs := strings.Split(tt.input, ",")
			for i, id := range actionIDs {
				actionIDs[i] = strings.TrimSpace(id)
			}

			if len(actionIDs) != len(tt.expected) {
				t.Errorf("got %d actions, want %d", len(actionIDs), len(tt.expected))
			}

			for i, id := range actionIDs {
				if i >= len(tt.expected) {
					break
				}
				if id != tt.expected[i] {
					t.Errorf("action[%d] = %v, want %v", i, id, tt.expected[i])
				}
			}
		})
	}
}

func TestMultiStringFlag(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		f := multiStringFlag{"a", "b", "c"}
		if got := f.String(); got != "a,b,c" {
			t.Errorf("String() = %q, want %q", got, "a,b,c")
		}
	})

	t.Run("Set single", func(t *testing.T) {
		var f multiStringFlag
		if err := f.Set("hello"); err != nil {
			t.Fatalf("Set() error: %v", err)
		}
		if len(f) != 1 || f[0] != "hello" {
			t.Errorf("after Set(\"hello\"), flag = %v", f)
		}
	})

	t.Run("Set comma-separated", func(t *testing.T) {
		var f multiStringFlag
		if err := f.Set("a, b, c"); err != nil {
			t.Fatalf("Set() error: %v", err)
		}
		if len(f) != 3 || f[0] != "a" || f[1] != "b" || f[2] != "c" {
			t.Errorf("after Set(\"a, b, c\"), flag = %v", f)
		}
	})

	t.Run("Set empty", func(t *testing.T) {
		var f multiStringFlag
		if err := f.Set(""); err != nil {
			t.Fatalf("Set() error: %v", err)
		}
		if len(f) != 0 {
			t.Errorf("after Set(\"\"), flag = %v, want empty", f)
		}
	})
}

func TestNormalizeArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr bool
	}{
		{
			name:  "no transcript flag",
			input: []string{"-k", "key", "audio.mp3"},
			want:  []string{"-k", "key", "audio.mp3"},
		},
		{
			name:  "transcript with single file",
			input: []string{"-transcript", "file1.txt", "-action", "summary"},
			want:  []string{"-transcript", "file1.txt", "-action", "summary"},
		},
		{
			name:  "transcript with multiple files",
			input: []string{"-transcript", "file1.txt", "file2.txt", "-action", "summary"},
			want:  []string{"-transcript", "file1.txt,file2.txt", "-action", "summary"},
		},
		{
			name:  "transcript= with multiple files",
			input: []string{"-transcript=file1.txt", "file2.txt", "-action", "summary"},
			want:  []string{"-transcript=file1.txt,file2.txt", "-action", "summary"},
		},
		{
			name:    "transcript with no value",
			input:   []string{"-transcript"},
			wantErr: true,
		},
		{
			name:  "empty args",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "transcript with files at end",
			input: []string{"-transcript", "a.txt", "b.txt", "c.txt"},
			want:  []string{"-transcript", "a.txt,b.txt,c.txt"},
		},
		{
			name:  "other flags pass through",
			input: []string{"-k", "key", "-o", "out.txt", "audio.mp3"},
			want:  []string{"-k", "key", "-o", "out.txt", "audio.mp3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeArgs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeArgs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- run() integration tests ---

func TestRunSongTranscriptMutualExclusion(t *testing.T) {
	err := run(runOptions{
		song:            true,
		transcriptFiles: []string{"some.txt"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "-song requires an audio file") {
		t.Errorf("error %q missing expected message", err.Error())
	}
}

func TestRunSongMode_WritesValidationFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakeVocals := filepath.Join(t.TempDir(), "vocals.wav")
	if err := os.WriteFile(fakeVocals, []byte("fake"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "audio") {
			if _, err := w.Write([]byte(`{"text":"I will always love you"}`)); err != nil {
				t.Fatalf("Write: %v", err)
			}
		} else {
			lv := lyrics.LyricsValidation{CoherenceScore: 80, Confidence: 0.9}
			b, _ := json.Marshal(lv)
			resp := fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, string(b))
			if _, err := w.Write([]byte(resp)); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}))
	defer srv.Close()
	openai.BaseURL = srv.URL

	audioDir := t.TempDir()
	audioPath := filepath.Join(audioDir, "song.mp3")
	if err := os.WriteFile(audioPath, []byte("audio"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts := runOptions{
		apiKey:         "test-key",
		provider:       "openai",
		enableFallback: false,
		song:           true,
		vocalExtractor: func(s string) (string, func(), error) {
			return fakeVocals, func() {}, nil
		},
		args: []string{audioPath},
	}
	if err := run(opts); err != nil {
		t.Fatalf("run: %v", err)
	}

	ext := filepath.Ext(audioPath)
	base := strings.TrimSuffix(audioPath, ext)
	validationFile := base + "-lyrics-validation.json"
	if _, err := os.Stat(validationFile); err != nil {
		t.Errorf("validation file not found: %v", err)
	}
}

func TestRunSongMode_CustomOutputFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakeVocals := filepath.Join(t.TempDir(), "vocals.wav")
	if err := os.WriteFile(fakeVocals, []byte("fake"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "audio") {
			if _, err := w.Write([]byte(`{"text":"sweet dreams are made of this"}`)); err != nil {
				t.Fatalf("Write: %v", err)
			}
		} else {
			lv := lyrics.LyricsValidation{CoherenceScore: 70, Confidence: 0.8}
			b, _ := json.Marshal(lv)
			if _, err := w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, string(b)))); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}))
	defer srv.Close()
	openai.BaseURL = srv.URL

	audioDir := t.TempDir()
	audioPath := filepath.Join(audioDir, "track.mp3")
	if err := os.WriteFile(audioPath, []byte("audio"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	customOut := filepath.Join(t.TempDir(), "custom.txt")

	opts := runOptions{
		apiKey:         "test-key",
		provider:       "openai",
		song:           true,
		output:         customOut,
		vocalExtractor: func(s string) (string, func(), error) { return fakeVocals, func() {}, nil },
		args:           []string{audioPath},
	}
	if err := run(opts); err != nil {
		t.Fatalf("run: %v", err)
	}

	ext := filepath.Ext(audioPath)
	base := strings.TrimSuffix(audioPath, ext)
	validationFile := base + "-lyrics-validation.json"
	if _, err := os.Stat(validationFile); err != nil {
		t.Errorf("validation file not found at %q: %v", validationFile, err)
	}
}

func TestRunSongMode_WithActions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	fakeVocals := filepath.Join(t.TempDir(), "vocals.wav")
	if err := os.WriteFile(fakeVocals, []byte("fake"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "audio") {
			if _, err := w.Write([]byte(`{"text":"I want to break free"}`)); err != nil {
				t.Fatalf("Write: %v", err)
			}
		} else {
			lv := lyrics.LyricsValidation{CoherenceScore: 88, Confidence: 0.93}
			b, _ := json.Marshal(lv)
			if _, err := w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, string(b)))); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}))
	defer srv.Close()
	openai.BaseURL = srv.URL

	audioDir := t.TempDir()
	audioPath := filepath.Join(audioDir, "song2.mp3")
	if err := os.WriteFile(audioPath, []byte("audio"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts := runOptions{
		apiKey:         "test-key",
		provider:       "openai",
		song:           true,
		postAction:     "openai-meeting-summary",
		vocalExtractor: func(s string) (string, func(), error) { return fakeVocals, func() {}, nil },
		args:           []string{audioPath},
	}
	if err := run(opts); err != nil {
		t.Fatalf("run: %v", err)
	}

	ext := filepath.Ext(audioPath)
	base := strings.TrimSuffix(audioPath, ext)
	validationFile := base + "-lyrics-validation.json"
	if _, err := os.Stat(validationFile); err != nil {
		t.Errorf("validation file not found: %v", err)
	}
	actionFile := base + "-openai-meeting-summary.txt"
	if _, err := os.Stat(actionFile); err != nil {
		t.Errorf("action result file not found: %v", err)
	}
}

func TestRunListActions(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey:      "XXXX",
		listActions: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTranscriptMode(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	ts := httptest.NewServer(openAISuccessHandler("processed output"))
	defer ts.Close()
	overrideOpenAIBaseURL(t, ts.URL)

	transcriptFile := filepath.Join(t.TempDir(), "test-transcript.txt")
	if err := os.WriteFile(transcriptFile, []byte("This is a test transcript."), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		enableFallback:  true,
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{transcriptFile},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTranscriptModeMultipleFiles(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	ts := httptest.NewServer(openAISuccessHandler("processed output"))
	defer ts.Close()
	overrideOpenAIBaseURL(t, ts.URL)

	dir := t.TempDir()
	file1 := filepath.Join(dir, "t1.txt")
	file2 := filepath.Join(dir, "t2.txt")
	if err := os.WriteFile(file1, []byte("Transcript one."), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if err := os.WriteFile(file2, []byte("Transcript two."), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		enableFallback:  true,
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{file1, file2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTranscriptModeNoAction(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		transcriptFiles: []string{"file.txt"},
	})
	if err == nil {
		t.Fatal("expected error when no action specified")
	}
	if !strings.Contains(err.Error(), "-action or --auto") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunTranscriptFileNotFound(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{"/nonexistent/file.txt"},
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunNoAudioFile(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{apiKey: "test-key"})
	if err == nil {
		t.Fatal("expected error for missing audio file")
	}
	if !strings.Contains(err.Error(), "audio file path is required") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunAudioFileNotFound(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey: "test-key",
		args:   []string{"/nonexistent/audio.mp3"},
	})
	if err == nil {
		t.Fatal("expected error for missing audio")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunWithCustomConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := `openai_api_key: "from-config"
post_actions:
  - id: "test-action"
    name: "Test Action"
    type: "openai"
    prompt: "Test"
    model: "gpt-4"
    temperature: 0.3
    max_tokens: 1000
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := run(runOptions{
		apiKey:      "XXXX",
		configFile:  configPath,
		listActions: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUnknownAction(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	transcriptFile := filepath.Join(t.TempDir(), "t.txt")
	if err := os.WriteFile(transcriptFile, []byte("text"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		postAction:      "nonexistent-action",
		transcriptFiles: []string{transcriptFile},
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunWithProviderFromConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := `provider: "gemini"
gemini_api_key: "gemini-from-config"
gemini_model: "gemini-2.0-flash"
post_actions:
  - id: "test"
    name: "Test"
    type: "gemini"
    prompt: "Test"
    model: "gemini-2.0-flash"
    temperature: 0.3
    max_tokens: 1000
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := run(runOptions{
		apiKey:      "XXXX",
		configFile:  configPath,
		listActions: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
