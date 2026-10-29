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
	"path"
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
	Type     string        `json:"type"`
	Priority string        `json:"priority"` // "high" when the moment will pass soon, else ""
	Text     string        `json:"text"`
	Why      string        `json:"why"`
	Source   string        `json:"source"` // context file it comes from, if any
	At       time.Duration `json:"-"`
}

// High is the priority of a suggestion whose moment will pass soon.
const High = "high"

// Memory is everything the assistant remembers between cycles.
type Memory struct {
	Summary       string
	Decisions     []string
	OpenQuestions []string
	NotCovered    []string // agenda items and goals from the context not discussed yet
	Suggestions   []Suggestion
	ActionItems   []string  // only from the final call
	Research      []Finding // the librarian's results, oldest first
	Asked         []string  // every lookup query accepted, for dedup and the limit
	Searched      []string  // the user's /search questions (untrusted, only rendered in the Research block): they dedup model lookups but never count toward the limit
	QA            []QA      // the user's typed questions and their answers, oldest first
	Fixes         []Fix     // the corrections in force, in the order they were made
	Briefs        []Brief   // the user's standing instructions and facts, in order
	Docs          []Doc     // the sources the user added during the meeting, in order
}

// Doc is a source the user added with /add: a local file or folder, a web
// page, or a document the librarian found.
type Doc struct {
	Name   string // as the model cites it: the "### file:" names for files
	Title  string // what the user sees
	Origin string // the path or URL it came from
	Via    string // "file", "web", "librarian", "notes" (an earlier assist notes file) or "transcript"
	Text   string
	At     time.Duration
}

// Trusted says whether the text goes in the trusted context: only what the
// user pointed at on their own disk does.
func (d Doc) Trusted() bool { return d.Via == "file" }

// Brief is an instruction or fact the user typed with /brief.
type Brief struct {
	Text string
	At   time.Duration
}

// QA is a question the user typed and the answer they got.
type QA struct {
	Question, Answer string
	Source           string // a context file, "research" or "general"
	General          bool   // the answer is from general knowledge, not the meeting's sources
	At               time.Duration
}

// Reply is what the model returns. A nil list means "unchanged"; an empty
// list clears it.
type Reply struct {
	Summary       string      `json:"summary"`
	Decisions     []string    `json:"decisions"`
	OpenQuestions []string    `json:"open_questions"`
	NotCovered    []string    `json:"not_covered"`
	Suggestions   Suggestions `json:"suggestions"`
	ActionItems   []string    `json:"action_items"`
	Lookups       Lookups     `json:"lookups"`
	Fixes         FixGuesses  `json:"fixes"`
}

// FixGuess is a fix the model proposes for a word it thinks was misheard. It
// changes nothing until the user accepts it.
type FixGuess struct {
	Wrong string `json:"wrong"`
	Right string `json:"right"`
	Why   string `json:"why"`
}

// FixGuesses skips entries that aren't guess objects, like Suggestions.
type FixGuesses []FixGuess

func (g *FixGuesses) UnmarshalJSON(data []byte) error {
	*g = lenientList[FixGuess](data)
	return nil
}

// Suggestions skips entries that aren't suggestion objects (models sometimes
// return plain strings), so one odd field doesn't lose the whole reply.
type Suggestions []Suggestion

func (s *Suggestions) UnmarshalJSON(data []byte) error {
	*s = lenientList[Suggestion](data)
	return nil
}

// lenientList decodes a JSON list, skipping entries that don't decode as T;
// anything but a list gives none.
func lenientList[T any](data []byte) []T {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := []T{}
	for _, r := range raw {
		var v T
		if json.Unmarshal(r, &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// Caps keep the memory, and so every prompt, bounded over a long meeting.
const (
	maxSummary        = 2000
	maxFinalSummary   = 6000
	maxDecisions      = 20
	maxOpenQuestions  = 15
	maxNotCovered     = 15
	maxActionItems    = 30
	maxItem           = 300
	maxSuggestionsPer = 2 // fewer, sharper: a live feed is read at a glance
	// Two suggestions sharing this share of their words are the same idea. A
	// rephrasing keeps nearly all words; "who owns the rollback" and "who owns
	// the budget" (4 of 6) must stay distinct.
	similarSuggestion = 0.8
)

// Parse extracts the JSON object from a model answer, ignoring any prose or
// code fence around it.
func Parse(answer string) (Reply, error) {
	var r Reply
	if err := parseJSON(answer, &r); err != nil {
		return Reply{}, err
	}
	return r, nil
}

// parseJSON decodes the outermost JSON object of a model answer into v.
func parseJSON(answer string, v any) error {
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object in model answer: %q", truncate(answer, 200))
	}
	if err := json.Unmarshal([]byte(answer[start:end+1]), v); err != nil {
		return fmt.Errorf("parsing model answer: %w", err)
	}
	return nil
}

// Apply merges a cycle's reply into the memory and returns what is new, for
// the feed: suggestions not made before, and decisions not listed before.
// sources are the context files: an answer must come from one of them, as an
// answer from the model's own opinion can contradict what was just said.
func (m *Memory) Apply(r Reply, at time.Duration, sources []string) (newSuggestions []Suggestion, newDecisions []string) {
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
	if r.NotCovered != nil {
		m.NotCovered = cleanList(r.NotCovered, maxNotCovered)
	}
	for _, s := range r.Suggestions {
		if len(newSuggestions) == maxSuggestionsPer {
			break
		}
		s.Type = strings.ToLower(strings.TrimSpace(s.Type))
		s.Text = truncate(strings.TrimSpace(s.Text), maxItem)
		s.Why = truncate(strings.TrimSpace(s.Why), maxItem)
		s.Source = m.matchSource(s.Source, sources)
		if strings.EqualFold(strings.TrimSpace(s.Priority), High) {
			s.Priority = High
		} else {
			s.Priority = ""
		}
		if !validType(s.Type) || s.Text == "" || m.repeats(s.Text) || repeatsAny(newSuggestions, s.Text) {
			continue
		}
		if s.Type == TypeAnswer && s.Source == "" {
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
	if r.NotCovered != nil {
		m.NotCovered = cleanList(r.NotCovered, maxNotCovered)
	}
	if r.ActionItems != nil {
		m.ActionItems = cleanList(r.ActionItems, maxActionItems)
	}
}

// matchSource returns the context file a model's "source" names, matched on
// the path or the base name, ignoring case; or ResearchSource when it names
// the librarian's research and some lookup found something; "" when it names
// none.
func (m *Memory) matchSource(source string, files []string) string {
	src := strings.ToLower(strings.TrimSpace(source))
	if src == "" {
		return ""
	}
	for _, f := range files {
		lf := strings.ToLower(f)
		if strings.Contains(src, lf) || strings.Contains(src, path.Base(lf)) {
			return f
		}
	}
	if src == ResearchSource && m.foundAny() {
		return ResearchSource
	}
	return ""
}

// foundAny reports whether the librarian found anything yet.
func (m *Memory) foundAny() bool {
	for _, f := range m.Research {
		if f.Found {
			return true
		}
	}
	return false
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
	return jaccard(a, b) >= similarSuggestion
}

// jaccard is the share of words two texts have in common: 0 when either has
// none.
func jaccard(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	return float64(inter) / float64(union)
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
