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
	if !strings.Contains(err.Error(), "-action, --auto or -tags") {
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

func TestRunElevenLabsTuningFlags(t *testing.T) {
	audioFile, _ := setupTranscriberTest(t)
	t.Setenv("ELEVENLABS_API_KEY", "env-key")

	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			form = r.MultipartForm.Value
		}
		writeJSON(w, map[string]any{"text": "ok"})
	}))
	defer server.Close()
	orig := elevenlabs.BaseURL
	elevenlabs.BaseURL = server.URL
	t.Cleanup(func() { elevenlabs.BaseURL = orig })

	err := run(runOptions{
		apiKey:      "test-key",
		transcriber: "elevenlabs",
		keyterms:    "Acme,Globex",
		numSpeakers: 3,
		args:        []string{audioFile},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(form["keyterms"], ",") != "Acme,Globex" {
		t.Errorf("keyterms = %v", form["keyterms"])
	}
	if strings.Join(form["num_speakers"], "") != "3" {
		t.Errorf("num_speakers = %v", form["num_speakers"])
	}
}

// fakeClaude installs a stand-in for the claude CLI that saves its prompt to
// $FAKE_PROMPT and prints answer.
func fakeClaude(t *testing.T, answer string) string {
	t.Helper()
	dir := t.TempDir()
	promptFile := filepath.Join(dir, "prompt")
	t.Setenv("FAKE_PROMPT", promptFile)
	bin := filepath.Join(dir, "fake-claude")
	script := "#!/bin/sh\necho \"$@\" > \"$FAKE_PROMPT.args\"\ncat > \"$FAKE_PROMPT\"\ncat <<'EOF'\n" + answer + "\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write fake claude: %v", err)
	}
	orig := harness.ClaudeBinary
	harness.ClaudeBinary = bin
	t.Cleanup(func() { harness.ClaudeBinary = orig })
	return promptFile
}

