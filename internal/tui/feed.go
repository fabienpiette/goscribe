package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"goscribe/internal/assistant"
)

// feed is the scrolling list of items, with the words still being recognized
// shown as a dim last line. Wrapped items are cached for one width, so a
// status update only re-renders the partial line.
type feed struct {
	items                   []assistant.Item
	partialSpeaker, partial string

	width    int    // the width body was wrapped to
	done     int    // items already in body
	body     string // the wrapped items[:done]
	rendered int    // items wrapped so far, for tests
}

func (f *feed) add(it assistant.Item) {
	f.items = append(f.items, it)
	if it.Kind == assistant.ItemTranscript && it.Speaker == f.partialSpeaker {
		f.partialSpeaker, f.partial = "", ""
	}
}

// render wraps the entries to width cells, only the new ones when the width
// has not changed.
func (f *feed) render(width int) string {
	width = max(width, 1)
	if width != f.width {
		f.width, f.done, f.body = width, 0, ""
	}
	wrap := lipgloss.NewStyle().Width(width)
	var b strings.Builder
	b.WriteString(f.body)
	for _, it := range f.items[f.done:] {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(wrap.Render(entry(it)))
		f.rendered++
	}
	f.body, f.done = b.String(), len(f.items)
	if f.partial == "" {
		return f.body
	}
	partial := wrap.Render(partialStyle.Render(clean(f.partialSpeaker) + " ▸ " + clean(f.partial)))
	if f.body == "" {
		return partial
	}
	return f.body + "\n" + partial
}

// entry is one item, styled but not wrapped.
func entry(it assistant.Item) string {
	switch it.Kind {
	case assistant.ItemTranscript:
		return dimStyle.Render(clock(it.At)) + " " + speakerStyle(it.Speaker).Render(clean(it.Speaker)) + " " + clean(it.Text)
	case assistant.ItemAssist:
		s := badgeStyle(it.Badge).Render(clean(it.Badge)) + " "
		if it.High {
			s += highStyle.Render("‼") + " "
		}
		s += clean(it.Text)
		if it.Note != "" {
			s += " " + noteStyle.Render(clean(it.Note))
		}
		return s
	case assistant.ItemInfo:
		return dimStyle.Render(clean(it.Text))
	default:
		return clean(it.Text)
	}
}

// clean makes model output safe to print: escape sequences removed, line
// breaks and tabs turned into spaces, other control characters dropped. The
// model reads the untrusted transcript, so it could be led to emit sequences
// that clear the screen or write the clipboard.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, ansi.Strip(s))
}

// clock writes an elapsed time as mm:ss, or h:mm:ss past the hour.
func clock(d time.Duration) string {
	s := int(d.Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// newMarker is the line over the bottom of a scrolled-up feed: "↓ N new".
func newMarker(n, width int) string {
	return newStyle.Width(width).MaxWidth(width).Align(lipgloss.Center).Render(fmt.Sprintf("↓ %d new", n))
}
