package assistant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TranscriptPath is the live transcript file next to the notes: the notes
// name, without its extension, with "-transcript.md". It is "" without notes.
func TranscriptPath(notes string) string {
	if notes == "" {
		return ""
	}
	return strings.TrimSuffix(notes, filepath.Ext(notes)) + "-transcript.md"
}

// SourcesDir is the folder next to the notes that holds what /add saves: the
// notes name, without ".md", with "-sources". It is "" without notes.
func SourcesDir(notes string) string {
	if notes == "" {
		return ""
	}
	return strings.TrimSuffix(notes, ".md") + "-sources"
}

func transcriptLine(l Line) string {
	return fmt.Sprintf("[%s] %s: %s\n", clock(l.At), l.Speaker, l.Text)
}

// AppendTranscript adds one line to a transcript made by WriteTranscript.
func AppendTranscript(path string, l Line) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(transcriptLine(l)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// isTranscriptText says whether text is a transcript file: raw speech, so
// never trusted background.
func isTranscriptText(text string) bool {
	first, _, _ := strings.Cut(text, "\n")
	return strings.TrimSuffix(first, "\r") == "# Transcript"
}

// WriteTranscript rewrites the whole transcript atomically: the header, then
// every line. It starts the file and fixes lines already appended.
func WriteTranscript(path, header string, lines []Line) error {
	var b strings.Builder
	b.WriteString("# Transcript\n\n" + header + "\n\n")
	for _, l := range lines {
		b.WriteString(transcriptLine(l))
	}
	return WriteNotes(path, b.String())
}

// IsTranscript says whether text is a transcript file written by assist.
func IsTranscript(text string) bool { return isTranscriptText(text) }