// recordsWithTranscript builds records/<date>/ with one transcript, plus an
// older tagged recording that seeds the vocabulary.
func recordsWithTranscript(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}
	root := filepath.Join(t.TempDir(), "records")
	for _, d := range []string{"20261001", "20261013"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	old := "topics:\n  - design-tokens\nprojects:\n  - billing\n"
	if err := os.WriteFile(filepath.Join(root, "20261001", "old-reduce-tags.yml"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(root, "20261013", "vokoscreenNG-2026-10-13_14-09-55-reduce-transcript.txt")
	if err := os.WriteFile(transcript, []byte(strings.Repeat("We talked about design tokens with Sam. See PROJ-1. ", 5)), 0644); err != nil {
		t.Fatal(err)
	}
	return transcript
}

func TestRunTagsOnTranscript(t *testing.T) {
	transcript := recordsWithTranscript(t)
	promptFile := fakeClaude(t, `{"type":"standup","summary":"Token sync","topics":["Design Tokens"],"people":["Sam"],"tickets":["proj-1"],"projects":["billing"]}`)

	err := run(runOptions{apiKey: "XXXX", provider: "claude", tags: true, transcriptFiles: []string{transcript}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tagsFile := strings.TrimSuffix(transcript, "-transcript.txt") + "-tags.yml"
	data, err := os.ReadFile(tagsFile)
	if err != nil {
		t.Fatalf("tags file not written: %v", err)
	}
	for _, want := range []string{"date: \"2026-10-13\"", "type: standup", "- design-tokens", "- Sam", "- PROJ-1", "- billing"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("tags file missing %q:\n%s", want, data)
		}
	}

	prompt, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(prompt), "Reuse these existing topics") || !strings.Contains(string(prompt), "design-tokens") {
		t.Errorf("prompt should carry the vocabulary from sibling folders:\n%s", prompt)
	}
	if !strings.Contains(string(prompt), "We talked about design tokens") {
		t.Error("prompt should include the transcript")
	}
}

func TestRunTagsBadAnswerWritesNothing(t *testing.T) {
	transcript := recordsWithTranscript(t)
	fakeClaude(t, "Sorry, I can't help with that.")

	if err := run(runOptions{apiKey: "XXXX", provider: "claude", tags: true, transcriptFiles: []string{transcript}}); err != nil {
		t.Fatalf("a tagging failure must not fail the run: %v", err)
	}
	if _, err := os.Stat(strings.TrimSuffix(transcript, "-transcript.txt") + "-tags.yml"); err == nil {
		t.Error("tags file written from an unparseable answer")
	}
}

func TestRunTagsNeedsSingleTranscript(t *testing.T) {
	transcript := recordsWithTranscript(t)

	err := run(runOptions{apiKey: "XXXX", provider: "claude", tags: true, transcriptFiles: []string{transcript, transcript}})
	if err == nil || !strings.Contains(err.Error(), "single -transcript") {
		t.Errorf("error = %v, want single-transcript error", err)
	}
}

func TestRunTagsSkipsTooShortTranscript(t *testing.T) {
	transcript := recordsWithTranscript(t)
	if err := os.WriteFile(transcript, []byte("This is a test."), 0644); err != nil {
		t.Fatal(err)
	}
	promptFile := fakeClaude(t, `{"type":"other","summary":"s","topics":["x"]}`)

	if err := run(runOptions{apiKey: "XXXX", provider: "claude", tags: true, transcriptFiles: []string{transcript}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(promptFile); err == nil {
		t.Error("model was called for a 4-word transcript")
	}
	if _, err := os.Stat(strings.TrimSuffix(transcript, "-transcript.txt") + "-tags.yml"); err == nil {
		t.Error("tags file written for a too-short transcript")
	}
}

func TestRunEmptyKeyFlagUsesConfigKey(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	_ = os.Setenv("HOME", t.TempDir())

	if err := config.CreateDefault(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}
	if err := config.StoreAPIKey("key-from-config"); err != nil {
		t.Fatalf("failed to store key: %v", err)
	}

	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		openAISuccessHandler("processed output")(w, r)
	}))
	defer ts.Close()
	overrideOpenAIBaseURL(t, ts.URL)

	transcriptFile := filepath.Join(t.TempDir(), "t.txt")
	if err := os.WriteFile(transcriptFile, []byte("This is a test transcript."), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	err := run(runOptions{
		apiKey:          "", // -k not given
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{transcriptFile},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer key-from-config" {
		t.Errorf("Authorization = %q, want the key from the config file", gotAuth)
	}
}

// claude_model and codex_model in the config reach the CLI.
func TestRunUsesConfiguredHarnessModel(t *testing.T) {
	transcript := recordsWithTranscript(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := config.CreateDefault(); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, ".goscribe", "config.yml")
	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("claude_model: sonnet\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { harness.Models = map[string]string{} })
	promptFile := fakeClaude(t, `{"type":"standup","summary":"s","topics":["x"]}`)

	if err := run(runOptions{apiKey: "XXXX", provider: "claude", tags: true, transcriptFiles: []string{transcript}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args, _ := os.ReadFile(promptFile + ".args"); !strings.Contains(string(args), "--model sonnet") {
		t.Errorf("claude args = %q, want --model sonnet", args)
	}
}

// -provider hermes runs actions through the Hermes Agent CLI.
func TestRunHermesProvider(t *testing.T) {
	transcript := recordsWithTranscript(t)
	bin := filepath.Join(t.TempDir(), "hermes")
	script := `#!/bin/sh
cat > /dev/null
echo '{"type": "result", "exit_code": 0, "text": "hermes summary"}'
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	orig := harness.HermesBinary
	harness.HermesBinary = bin
	t.Cleanup(func() { harness.HermesBinary = orig })

	if err := run(runOptions{apiKey: "XXXX", provider: "hermes", postAction: "openai-meeting-summary", transcriptFiles: []string{transcript}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSuffix(transcript, ".txt") + "-openai-meeting-summary.txt"
	if got, _ := os.ReadFile(out); string(got) != "hermes summary" {
		t.Errorf("summary = %q", got)
	}
}
