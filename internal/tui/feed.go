package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"goscribe/internal/assistant"
)

// feed is the scrolling list of items, with the words still being recognized
// shown as a dim last line.
type feed struct {
	items                   []assistant.Item
	partialSpeaker, partial string
}

func (f *feed) add(it assistant.Item) {
	f.items = append(f.items, it)
	if it.Kind == assistant.ItemTranscript && it.Speaker == f.partialSpeaker {
		f.partialSpeaker, f.partial = "", ""
	}
}

// render wraps every entry to width cells.
func (f *feed) render(width int) string {
	wrap := lipgloss.NewStyle().Width(max(width, 1))
	lines := make([]string, 0, len(f.items)+1)
	for _, it := range f.items {
		lines = append(lines, wrap.Render(entry(it)))
	}
	if f.partial != "" {
		lines = append(lines, wrap.Render(partialStyle.Render(f.partialSpeaker+" ▸ "+f.partial)))
	}
	return strings.Join(lines, "\n")
}

// entry is one item, styled but not wrapped.
func entry(it assistant.Item) string {
	switch it.Kind {
	case assistant.ItemTranscript:
		return dimStyle.Render(clock(it.At)) + " " + speakerStyle(it.Speaker).Render(it.Speaker) + " " + it.Text
	case assistant.ItemAssist:
		s := badgeStyle(it.Badge).Render(it.Badge) + " "
		if it.High {
			s += highStyle.Render("‼") + " "
		}
		s += it.Text
		if it.Note != "" {
			s += " " + noteStyle.Render(it.Note)
		}
		return s
	case assistant.ItemInfo:
		return dimStyle.Render(it.Text)
	default:
		return it.Text
	}
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
