package tags

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQuery(t *testing.T) {
	q, err := ParseQuery([]string{"Dark-Mode", "person:Alex", "type:1:1", "1:1", "topics:tokens"}, "2026-09-01", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Term{
		{Field: "", Value: "dark-mode"},
		{Field: "people", Value: "alex"},
		{Field: "type", Value: "1:1"},
		{Field: "", Value: "1:1"},
		{Field: "topics", Value: "tokens"},
	}
	if len(q.Terms) != len(want) {
		t.Fatalf("terms = %v, want %v", q.Terms, want)
	}
	for i := range want {
		if q.Terms[i] != want[i] {
			t.Errorf("term %d = %+v, want %+v", i, q.Terms[i], want[i])
		}
	}

	for _, bad := range []struct {
		args         []string
		since, until string
	}{
		{args: []string{"topci:x"}},
		{args: []string{"topic:"}},
		{since: "2026-13-01"},
		{until: "yesterday"},
	} {
		if _, err := ParseQuery(bad.args, bad.since, bad.until); err == nil {
			t.Errorf("ParseQuery(%v, %q, %q) should fail", bad.args, bad.since, bad.until)
		}
	}
}

func TestQueryMatches(t *testing.T) {
	meeting := Tags{
		Date: "2026-10-13", Type: "1:1", Summary: "Sync on the token migration",
		Topics: []string{"design-tokens", "dark-mode"}, People: []string{"Alexis"},
		Tickets: []string{"PROJ-123"}, Projects: []string{"billing"},
	}
	cases := []struct {
		args         []string
		since, until string
		want         bool
	}{
		{args: []string{"token"}, want: true},                     // substring in topics/summary
		{args: []string{"ALEX"}, want: true},                      // case-insensitive
		{args: []string{"person:alex", "topic:dark"}, want: true}, // all terms
		{args: []string{"person:alex", "topic:hiring"}, want: false},
		{args: []string{"topic:billing"}, want: false}, // field-scoped: billing is a project
		{args: []string{"ticket:proj-123"}, want: true},
		{args: []string{"type:1:1"}, want: true},
		{since: "2026-10-13", until: "2026-10-13", want: true},
		{since: "2026-10-14", want: false},
		{until: "2026-10-12", want: false},
		{want: true}, // no terms lists everything
	}
	for _, c := range cases {
		q, err := ParseQuery(c.args, c.since, c.until)
		if err != nil {
			t.Fatalf("ParseQuery(%v): %v", c.args, err)
		}
		if got := q.Matches(meeting); got != c.want {
			t.Errorf("Matches(%v since=%q until=%q) = %v, want %v", c.args, c.since, c.until, got, c.want)
		}
	}

	undated, _ := ParseQuery(nil, "2026-01-01", "")
	if undated.Matches(Tags{Topics: []string{"x"}}) {
		t.Error("a date filter should exclude tags without a date")
	}
}

func TestSearch(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, tg Tags) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := Write(path, Normalize(tg)); err != nil {
			t.Fatal(err)
		}
	}
	write("20261001/a-reduce-tags.yml", Tags{Date: "2026-10-01", Topics: []string{"dark-mode"}})
	write("20261013/b-reduce-tags.yml", Tags{Date: "2026-10-13", Topics: []string{"dark-mode", "hiring"}})
	write("old/2024/deep/c-tags.yml", Tags{Date: "2024-03-13", Topics: []string{"dark-mode"}})
	write("20261002/other-tags.yml", Tags{Date: "2026-10-02", Topics: []string{"hiring"}})
	if err := os.WriteFile(filepath.Join(root, "20261002", "broken-tags.yml"), []byte("topics: [unclosed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "20261001", "a-reduce-transcript.txt"), []byte("t"), 0644); err != nil {
		t.Fatal(err)
	}
	// Live-assistant notes filed with the recording.
	if err := os.WriteFile(filepath.Join(root, "20261001", "a-reduce-assist.md"), []byte("n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "20261001", "a-reduce-openai-meeting-summary.txt"), []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}

	q, _ := ParseQuery([]string{"dark"}, "", "")
	matches, skipped, err := Search(root, q)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var dates []string
	for _, m := range matches {
		dates = append(dates, m.Tags.Date)
	}
	if strings.Join(dates, ",") != "2026-10-13,2026-10-01,2024-03-13" {
		t.Errorf("dates = %v, want newest first, deep folders included", dates)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Error(), "broken-tags.yml") {
		t.Errorf("skipped = %v, want the broken file", skipped)
	}

	a := matches[1]
	if a.Transcript() != filepath.Join(root, "20261001", "a-reduce-transcript.txt") {
		t.Errorf("Transcript() = %q", a.Transcript())
	}
	if related := a.Related(); len(related) != 2 || !strings.HasSuffix(related[0], "-assist.md") || !strings.HasSuffix(related[1], "-openai-meeting-summary.txt") {
		t.Errorf("Related() = %v", related)
	}

	if _, _, err := Search(filepath.Join(root, "missing"), q); err == nil {
		t.Error("a missing root should be an error")
	}
}

// Tags written before a name's date format was understood have no date: the
// records/<YYYYMMDD>/ folder stands in, so -since doesn't silently drop them.
func TestSearchDateFromFolder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "20261015", "team-sync-2026_10_15-reduce-tags.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, Normalize(Tags{Topics: []string{"dark-mode"}})); err != nil {
		t.Fatal(err)
	}

	q, _ := ParseQuery([]string{"dark-mode"}, "2026-10-01", "")
	matches, _, err := Search(root, q)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 1 || matches[0].Tags.Date != "2026-10-15" {
		t.Errorf("matches = %+v, want the undated meeting dated from its folder", matches)
	}
}
