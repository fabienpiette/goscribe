// Package assistant is the brain of "goscribe assist": it keeps the live
// transcript and a bounded memory of the meeting (summary, decisions, open
// questions, suggestions made), asks a model for updates in cycles, and shows
// the result in the terminal and in a notes file.
//
// Like internal/tags, the model only proposes JSON: this package validates and
// caps it, and writes everything itself. The model call is injected, so the
// package imports nothing else from the project.
package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Line is one committed utterance of the live transcript.
type Line struct {
	Speaker string        // "me" or "others"
	At      time.Duration // since the session started
	Text    string
}

// Suggestion types the model may return.
const (
	TypeQuestion = "question" // a question the user could ask
	TypeAnswer   = "answer"   // an answer to a question raised, from the context
	TypeMissed   = "missed"   // a point nobody covered yet
	TypeRisk     = "risk"     // a contradiction with the context or earlier talk
)

type Suggestion struct {
	Type string        `json:"type"`
	Text string        `json:"text"`
	Why  string        `json:"why"`
	At   time.Duration `json:"-"`
}

// Memory is everything the assistant remembers between cycles.
type Memory struct {
	Summary       string
	Decisions     []string
	OpenQuestions []string
	Suggestions   []Suggestion
	ActionItems   []string // only from the final call
}

// Reply is what the model returns. A nil list means "unchanged"; an empty
// list clears it.
type Reply struct {
	Summary       string      `json:"summary"`
	Decisions     []string    `json:"decisions"`
	OpenQuestions []string    `json:"open_questions"`
	Suggestions   Suggestions `json:"suggestions"`
	ActionItems   []string    `json:"action_items"`
}

// Suggestions skips entries that aren't suggestion objects (models sometimes
// return plain strings), so one odd field doesn't lose the whole reply.
type Suggestions []Suggestion

func (s *Suggestions) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil // not a list: no suggestions
	}
	*s = Suggestions{}
	for _, r := range raw {
		var sg Suggestion
		if json.Unmarshal(r, &sg) == nil {
			*s = append(*s, sg)
		}
	}
	return nil
}

// Caps keep the memory, and so every prompt, bounded over a long meeting.
const (
	maxSummary        = 2000
	maxFinalSummary   = 6000
	maxDecisions      = 20
	maxOpenQuestions  = 15
	maxActionItems    = 30
	maxItem           = 300
	maxSuggestionsPer = 3
	// Two suggestions sharing this share of their words are the same idea. A
	// rephrasing keeps nearly all words; "who owns the rollback" and "who owns
	// the budget" (4 of 6) must stay distinct.
	similarSuggestion = 0.8
)

// Parse extracts the JSON object from a model answer, ignoring any prose or
// code fence around it.
func Parse(answer string) (Reply, error) {
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end <= start {
		return Reply{}, fmt.Errorf("no JSON object in model answer: %q", truncate(answer, 200))
	}
	var r Reply
	if err := json.Unmarshal([]byte(answer[start:end+1]), &r); err != nil {
		return Reply{}, fmt.Errorf("parsing model answer: %w", err)
	}
	return r, nil
}

// Apply merges a cycle's reply into the memory and returns what is new, for
// the feed: suggestions not made before, and decisions not listed before.
func (m *Memory) Apply(r Reply, at time.Duration) (newSuggestions []Suggestion, newDecisions []string) {
	if s := strings.TrimSpace(r.Summary); s != "" {
		m.Summary = truncate(s, maxSummary)
	}
	if r.Decisions != nil {
		decisions := cleanList(r.Decisions, maxDecisions)
		for _, d := range decisions {
			if !containsSimilar(m.Decisions, d) {
				newDecisions = append(newDecisions, d)
			}
		}
		m.Decisions = decisions
	}
	if r.OpenQuestions != nil {
		m.OpenQuestions = cleanList(r.OpenQuestions, maxOpenQuestions)
	}
	for _, s := range r.Suggestions {
		if len(newSuggestions) == maxSuggestionsPer {
			break
		}
		s.Type = strings.ToLower(strings.TrimSpace(s.Type))
		s.Text = truncate(strings.TrimSpace(s.Text), maxItem)
		s.Why = truncate(strings.TrimSpace(s.Why), maxItem)
		if !validType(s.Type) || s.Text == "" || m.repeats(s.Text) || repeatsAny(newSuggestions, s.Text) {
			continue
		}
		s.At = at
		newSuggestions = append(newSuggestions, s)
	}
	m.Suggestions = append(m.Suggestions, newSuggestions...)
	return newSuggestions, newDecisions
}

// ApplyFinal stores the end-of-meeting reply: a longer summary, the final
// lists and the action items. Suggestions are ignored.
func (m *Memory) ApplyFinal(r Reply) {
	if s := strings.TrimSpace(r.Summary); s != "" {
		m.Summary = truncate(s, maxFinalSummary)
	}
	if r.Decisions != nil {
		m.Decisions = cleanList(r.Decisions, maxDecisions)
	}
	if r.OpenQuestions != nil {
		m.OpenQuestions = cleanList(r.OpenQuestions, maxOpenQuestions)
	}
	if r.ActionItems != nil {
		m.ActionItems = cleanList(r.ActionItems, maxActionItems)
	}
}

func validType(t string) bool {
	switch t {
	case TypeQuestion, TypeAnswer, TypeMissed, TypeRisk:
		return true
	}
	return false
}

func (m *Memory) repeats(text string) bool {
	return repeatsAny(m.Suggestions, text)
}

func repeatsAny(list []Suggestion, text string) bool {
	for _, s := range list {
		if similar(s.Text, text) {
			return true
		}
	}
	return false
}

func containsSimilar(list []string, text string) bool {
	for _, s := range list {
		if similar(s, text) {
			return true
		}
	}
	return false
}

// similar compares the word sets of two texts (Jaccard index), ignoring case
// and punctuation, so a rephrased suggestion still counts as a repeat.
func similar(a, b string) bool {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	return float64(inter)/float64(union) >= similarSuggestion
}

func words(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		set[w] = true
	}
	return set
}

func cleanList(items []string, max int) []string {
	out := []string{}
	for _, it := range items {
		it = truncate(strings.TrimSpace(it), maxItem)
		if it == "" || containsExact(out, it) {
			continue
		}
		out = append(out, it)
		if len(out) == max {
			break
		}
	}
	return out
}

func containsExact(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// truncate cuts s to at most max runes, adding "…" when it cuts.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
