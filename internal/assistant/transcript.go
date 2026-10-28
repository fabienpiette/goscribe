package assistant

import (
	"fmt"
	"os"
	"strings"
)

// TranscriptPath is the live transcript file next to the notes: the notes
// name with "-transcript" before the extension. It is "" without notes.
func TranscriptPath(notes string) string {
	if notes == "" {
		return ""
	}
	return strings.TrimSuffix(notes, ".md") + "-transcript.md"
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
