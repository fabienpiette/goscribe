package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/pkg/config"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	_ = json.NewEncoder(w).Encode(v)
}

func overrideBaseURL(t *testing.T, url string) {
	t.Helper()
	orig := BaseURL
	BaseURL = url
	t.Cleanup(func() { BaseURL = orig })
}

func openAISuccessHandler(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := chatCompletionResponse{
			Choices: []struct {
				Message message `json:"message"`
			}{
				{Message: message{Role: "assistant", Content: text}},
			},
		}
		writeJSON(w, resp)
	}
}

func TestMakeRequest(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
			}
			resp := chatCompletionResponse{
				Choices: []struct {
					Message message `json:"message"`
				}{
					{Message: message{Role: "assistant", Content: "Hello!"}},
				},
			}
			writeJSON(w, resp)
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		reqBody := chatCompletionRequest{
			Model:    "gpt-4",
			Messages: []message{{Role: "user", Content: "Hi"}},
		}
		got, err := makeRequest(reqBody, "test-key", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Choices[0].Message.Content != "Hello!" {
			t.Errorf("got content %q, want %q", got.Choices[0].Message.Content, "Hello!")
		}
	})

	t.Run("429 rate limit no retry", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		_, err := makeRequest(chatCompletionRequest{}, "key", 0)
		if err == nil {
			t.Fatal("expected error for 429")
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("error should mention 429: %v", err)
		}
	})

	t.Run("500 error no retry", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		_, err := makeRequest(chatCompletionRequest{}, "key", 0)
		if err == nil {
			t.Fatal("expected error for 500")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error should mention 500: %v", err)
		}
	})

	t.Run("empty choices", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, chatCompletionResponse{})
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		_, err := makeRequest(chatCompletionRequest{}, "key", 0)
		if err == nil {
			t.Fatal("expected error for empty choices")
		}
		if !strings.Contains(err.Error(), "no response") {
			t.Errorf("error should mention no response: %v", err)
		}
	})
}

func TestProcessChunked(t *testing.T) {
	t.Run("small transcript fits in context", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("processed result"))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{
			Model:       "gpt-4",
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		got, err := ProcessChunked("short transcript", action, "test-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "processed result" {
			t.Errorf("got %q, want %q", got, "processed result")
		}
	})

	t.Run("large transcript triggers chunking", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("merged output"))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{
			Model:       "gpt-4",
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		bigTranscript := strings.Repeat("This is a test sentence. ", 2000)
		got, err := ProcessChunked(bigTranscript, action, "test-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "merged output" {
			t.Errorf("got %q, want %q", got, "merged output")
		}
	})
}

func TestProcessWithOpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatCompletionResponse{
			Choices: []struct {
				Message message `json:"message"`
			}{
				{Message: message{Role: "assistant", Content: "Summary: meeting notes"}},
			},
		}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURL(t, ts.URL)

	action := &config.PostAction{
		Model:       "gpt-4",
		Prompt:      "Summarize this",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	got, err := processWithOpenAI("transcript text", action, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Summary: meeting notes" {
		t.Errorf("got %q, want %q", got, "Summary: meeting notes")
	}
}

func TestTranscribeAudio(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("expected multipart request, got %s", r.Header.Get("Content-Type"))
		}
		resp := transcriptionResponse{Text: "whisper transcription"}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURL(t, ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := TranscribeAudio(tmpFile, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "whisper transcription" {
		t.Errorf("got %q, want %q", got, "whisper transcription")
	}
}

func TestMergeChunkResults(t *testing.T) {
	t.Run("single chunk no merge", func(t *testing.T) {
		got, err := mergeChunkResults([]string{"only chunk"}, &config.PostAction{Model: "gpt-4", MaxTokens: 1000}, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "only chunk" {
			t.Errorf("got %q, want %q", got, "only chunk")
		}
	})

	t.Run("multiple chunks merged via API", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("merged result"))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{
			Name:      "Test Action",
			Model:     "gpt-4o",
			MaxTokens: 1000,
		}
		got, err := mergeChunkResults([]string{"chunk1", "chunk2"}, action, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "merged result" {
			t.Errorf("got %q, want %q", got, "merged result")
		}
	})
}

func TestHierarchicalMerge(t *testing.T) {
	t.Run("single result passthrough", func(t *testing.T) {
		got, err := hierarchicalMerge([]string{"solo"}, &config.PostAction{Model: "gpt-4o", Name: "Test", MaxTokens: 1000}, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "solo" {
			t.Errorf("got %q, want %q", got, "solo")
		}
	})
}
