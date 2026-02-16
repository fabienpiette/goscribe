package gemini

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

func geminiSuccessHandler(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := geminiResponse{
			Candidates: []geminiCandidate{
				{Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{{Text: text}},
				}},
			},
		}
		writeJSON(w, resp)
	}
}

func TestMakeRequest(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-goog-api-key") != "test-key" {
				t.Errorf("unexpected api key header: %s", r.Header.Get("x-goog-api-key"))
			}
			if !strings.Contains(r.URL.Path, "gemini-2.0-flash") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			resp := geminiResponse{
				Candidates: []geminiCandidate{
					{Content: struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text string `json:"text"`
						}{{Text: "Gemini says hello"}},
					}},
				},
			}
			writeJSON(w, resp)
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		contents := []geminiContent{{Parts: []geminiPart{{Text: "Hi"}}}}
		got, err := makeRequest("gemini-2.0-flash", contents, "test-key", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Candidates[0].Content.Parts[0].Text != "Gemini says hello" {
			t.Errorf("got %q, want %q", got.Candidates[0].Content.Parts[0].Text, "Gemini says hello")
		}
	})

	t.Run("429 rate limit", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		_, err := makeRequest("gemini-2.0-flash", nil, "key", 0)
		if err == nil {
			t.Fatal("expected error for 429")
		}
		if !strings.Contains(err.Error(), "rate limit") {
			t.Errorf("error should mention rate limit: %v", err)
		}
	})

	t.Run("API error in body", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := geminiResponse{
				Error: &geminiError{Code: 400, Message: "bad request", Status: "INVALID_ARGUMENT"},
			}
			writeJSON(w, resp)
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		_, err := makeRequest("gemini-2.0-flash", nil, "key", 0)
		if err == nil {
			t.Fatal("expected error for API error in body")
		}
		if !strings.Contains(err.Error(), "bad request") {
			t.Errorf("error should contain message: %v", err)
		}
	})

	t.Run("empty candidates", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, geminiResponse{})
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		_, err := makeRequest("gemini-2.0-flash", nil, "key", 0)
		if err == nil {
			t.Fatal("expected error for empty candidates")
		}
		if !strings.Contains(err.Error(), "no response") {
			t.Errorf("error should mention no response: %v", err)
		}
	})
}

func TestProcessWithGemini(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("Gemini summary"))
	defer ts.Close()
	overrideBaseURL(t, ts.URL)

	action := &config.PostAction{
		Model:       "gemini-2.0-flash",
		Prompt:      "Summarize this",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	got, err := processWithGemini("transcript text", action, "test-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Gemini summary" {
		t.Errorf("got %q, want %q", got, "Gemini summary")
	}
}

func TestProcessChunked(t *testing.T) {
	t.Run("small transcript fits in context", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("gemini processed"))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{
			Model:       "gemini-2.0-flash",
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		got, err := ProcessChunked("short text", action, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gemini processed" {
			t.Errorf("got %q, want %q", got, "gemini processed")
		}
	})

	t.Run("default model when empty", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("default model"))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		got, err := ProcessChunked("short text", action, "key", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "default model" {
			t.Errorf("got %q, want %q", got, "default model")
		}
	})
}

func TestTranscribe(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("transcribed audio text"))
	defer ts.Close()
	overrideBaseURL(t, ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribe(tmpFile, "test-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "transcribed audio text" {
		t.Errorf("got %q, want %q", got, "transcribed audio text")
	}
}

func TestMergeChunkResults(t *testing.T) {
	t.Run("single chunk no merge", func(t *testing.T) {
		got, err := mergeChunkResults([]string{"only chunk"}, &config.PostAction{}, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "only chunk" {
			t.Errorf("got %q, want %q", got, "only chunk")
		}
	})

	t.Run("multiple chunks merged via API", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("gemini merged"))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{Name: "Test", MaxTokens: 1000}
		got, err := mergeChunkResults([]string{"chunk1", "chunk2"}, action, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gemini merged" {
			t.Errorf("got %q, want %q", got, "gemini merged")
		}
	})

	t.Run("API error falls back to concatenation", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
		}))
		defer ts.Close()
		overrideBaseURL(t, ts.URL)

		action := &config.PostAction{Name: "Test", MaxTokens: 1000}
		got, err := mergeChunkResults([]string{"chunk1", "chunk2"}, action, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "chunk1") || !strings.Contains(got, "chunk2") {
			t.Errorf("expected concatenated chunks, got %q", got)
		}
	})
}
