package assistant

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// Restored is what an earlier assist notes file gives back: its briefs and
// fixes. Skipped counts the lines that could not be restored.
type Restored struct {
	Briefs  []string
	Fixes   []Fix
	Skipped int
}

// LoadRestore reads an assist notes file for ParseNotes.
func LoadRestore(path string) (Restored, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Restored{}, false, err
	}
	return ParseNotes(string(data))
}

// ParseNotes reads the ## Brief and ## Fixes sections of an assist notes file
// (the grammar of Render). ok is false when text is not one: its first line is
// not "# Meeting notes" or the draft note is not in its first lines. A section
// that appears twice is an error, and then nothing is restored: a model could
// have forged the second one.
func ParseNotes(text string) (r Restored, ok bool, err error) {
	if IsPageCopy(text) || !isNotesText(text) {
		return Restored{}, false, nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	section, seen := "", map[string]bool{}
	var briefs []string
	var fixes []Fix
	skipped := 0
	for _, l := range lines[1:] {
		switch {
		case l == "## Brief" || l == "## Fixes":
			if seen[l] {
				return Restored{}, true, fmt.Errorf("two %q sections", l)
			}
			seen[l], section = true, l
		case strings.HasPrefix(l, "## "):
			section = ""
		case strings.TrimSpace(l) == "" || section == "":
		case section == "## Brief":
			text, found := strings.CutPrefix(l, "- ")
			text = strings.TrimSpace(Clean(text))
			switch {
			case !found || ValidateBrief(text) != nil,
				len(briefs) >= maxBriefs,
				slices.ContainsFunc(briefs, func(b string) bool { return strings.EqualFold(b, text) }):
				skipped++
			default:
				briefs = append(briefs, text)
			}
		default:
			if f, good := parseFixLine(l); good {
				fixes = append(fixes, f)
			} else {
				skipped++
			}
		}
	}
	return Restored{Briefs: briefs, Fixes: fixes, Skipped: skipped}, true, nil
}

// isNotesText says whether text looks like an assist notes file: "# Meeting
// notes" on the first line and the draft note in the first five.
func isNotesText(text string) bool {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return lines[0] == "# Meeting notes" && slices.Contains(lines[:min(len(lines), 5)], draftNote)
}

// parseFixLine reads "- `wrong` → `right`", with " _(suggested)_" after it for
// a guessed fix.
func parseFixLine(l string) (Fix, bool) {
	rest, ok := strings.CutPrefix(l, "- ")
	if !ok {
		return Fix{}, false
	}
	wrong, rest, ok := readCodeSpan(rest)
	if !ok {
		return Fix{}, false
	}
	rest, ok = strings.CutPrefix(rest, " → ")
	if !ok {
		return Fix{}, false
	}
	right, rest, ok := readCodeSpan(rest)
	if !ok {
		return Fix{}, false
	}
	guessed := false
	switch rest {
	case "":
	case " _(suggested)_":
		guessed = true
	default:
		return Fix{}, false
	}
	wrong, right = strings.TrimSpace(Clean(wrong)), strings.TrimSpace(Clean(right))
	if ValidateFix(wrong, right) != nil {
		return Fix{}, false
	}
	return Fix{Wrong: wrong, Right: right, Guessed: guessed}, true
}

// readCodeSpan reads a code span at the start of s: n backticks, the text and
// the next run of exactly n backticks. For n > 1 one space on each side of the
// text is dropped, as codeSpan adds them.
func readCodeSpan(s string) (text, rest string, ok bool) {
	n := len(s) - len(strings.TrimLeft(s, "`"))
	if n == 0 {
		return "", "", false
	}
	for i := n; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			text = s[n:i]
			if n > 1 && len(text) >= 2 && text[0] == ' ' && text[len(text)-1] == ' ' {
				text = text[1 : len(text)-1]
			}
			return text, s[j:], true
		}
		i = j
	}
	return "", "", false
}

// restore puts the briefs and fixes of an earlier meeting in force, silently:
// one line in the feed says what came back. Briefs go through the checks of
// /brief and fixes skip those already in force; what fails counts as skipped.
// A fix rewrites the lines said so far. The notes and the board are written
// once at the end.
func (s *state) restore(r Restored, from string) {
	briefs, fixes, skipped := 0, 0, r.Skipped
	for _, text := range r.Briefs {
		text = strings.TrimSpace(Clean(text))
		if ValidateBrief(text) != nil || len(s.mem.Briefs) >= maxBriefs ||
			slices.ContainsFunc(s.mem.Briefs, func(b Brief) bool { return strings.EqualFold(b.Text, text) }) {
			skipped++
			continue
		}
		s.mem.Briefs = append(s.mem.Briefs, Brief{Text: text})
		briefs++
	}
	for _, f := range r.Fixes {
		if ValidateFix(f.Wrong, f.Right) != nil || s.inForce(f) {
			skipped++
			continue
		}
		s.putFix(f)
		fixes++
	}
	if briefs > 0 {
		s.updateAsked = len(s.lines) > 0
		s.addChange(briefChangedLine)
	}
	if briefs+fixes > 0 {
		_ = s.writeNotes(false) // a failure is shown in the feed
	}
	s.board()
	s.info(restoreMessage(briefs, fixes, skipped, from))
}

func restoreMessage(briefs, fixes, skipped int, from string) string {
	var parts []string
	if briefs > 0 {
		parts = append(parts, plural(briefs, "brief", "briefs"))
	}
	if fixes > 0 {
		parts = append(parts, plural(fixes, "fix", "fixes"))
	}
	msg := "↺ nothing to restore from " + from
	if len(parts) > 0 {
		msg = "↺ restored " + strings.Join(parts, ", ") + " from " + from
	}
	if skipped > 0 {
		msg += "; " + plural(skipped, "line", "lines") + " skipped"
	}
	return msg
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
