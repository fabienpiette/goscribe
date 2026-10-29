package tui

import "github.com/charmbracelet/lipgloss"

// Style 2, flat and tinted: no borders; the bars on a tinted background, the
// dashboard on a lighter one, badges as colored blocks with dark text.
var (
	tint     = lipgloss.AdaptiveColor{Light: "#E4E7F0", Dark: "#262A36"}
	lighter  = lipgloss.AdaptiveColor{Light: "#F2F3F7", Dark: "#1C1F28"}
	dimFg    = lipgloss.AdaptiveColor{Light: "#7A7F8C", Dark: "#7D8392"}
	brightFg = lipgloss.AdaptiveColor{Light: "#111318", Dark: "#F2F4F8"}
	badgeFg  = lipgloss.Color("#14161C")

	barStyle   = lipgloss.NewStyle().Background(tint).Foreground(brightFg)
	titleStyle = barStyle.Bold(true)
	barDim     = barStyle.Foreground(dimFg)
	mutedStyle = barStyle.Foreground(lipgloss.AdaptiveColor{Light: "#B5452E", Dark: "#F08A6C"})

	dimStyle     = lipgloss.NewStyle().Foreground(dimFg)
	partialStyle = dimStyle.Italic(true)
	noteStyle    = dimStyle
	highStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#C0392B", Dark: "#FF6B6B"})
	newStyle     = lipgloss.NewStyle().Bold(true).Background(tint).Foreground(brightFg)

	dashStyle      = lipgloss.NewStyle().Background(lighter).Foreground(brightFg).Padding(0, 1)
	dashTitleStyle = lipgloss.NewStyle().Background(lighter).Foreground(dimFg).Bold(true)
	dashTextStyle  = lipgloss.NewStyle().Background(lighter).Foreground(brightFg)
	dashDimStyle   = dashTextStyle.Foreground(dimFg)
)

// speakerColors color the speaker of a transcript line.
var speakerColors = map[string]lipgloss.TerminalColor{
	"me":     lipgloss.AdaptiveColor{Light: "#1F6FD1", Dark: "#6CB6FF"},
	"others": lipgloss.AdaptiveColor{Light: "#7B3FC4", Dark: "#C49BFF"},
	"you":    brightFg,
}

// badgeColors are the backgrounds of the badge blocks; LOOKUP and unknown badges use badgeDefault.
var badgeColors = map[string]lipgloss.TerminalColor{
	"ASK":       lipgloss.AdaptiveColor{Light: "#4A90E2", Dark: "#6CB6FF"},
	"ANSWER":    lipgloss.AdaptiveColor{Light: "#3FA66B", Dark: "#7EE2A8"},
	"MISSED":    lipgloss.AdaptiveColor{Light: "#D9A400", Dark: "#F5C542"},
	"RISK":      lipgloss.AdaptiveColor{Light: "#E05A47", Dark: "#FF8A7A"},
	"DECISION":  lipgloss.AdaptiveColor{Light: "#2BA8A0", Dark: "#5FD8CF"},
	"RECAP":     lipgloss.AdaptiveColor{Light: "#9A6FD6", Dark: "#C49BFF"},
	"LIBRARIAN": lipgloss.AdaptiveColor{Light: "#C77DB0", Dark: "#F0A6D8"},
	"FIX":       lipgloss.AdaptiveColor{Light: "#D9822B", Dark: "#FFA657"},
	"FIX?":      lipgloss.AdaptiveColor{Light: "#E0A261", Dark: "#FFC48A"},
	"BRIEF":     lipgloss.AdaptiveColor{Light: "#7A9A2E", Dark: "#B5D86A"},
	"SOURCE":    lipgloss.AdaptiveColor{Light: "#6E93C2", Dark: "#9DBBE0"},
}

var badgeDefault lipgloss.TerminalColor = lipgloss.AdaptiveColor{Light: "#9AA0AC", Dark: "#B8BECC"}

func badgeStyle(badge string) lipgloss.Style {
	bg, ok := badgeColors[badge]
	if !ok {
		bg = badgeDefault
	}
	return lipgloss.NewStyle().Bold(true).Foreground(badgeFg).Background(bg).Padding(0, 1)
}

func speakerStyle(speaker string) lipgloss.Style {
	if c, ok := speakerColors[speaker]; ok {
		return lipgloss.NewStyle().Bold(true).Foreground(c)
	}
	return lipgloss.NewStyle().Bold(true)
}
