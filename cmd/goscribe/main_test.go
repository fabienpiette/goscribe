package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/internal/elevenlabs"
	"goscribe/internal/harness"
	"goscribe/internal/openai"
	"goscribe/internal/whisperlocal"
	"goscribe/pkg/config"
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

func TestRunUnknownActionFailsBeforeTranscription(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	apiCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		openAISuccessHandler("should not be called")(w, r)
	}))
	defer server.Close()
	overrideOpenAIBaseURL(t, server.URL)

	audioFile := filepath.Join(t.TempDir(), "meeting.mp3")
	if err := os.WriteFile(audioFile, []byte("fake audio"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err := run(runOptions{
		apiKey:     "test-key",
		postAction: "openai-meeting-summary, nonexistent-action",
		args:       []string{audioFile},
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "nonexistent-action") {
		t.Errorf("error = %q, want it to name the unknown action", err.Error())
	}
	if apiCalls != 0 {
		t.Errorf("made %d API call(s), want 0: unknown actions must be rejected before transcription", apiCalls)
	}
	if _, statErr := os.Stat(strings.TrimSuffix(audioFile, ".mp3") + "-transcript.txt"); statErr == nil {
		t.Error("transcript file was written; validation should happen before transcription")
	}
}

// setupTranscriberTest creates a default config in a temp HOME, an audio file, and an
// OpenAI stub that counts calls. Returns the audio path and a pointer to the call count.
func setupTranscriberTest(t *testing.T) (string, *int) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	apiCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		openAISuccessHandler("summary")(w, r)
	}))
	t.Cleanup(server.Close)
	overrideOpenAIBaseURL(t, server.URL)

	audioFile := filepath.Join(t.TempDir(), "meeting.mp3")
	if err := os.WriteFile(audioFile, []byte("fake audio"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	return audioFile, &apiCalls
}

func TestRunInvalidTranscriber(t *testing.T) {
	audioFile, apiCalls := setupTranscriberTest(t)

	err := run(runOptions{apiKey: "test-key", transcriber: "whisperx", args: []string{audioFile}})
	if err == nil || !strings.Contains(err.Error(), "invalid transcriber 'whisperx'") {
		t.Fatalf("error = %v, want invalid transcriber", err)
	}
	if *apiCalls != 0 {
		t.Errorf("made %d API call(s), want 0", *apiCalls)
	}
}

func TestRunLocalTranscriber(t *testing.T) {
	audioFile, apiCalls := setupTranscriberTest(t)

	fakeWhisper(t)

	err := run(runOptions{
		apiKey:      "test-key",
		transcriber: "local",
		postAction:  "openai-meeting-summary",
		args:        []string{audioFile},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := strings.TrimSuffix(audioFile, ".mp3")
	transcript, err := os.ReadFile(base + "-transcript.txt")
	if err != nil || string(transcript) != "local words" {
		t.Errorf("transcript = %q, err = %v", transcript, err)
	}
	if _, err := os.Stat(base + "-openai-meeting-summary.txt"); err != nil {
		t.Errorf("post-action output missing: %v", err)
	}
	// Only the post-action may hit OpenAI; transcription stays local.
	if *apiCalls != 1 {
		t.Errorf("made %d OpenAI call(s), want 1 (the summary only)", *apiCalls)
	}
}

func TestRunElevenLabsTranscriberKeyFromEnv(t *testing.T) {
	audioFile, apiCalls := setupTranscriberTest(t)
	t.Setenv("ELEVENLABS_API_KEY", "env-key")

	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("xi-api-key")
		writeJSON(w, map[string]any{"text": "scribe words"})
	}))
	defer server.Close()
	orig := elevenlabs.BaseURL
	elevenlabs.BaseURL = server.URL
	t.Cleanup(func() { elevenlabs.BaseURL = orig })

	if err := run(runOptions{apiKey: "test-key", transcriber: "elevenlabs", args: []string{audioFile}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	transcript, _ := os.ReadFile(strings.TrimSuffix(audioFile, ".mp3") + "-transcript.txt")
	if string(transcript) != "scribe words" {
		t.Errorf("transcript = %q", transcript)
	}
	if gotKey != "env-key" {
		t.Errorf("xi-api-key = %q, want key from ELEVENLABS_API_KEY", gotKey)
	}
	if *apiCalls != 0 {
		t.Errorf("made %d OpenAI call(s), want 0", *apiCalls)
	}
}

// fakeWhisper stands in for whisper-ctranslate2 and always transcribes "local words".
func fakeWhisper(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-whisper")
	script := `#!/bin/sh
audio="$1"
while [ $# -gt 0 ]; do [ "$1" = "--output_dir" ] && out="$2"; shift; done
base=$(basename "$audio"); echo "local words" > "$out/${base%.*}.txt"
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake binary: %v", err)
	}
	orig := whisperlocal.Binary
	whisperlocal.Binary = bin
	t.Cleanup(func() { whisperlocal.Binary = orig })
}

func TestRunClaudeProviderDefaultsToLocalTranscriber(t *testing.T) {
	audioFile, apiCalls := setupTranscriberTest(t)
	fakeWhisper(t)

	bin := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\necho 'claude summary'\n"), 0755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	orig := harness.ClaudeBinary
	harness.ClaudeBinary = bin
	t.Cleanup(func() { harness.ClaudeBinary = orig })

	err := run(runOptions{
		apiKey:     "test-key",
		provider:   "claude",
		postAction: "openai-meeting-summary",
		args:       []string{audioFile},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := strings.TrimSuffix(audioFile, ".mp3")
	if got, _ := os.ReadFile(base + "-transcript.txt"); string(got) != "local words" {
		t.Errorf("transcript = %q, want the local transcriber's output", got)
	}
	if got, _ := os.ReadFile(base + "-openai-meeting-summary.txt"); string(got) != "claude summary" {
		t.Errorf("summary = %q, want claude's answer", got)
	}
	if *apiCalls != 0 {
		t.Errorf("made %d OpenAI call(s), want 0", *apiCalls)
	}
}

func TestRunInvalidProvider(t *testing.T) {
	audioFile, apiCalls := setupTranscriberTest(t)

	err := run(runOptions{apiKey: "test-key", provider: "mistral", args: []string{audioFile}})
	if err == nil || !strings.Contains(err.Error(), "invalid provider 'mistral'") {
		t.Fatalf("error = %v, want invalid provider", err)
	}
	if *apiCalls != 0 {
		t.Errorf("made %d API call(s), want 0", *apiCalls)
	}
}
