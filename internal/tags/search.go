package tags

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SearchFields maps the field names accepted in "field:value" terms
// (singular and plural) to the tag field they search.
var SearchFields = map[string]string{
	"topic": "topics", "topics": "topics",
	"person": "people", "people": "people",
	"ticket": "tickets", "tickets": "tickets",
	"project": "projects", "projects": "projects",
	"type":    "type",
	"summary": "summary",
}

// Term is one search term. An empty Field matches any field.
type Term struct {
	Field string
	Value string
}

// Query matches tag files whose fields contain every term (case-insensitive
// substring) and whose date falls within Since/Until (YYYY-MM-DD, inclusive).
type Query struct {
	Terms []Term
	Since string
	Until string
}

// Match is a tags file that satisfied a query.
type Match struct {
	TagsFile string
	Tags     Tags
}

// Transcript returns the transcript path that goes with the tags file.
func (m Match) Transcript() string {
	return strings.TrimSuffix(m.TagsFile, "-tags.yml") + "-transcript.txt"
}

// Related returns the other text outputs next to the transcript (action results).
func (m Match) Related() []string {
	base := strings.TrimSuffix(m.TagsFile, "-tags.yml")
	files, _ := filepath.Glob(base + "-*.txt")
	var out []string
	for _, f := range files {
		if f != m.Transcript() {
			out = append(out, f)
		}
	}
	return out
}

var fieldName = regexp.MustCompile(`^[a-z]+$`)

// ParseQuery builds a query from command-line terms ("dark-mode", "person:alan")
// and optional dates.
func ParseQuery(args []string, since, until string) (Query, error) {
	q := Query{Since: since, Until: until}
	for _, d := range []string{since, until} {
		if d == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return Query{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", d)
		}
	}
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		term := Term{Value: arg}
		// "type:1:1" splits at the first colon; "1:1" alone stays a free term.
		if field, value, ok := strings.Cut(arg, ":"); ok && fieldName.MatchString(field) {
			mapped, known := SearchFields[field]
			if !known {
				return Query{}, fmt.Errorf("unknown field %q (use topic, person, ticket, project, type or summary)", field)
			}
			if value == "" {
				return Query{}, fmt.Errorf("empty value for %q", field)
			}
			term = Term{Field: mapped, Value: value}
		}
		term.Value = strings.ToLower(term.Value)
		q.Terms = append(q.Terms, term)
	}
	return q, nil
}

func (q Query) Matches(t Tags) bool {
	if (q.Since != "" || q.Until != "") && t.Date == "" {
		return false
	}
	// YYYY-MM-DD strings compare correctly as text.
	if q.Since != "" && t.Date < q.Since {
		return false
	}
	if q.Until != "" && t.Date > q.Until {
		return false
	}
	for _, term := range q.Terms {
		if !matchTerm(t, term) {
			return false
		}
	}
	return true
}

func matchTerm(t Tags, term Term) bool {
	fields := map[string][]string{
		"topics":   t.Topics,
		"people":   t.People,
		"tickets":  t.Tickets,
		"projects": t.Projects,
		"type":     {t.Type},
		"summary":  {t.Summary},
	}
	for name, values := range fields {
		if term.Field != "" && term.Field != name {
			continue
		}
		for _, v := range values {
			if strings.Contains(strings.ToLower(v), term.Value) {
				return true
			}
		}
	}
	return false
}

// Search walks root recursively for *-tags.yml files and returns those matching q,
// newest first. Files that can't be read or parsed are reported in skipped.
func Search(root string, q Query) (matches []Match, skipped []error, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			skipped = append(skipped, walkErr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), "-tags.yml") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			skipped = append(skipped, readErr)
			return nil
		}
		var t Tags
		if yamlErr := yaml.Unmarshal(data, &t); yamlErr != nil {
			skipped = append(skipped, fmt.Errorf("%s: %w", path, yamlErr))
			return nil
		}
		if t.Date == "" {
			// Tagged before its name's date format was understood.
			t.Date = DateFromName(path)
		}
		if q.Matches(t) {
			matches = append(matches, Match{TagsFile: path, Tags: t})
		}
		return nil
	})
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Tags.Date != matches[j].Tags.Date {
			return matches[i].Tags.Date > matches[j].Tags.Date
		}
		return matches[i].TagsFile < matches[j].TagsFile
	})
	return matches, skipped, err
}
