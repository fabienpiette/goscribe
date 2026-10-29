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
// when the runes around it are not letters, digits or combining marks, which
// regexp's ASCII-only \b would get wrong for accented words. It is
// idempotent: when right holds wrong (film -> short film), an occurrence that
// already sits inside right is left alone.
func ReplaceWord(s, wrong, right string) (out string, n int) {
	w := []rune(wrong)
	if len(w) == 0 {
		return s, 0
	}
	rr := []rune(right)
	var inRight []int
	if len(rr) > len(w) { // else right is wrong in another case: always rewrite
		inRight = offsets(rr, w)
	}
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); {
		if matchesAt(r, i, w) && !insideRight(r, i, rr, inRight) {
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

// offsets lists where w occurs in r, ignoring case.
func offsets(r, w []rune) []int {
	var out []int
	for k := 0; k+len(w) <= len(r); k++ {
		if foldAt(r, k, w) {
			out = append(out, k)
		}
	}
	return out
}

// insideRight reports whether the match at i is part of right already
// written in r: right starts at i-k for some offset k of wrong in right.
func insideRight(r []rune, i int, right []rune, ks []int) bool {
	for _, k := range ks {
		if i-k >= 0 && foldAt(r, i-k, right) {
			return true
		}
	}
	return false
}

// foldAt reports whether w is at r[i:], ignoring case. Case folds are rune
// by rune, so a fold that changes the length (ß and SS) never matches.
func foldAt(r []rune, i int, w []rune) bool {
	if i+len(w) > len(r) {
		return false
	}
	for k, c := range w {
		if r[i+k] != c && !strings.EqualFold(string(r[i+k]), string(c)) {
			return false
		}
	}
	return true
}

// matchesAt reports whether w matches r at i, ignoring case, on word boundaries.
func matchesAt(r []rune, i int, w []rune) bool {
	if !foldAt(r, i, w) {
		return false
	}
	if i > 0 && isWordRune(r[i-1]) {
		return false
	}
	end := i + len(w)
	return end == len(r) || !isWordRune(r[end])
}

// isWordRune: combining marks count, so the e of a decomposed é stays in its word.
func isWordRune(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c) || unicode.Is(unicode.M, c)
}

// ApplyFixes applies each fix once, in order. A fix never rescans its own
// output and skips text it already fixed, so film -> short film does not
// double up.
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
