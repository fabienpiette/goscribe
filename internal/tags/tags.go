// Package tags builds a "<base>-tags.yml" file next to a transcript so recordings
// can be found by subject with plain find/grep. The model proposes the tags as
// JSON; this package cleans them up and writes the YAML itself, one list item per
// line, so `grep -rlw dark-mode --include='*-tags.yml'` is a reliable search.
package tags

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Types is the closed list of meeting types the model picks from.
var Types = []string{"standup", "1:1", "demo", "planning", "retro", "interview", "presentation", "workshop", "meeting", "other"}

// MinWords is the shortest transcript worth tagging. Shorter ones are usually
// test clips or speech-to-text hallucinations, and tagging them adds noise.
const MinWords = 30

// MaxVocabulary caps how many existing tags are sent to the model.
const MaxVocabulary = 400

type Tags struct {
	Date     string   `yaml:"date,omitempty" json:"-"`
	Type     string   `yaml:"type" json:"type"`
	Summary  string   `yaml:"summary" json:"summary"`
	Topics   []string `yaml:"topics" json:"topics"`
	People   []string `yaml:"people" json:"people"`
	Tickets  []string `yaml:"tickets" json:"tickets"`
	Projects []string `yaml:"projects" json:"projects"`
}

// Vocabulary holds the topics and projects already used in other tag files,
// most frequent first, so the model reuses them instead of inventing synonyms.
type Vocabulary struct {
	Topics   []string
	Projects []string
}

// Path returns where the tags file for a transcript (or audio) file goes:
// "x-transcript.txt" and "x.mp3" both give "x-tags.yml".
func Path(file string) string {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	base = strings.TrimSuffix(base, "-transcript")
	return base + "-tags.yml"
}

var (
	// 2026-10-15, or 2026_10_15 as in Google Meet downloads.
	separatedDate = regexp.MustCompile(`(\d{4})[-_](\d{2})[-_](\d{2})`)
	compactDate   = regexp.MustCompile(`(?:^|\D)(\d{4})(\d{2})(\d{2})(?:\D|$)`)
	dayFolder     = regexp.MustCompile(`^(\d{4})(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])$`)
)

// DateFromName returns the YYYY-MM-DD date in a file name, else the date of its
// records/<YYYYMMDD>/ folder, else "".
func DateFromName(path string) string {
	name := filepath.Base(path)
	if m := separatedDate.FindStringSubmatch(name); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3]
	}
	if m := compactDate.FindStringSubmatch(name); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3]
	}
	if m := dayFolder.FindStringSubmatch(filepath.Base(filepath.Dir(path))); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3]
	}
	return ""
}

// Prompt is the instruction sent to the model before the transcript.
func Prompt(v Vocabulary) string {
	var b strings.Builder
	b.WriteString(`Tag this meeting transcript so it can be found later by subject.

Reply with ONLY a JSON object, no explanation and no code fence, with these keys:
- "type": one of ` + strings.Join(Types, ", ") + `
- "summary": one sentence (max 20 words) saying what the meeting was about
- "topics": 3 to 8 subjects discussed, as short lowercase kebab-case tags (e.g. "design-tokens", "release-process")
- "people": names of people mentioned or speaking, as written (first name is fine); empty list if none
- "tickets": issue or ticket IDs said or shown, like "PROJ-123"; empty list if none
- "projects": product, project, team or tool names discussed, as kebab-case tags; empty list if none

Rules:
- Tag what was actually discussed, not what was only mentioned in passing.
- Prefer general, reusable topics over one-off phrases.
`)
	if len(v.Topics) > 0 {
		b.WriteString("- Reuse these existing topics whenever one fits; add a new topic only when none does: " + strings.Join(v.Topics, ", ") + "\n")
	}
	if len(v.Projects) > 0 {
		b.WriteString("- Reuse these existing projects whenever one fits: " + strings.Join(v.Projects, ", ") + "\n")
	}
	return b.String()
}

// Parse reads the model's JSON answer (tolerating code fences or surrounding
// text) and returns cleaned-up tags.
func Parse(answer string) (Tags, error) {
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end <= start {
		return Tags{}, fmt.Errorf("no JSON object in model answer: %q", truncate(answer, 200))
	}
	var t Tags
	if err := json.Unmarshal([]byte(answer[start:end+1]), &t); err != nil {
		return Tags{}, fmt.Errorf("parsing model answer: %w", err)
	}
	return Normalize(t), nil
}

var (
	nonSlug  = regexp.MustCompile(`[^a-z0-9]+`)
	ticketRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*-[0-9]+$`)
	spaces   = regexp.MustCompile(`\s+`)
)

func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// Normalize enforces the formats grep relies on: kebab-case topics and projects,
// uppercase ticket IDs, known meeting types, one-line summary, sorted unique lists.
func Normalize(t Tags) Tags {
	t.Type = strings.ToLower(strings.TrimSpace(t.Type))
	known := false
	for _, typ := range Types {
		known = known || typ == t.Type
	}
	if !known {
		t.Type = "other"
	}
	t.Summary = truncate(spaces.ReplaceAllString(strings.TrimSpace(t.Summary), " "), 200)
	t.Topics = clean(t.Topics, slug)
	t.Projects = clean(t.Projects, slug)
	t.People = clean(t.People, func(s string) string { return spaces.ReplaceAllString(strings.TrimSpace(s), " ") })
	t.Tickets = clean(t.Tickets, func(s string) string {
		s = strings.ToUpper(strings.TrimSpace(s))
		if !ticketRe.MatchString(s) {
			return ""
		}
		return s
	})
	return t
}

// clean maps each value, then drops empties and case-insensitive duplicates and sorts.
// It always returns a non-nil slice so empty lists are written as [] rather than null.
func clean(values []string, fn func(string) string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = fn(v)
		if key := strings.ToLower(v); v != "" && !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// CollectVocabulary reads tag files in root and one level of subfolders
// (the records/<date>/ layout) and returns their topics and projects, most used first.
// The depth is bounded so a transcript sitting in a big folder can't trigger a deep scan.
func CollectVocabulary(root string) Vocabulary {
	topics, projects := map[string]int{}, map[string]int{}
	for _, pattern := range []string{"*-tags.yml", "*/*-tags.yml"} {
		files, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var t Tags
			if yaml.Unmarshal(data, &t) != nil {
				continue
			}
			for _, s := range t.Topics {
				topics[s]++
			}
			for _, s := range t.Projects {
				projects[s]++
			}
		}
	}
	return Vocabulary{Topics: byCount(topics), Projects: byCount(projects)}
}

func byCount(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for k := range counts {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	if len(out) > MaxVocabulary {
		out = out[:MaxVocabulary]
	}
	return out
}

// Write saves t as YAML with block-style lists (one item per line).
func Write(path string, t Tags) error {
	data, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("encoding tags: %w", err)
	}
	header := "# Generated by goscribe. Search: grep -rlw <tag> --include='*-tags.yml'\n"
	return os.WriteFile(path, append([]byte(header), data...), 0644)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
