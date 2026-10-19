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

// draftNote tells the reader what the notes are worth: they come from live
// speech-to-text and a model that never saw the whole meeting at once.
const draftNote = "_Live draft from realtime speech-to-text: a summary made from the full recording is more reliable._"

// Render writes the notes file: the final summary and action items first when
// the meeting is over, then the live summary, agenda items not covered,
// decisions, open questions, the librarian's research and every suggestion
// made.
func Render(m Memory, header string, final bool, updated time.Duration) string {
	var b strings.Builder
	b.WriteString("# Meeting notes\n\n")
	fmt.Fprintf(&b, "%s · updated at %s\n\n%s\n\n", header, clock(updated), draftNote)
	summaryTitle, notCoveredTitle := "Summary so far", "Not covered yet"
	if final {
		summaryTitle, notCoveredTitle = "Final summary", "Not covered"
	}
	section(&b, summaryTitle, m.Summary)
	if final {
		listSection(&b, "Action items", m.ActionItems)
	}
	listSection(&b, notCoveredTitle, m.NotCovered)
	listSection(&b, "Decisions", m.Decisions)
	listSection(&b, "Open questions", m.OpenQuestions)
	if len(m.Research) > 0 {
		b.WriteString("## Research\n\n")
		for _, f := range m.Research {
			b.WriteString(researchLine(f) + "\n")
		}
		b.WriteString("\n")
	}
	if len(m.Suggestions) > 0 {
		b.WriteString("## Suggestions\n\n")
		for _, s := range m.Suggestions {
			fmt.Fprintf(&b, "- [%s] %s: %s", clock(s.At), suggestionTitle(s), s.Text)
			if note := suggestionNote(s); note != "" {
				fmt.Fprintf(&b, " _(%s)_", note)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// suggestionTitle is "❓ Ask", with "‼" in front of a high-priority one.
func suggestionTitle(s Suggestion) string {
	l := suggestionLabels[s.Type]
	title := l.icon + " " + l.label
	if s.Priority == High {
		title = "‼ " + title
	}
	return title
}

// suggestionNote joins the why and the context file it comes from.
func suggestionNote(s Suggestion) string {
	switch {
	case s.Source == "":
		return s.Why
	case s.Why == "":
		return s.Source
	}
	return s.Why + " · " + s.Source
}

// researchLine is "- [hh:mm:ss] query → answer (sources)".
func researchLine(f Finding) string {
	line := fmt.Sprintf("- [%s] %s → ", clock(f.At), f.Query)
	if !f.Found {
		return line + "nothing found"
	}
	return line + f.Answer + joinSources(f.Sources)
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
