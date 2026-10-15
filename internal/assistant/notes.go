package assistant

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"
)

var suggestionLabels = map[string]struct{ icon, label string }{
	TypeQuestion: {"❓", "Ask"},
	TypeAnswer:   {"💬", "Answer"},
	TypeMissed:   {"📌", "Missed"},
	TypeRisk:     {"⚠️", "Risk"},
}

// Render writes the notes file: the final summary and action items first when
// the meeting is over, then the live summary, decisions, open questions and
// every suggestion made.
func Render(m Memory, header string, final bool, updated time.Duration) string {
	var b strings.Builder
	b.WriteString("# Meeting notes\n\n")
	fmt.Fprintf(&b, "%s · updated at %s\n\n", header, clock(updated))
	summaryTitle := "Summary so far"
	if final {
		summaryTitle = "Final summary"
	}
	section(&b, summaryTitle, m.Summary)
	if final {
		listSection(&b, "Action items", m.ActionItems)
	}
	listSection(&b, "Decisions", m.Decisions)
	listSection(&b, "Open questions", m.OpenQuestions)
	if len(m.Suggestions) > 0 {
		b.WriteString("## Suggestions\n\n")
		for _, s := range m.Suggestions {
			l := suggestionLabels[s.Type]
			fmt.Fprintf(&b, "- [%s] %s %s: %s", clock(s.At), l.icon, l.label, s.Text)
			if s.Why != "" {
				fmt.Fprintf(&b, " _(%s)_", s.Why)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func section(b *strings.Builder, title, body string) {
	if body == "" {
		body = "_Nothing yet._"
	}
	fmt.Fprintf(b, "## %s\n\n%s\n\n", title, body)
}

func listSection(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, it := range items {
		b.WriteString("- " + it + "\n")
	}
	b.WriteString("\n")
}

// WriteNotes replaces path atomically, so a crash never leaves half a file.
func WriteNotes(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Clean removes control characters (including the ESC of terminal escape
// sequences) from text that came from speech-to-text or a model, before it is
// printed: a spoken or injected sequence must not drive the user's terminal.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
