package assistant

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

// maxFixSide caps one side of a fix, in runes.
const maxFixSide = 40

// Fix is a live correction: a word or phrase the transcript got wrong and what
// it should read instead.
type Fix struct {
	Wrong, Right string
	Guessed      bool // accepted from a model proposal
	At           time.Duration
}

// ReplaceWord replaces every whole-word, case-insensitive occurrence of wrong
// (a word or phrase) in s with right as given; n is the count. A match is whole
// when the runes around it are not letters or digits, which regexp's ASCII-only
// \b would get wrong for accented words.
func ReplaceWord(s, wrong, right string) (out string, n int) {
	w := []rune(wrong)
	if len(w) == 0 {
		return s, 0
	}
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); {
		if matchesAt(r, i, w) {
			b.WriteString(right)
			i += len(w)
			n++
			continue
		}
		b.WriteRune(r[i])
		i++
	}
	if n == 0 {
		return s, 0
	}
	return b.String(), n
}

// matchesAt reports whether w matches r at i, ignoring case, on word boundaries.
func matchesAt(r []rune, i int, w []rune) bool {
	end := i + len(w)
	if end > len(r) {
		return false
	}
	for k, c := range w {
		if r[i+k] != c && !strings.EqualFold(string(r[i+k]), string(c)) {
			return false
		}
	}
	if i > 0 && isWordRune(r[i-1]) {
		return false
	}
	return end == len(r) || !isWordRune(r[end])
}

func isWordRune(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }

// ApplyFixes applies each fix once, in order. A fix never rescans its own
// output, so film -> short film does not double up; call it on raw text only.
func ApplyFixes(s string, fixes []Fix) string {
	for _, f := range fixes {
		s, _ = ReplaceWord(s, f.Wrong, f.Right)
	}
	return s
}

// ApplyFix replaces wrong with right in the summary, decisions, open questions
// and agenda; it reports whether anything changed.
func (m *Memory) ApplyFix(f Fix) bool {
	changed := false
	fix := func(s string) string {
		out, n := ReplaceWord(s, f.Wrong, f.Right)
		if n > 0 {
			changed = true
		}
		return out
	}
	m.Summary = fix(m.Summary)
	for _, list := range [][]string{m.Decisions, m.OpenQuestions, m.NotCovered} {
		for i := range list {
			list[i] = fix(list[i])
		}
	}
	return changed
}

// ValidateFix checks the two sides a user typed for /fix.
func ValidateFix(wrong, right string) error {
	if strings.TrimSpace(wrong) == "" || strings.TrimSpace(right) == "" {
		return errors.New("usage: /fix <wrong> <right>")
	}
	if wrong == right {
		return errors.New("the fix changes nothing")
	}
	if len([]rune(wrong)) > maxFixSide || len([]rune(right)) > maxFixSide {
		return errors.New("a fix is at most 40 characters per side")
	}
	return nil
}
