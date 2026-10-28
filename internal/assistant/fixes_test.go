package assistant

import (
	"slices"
	"testing"
)

func TestReplaceWord(t *testing.T) {
	tests := []struct {
		name, in, wrong, right, want string
		n                            int
	}{
		{"simple", "the cash work", "cash", "cache", "the cache work", 1},
		{"case and punctuation", "Cash. cash, CASH", "cash", "cache", "cache. cache, cache", 3},
		{"inside other words", "cashly and recash", "cash", "cache", "cashly and recash", 0},
		{"phrase", "dark mood mode", "dark mood", "dark mode", "dark mode mode", 1},
		{"repeated", "cash cash", "cash", "cache", "cache cache", 2},
		{"case-only fix", "acme ships", "acme", "Acme", "Acme ships", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, n := ReplaceWord(tt.in, tt.wrong, tt.right)
			if got != tt.want || n != tt.n {
				t.Errorf("ReplaceWord(%q, %q, %q) = %q, %d; want %q, %d", tt.in, tt.wrong, tt.right, got, n, tt.want, tt.n)
			}
		})
	}
}

func TestReplaceWordUnicodeBoundaries(t *testing.T) {
	tests := []struct {
		in, wrong, right, want string
		n                      int
	}{
		{"l'été dernier", "été", "ete", "l'ete dernier", 1},
		{"thèmes", "thème", "theme", "thèmes", 0},
		{"Zoë said", "zoë", "Zoe", "Zoe said", 1},
	}
	for _, tt := range tests {
		got, n := ReplaceWord(tt.in, tt.wrong, tt.right)
		if got != tt.want || n != tt.n {
			t.Errorf("ReplaceWord(%q, %q, %q) = %q, %d; want %q, %d", tt.in, tt.wrong, tt.right, got, n, tt.want, tt.n)
		}
	}
}

func TestApplyFixesNoReapply(t *testing.T) {
	got := ApplyFixes("the film", []Fix{{Wrong: "film", Right: "short film"}})
	if got != "the short film" {
		t.Errorf("got %q, want %q", got, "the short film")
	}
	got = ApplyFixes("the film", []Fix{{Wrong: "film", Right: "short film"}, {Wrong: "short film", Right: "movie"}})
	if got != "the movie" {
		t.Errorf("got %q, want %q", got, "the movie")
	}
}

func TestMemoryApplyFix(t *testing.T) {
	m := &Memory{
		Summary:       "We discussed cash.",
		Decisions:     []string{"Ship cash first"},
		OpenQuestions: []string{"Who owns cash?"},
		NotCovered:    []string{"Cash rollout"},
		Suggestions:   []Suggestion{{Text: "cash"}},
		QA:            []QA{{Question: "cash?", Answer: "cash"}},
	}
	if !m.ApplyFix(Fix{Wrong: "cash", Right: "cache"}) {
		t.Fatal("ApplyFix = false, want true")
	}
	if m.Summary != "We discussed cache." ||
		!slices.Equal(m.Decisions, []string{"Ship cache first"}) ||
		!slices.Equal(m.OpenQuestions, []string{"Who owns cache?"}) ||
		!slices.Equal(m.NotCovered, []string{"cache rollout"}) {
		t.Errorf("not replaced everywhere: %+v", m)
	}
	if m.Suggestions[0].Text != "cash" || m.QA[0].Question != "cash?" || m.QA[0].Answer != "cash" {
		t.Errorf("suggestions or QA were touched: %+v", m)
	}
	if m.ApplyFix(Fix{Wrong: "cash", Right: "cache"}) {
		t.Error("ApplyFix = true with no occurrence left")
	}
}

func TestValidateFix(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyzabcdefghijklmno" // 41
	tests := []struct {
		wrong, right, want string
	}{
		{"", "x", "usage: /fix <wrong> <right>"},
		{"x", "  ", "usage: /fix <wrong> <right>"},
		{"same", "same", "the fix changes nothing"},
		{long, "x", "a fix is at most 40 characters per side"},
		{"x", long, "a fix is at most 40 characters per side"},
		{"acme", "Acme", ""},
	}
	for _, tt := range tests {
		err := ValidateFix(tt.wrong, tt.right)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tt.want {
			t.Errorf("ValidateFix(%q, %q) = %q, want %q", tt.wrong, tt.right, got, tt.want)
		}
	}
}

// A fix whose right side holds the wrong one leaves text already fixed alone,
// so applying it twice changes nothing.
func TestReplaceWordIdempotent(t *testing.T) {
	tests := []struct {
		in, wrong, right, want string
		n                      int
	}{
		{"the short film and the film", "film", "short film", "the short film and the short film", 1},
		{"Acme Corp and Acme", "Acme", "Acme Corp", "Acme Corp and Acme Corp", 1},
		{"the SHORT FILM", "film", "short film", "the SHORT FILM", 0},
		{"a film, a film", "film", "short film", "a short film, a short film", 2},
	}
	for _, tt := range tests {
		got, n := ReplaceWord(tt.in, tt.wrong, tt.right)
		if got != tt.want || n != tt.n {
			t.Errorf("ReplaceWord(%q, %q, %q) = %q, %d; want %q, %d", tt.in, tt.wrong, tt.right, got, n, tt.want, tt.n)
		}
		if again, _ := ReplaceWord(got, tt.wrong, tt.right); again != got {
			t.Errorf("twice: %q, want %q", again, got)
		}
	}
}

// A combining mark belongs to its word: "the" is not a word of NFD "thé".
func TestReplaceWordCombiningMarks(t *testing.T) {
	got, n := ReplaceWord("the\u0301 the", "the", "a")
	if got != "the\u0301 a" || n != 1 {
		t.Errorf("got %q, %d; want %q, 1", got, n, "the\u0301 a")
	}
}
