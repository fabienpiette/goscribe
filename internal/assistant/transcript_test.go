package assistant

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTranscriptPath(t *testing.T) {
	if got := TranscriptPath("assist-2026-10-28_15-00-00.md"); got != "assist-2026-10-28_15-00-00-transcript.md" {
		t.Errorf("got %q", got)
	}
	if got := TranscriptPath("notes.txt"); got != "notes-transcript.md" {
		t.Errorf("got %q, want the extension replaced", got)
	}
	if got := TranscriptPath(filepath.Join("records.v2", "notes")); got != filepath.Join("records.v2", "notes-transcript.md") {
		t.Errorf("got %q, want a dot in the folder kept", got)
	}
	if got := TranscriptPath(""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestAppendThenRewriteTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.md")
	a := Line{Speaker: "others", At: 9 * time.Second, Text: "first"}
	b := Line{Speaker: "me", At: 12*time.Minute + 9*time.Second, Text: "second"}
	if err := WriteTranscript(path, "HEADER", []Line{a}); err != nil {
		t.Fatal(err)
	}
	if err := AppendTranscript(path, b); err != nil {
		t.Fatal(err)
	}
	b.Text = "second, fixed"
	if err := WriteTranscript(path, "HEADER", []Line{a, b}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	want := "# Transcript\n\nHEADER\n\n[00:00:09] others: first\n[00:12:09] me: second, fixed\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
