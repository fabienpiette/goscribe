package song_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/internal/gemini"
	"goscribe/internal/openai"
	"goscribe/internal/song"
	"goscribe/pkg/lyrics"
)

func writeFakeDemucs(t *testing.T, exitCode int, stderr string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "demucs")
	var body string
	if exitCode != 0 {
		body = `#!/bin/sh
echo "` + stderr + `" >&2
exit ` + fmt.Sprintf("%d", exitCode)
	} else {
		body = `#!/bin/sh
outdir=""
input=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) outdir="$2"; shift 2 ;;
    -n|--two-stems=*) shift 2 ;;
    -*) shift ;;
    *) input="$1"; shift ;;
  esac
done
stem=$(basename "$input")
stem="${stem%.*}"
mkdir -p "$outdir/htdemucs/$stem"
touch "$outdir/htdemucs/$stem/vocals.wav"
`
	}
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

func TestExtractVocals_Success(t *testing.T) {
	fakeDir := writeFakeDemucs(t, 0, "")
	t.Setenv("PATH", fakeDir+":"+os.Getenv("PATH"))

	audioDir := t.TempDir()
	audioPath := filepath.Join(audioDir, "mysong.mp3")
	if err := os.WriteFile(audioPath, []byte("fake"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	vocalsPath, cleanup, err := song.ExtractVocals(audioPath)
	if err != nil {
		t.Fatalf("ExtractVocals: %v", err)
	}
	defer cleanup()

	if !strings.HasSuffix(vocalsPath, "vocals.wav") {
		t.Errorf("vocalsPath %q does not end with vocals.wav", vocalsPath)
	}
	if _, err := os.Stat(vocalsPath); err != nil {
		t.Errorf("vocals.wav not found at %q: %v", vocalsPath, err)
	}

	cleanup()
	dir := filepath.Dir(filepath.Dir(filepath.Dir(vocalsPath)))
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove tmpdir %q", dir)
	}
}

func TestExtractVocals_DemucsNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := song.ExtractVocals("/some/song.mp3")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "demucs not found") {
		t.Errorf("error %q does not contain 'demucs not found'", err.Error())
	}
}

func validLyricsJSON() string {
	v := lyrics.LyricsValidation{
		CoherenceScore:  85,
		ViabilityScore:  80,
		StructureScore:  90,
		IsPlausibleSong: true,
		CleanedLyrics:   "clean lyrics",
		Confidence:      0.92,
		CoherenceIssues: []lyrics.CoherenceIssue{},
		SuspectedErrors: []lyrics.SuspectedError{},
		StructureAnalysis: lyrics.StructureAnalysis{
			HasRepetition: true, HasChorusPattern: true, StructureConsistent: true,
		},
		SemanticConsistency: lyrics.SemanticConsistency{
			HasTheme: true, ThemeDescription: "love",
		},
	}
	b, _ := json.Marshal(v)
	return strings.ReplaceAll(string(b), `"`, `\"`)
}

func TestValidateLyrics_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"choices":[{"message":{"content":"` + validLyricsJSON() + `"}}]}`
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(resp)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}))
	defer srv.Close()
	openai.BaseURL = srv.URL

	v, err := song.ValidateLyrics("some lyrics", "openai", "test-key", "", "gemini-2.0-flash", false)
	if err != nil {
		t.Fatalf("ValidateLyrics: %v", err)
	}
	if v.CoherenceScore != 85 {
		t.Errorf("CoherenceScore: got %v, want 85", v.CoherenceScore)
	}
	if !v.IsPlausibleSong {
		t.Error("IsPlausibleSong: want true")
	}
}

func TestValidateLyrics_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"choices":[{"message":{"content":"not valid json"}}]}`
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(resp)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}))
	defer srv.Close()
	openai.BaseURL = srv.URL

	_, err := song.ValidateLyrics("lyrics", "openai", "key", "", "gemini-2.0-flash", false)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parsing lyrics validation response") {
		t.Errorf("error %q missing expected message", err.Error())
	}
}

func TestValidateLyrics_FallbackToGemini(t *testing.T) {
	openaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer openaiSrv.Close()
	openai.BaseURL = openaiSrv.URL

	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"candidates":[{"content":{"parts":[{"text":"` + validLyricsJSON() + `"}]}}]}`
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(resp)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}))
	defer geminiSrv.Close()
	gemini.BaseURL = geminiSrv.URL

	v, err := song.ValidateLyrics("lyrics", "openai", "key", "gemini-key", "gemini-2.0-flash", true)
	if err != nil {
		t.Fatalf("ValidateLyrics with fallback: %v", err)
	}
	if v.CoherenceScore != 85 {
		t.Errorf("CoherenceScore after fallback: got %v, want 85", v.CoherenceScore)
	}
}
