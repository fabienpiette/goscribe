package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/internal/tags"
)

// searchRecords builds a small records/<date>/ tree with two tagged meetings.
func searchRecords(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, tg tags.Tags) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := tags.Write(path, tags.Normalize(tg)); err != nil {
			t.Fatal(err)
		}
	}
	write("20261001/a-reduce-tags.yml", tags.Tags{Date: "2026-10-01", Type: "standup", Summary: "Dark mode rollout",
		Topics: []string{"dark-mode"}, People: []string{"Adrien"}})
	write("20261013/b-reduce-tags.yml", tags.Tags{Date: "2026-10-13", Type: "1:1", Summary: "Token migration",
		Topics: []string{"design-tokens", "dark-mode"}, Tickets: []string{"LIV-17445"}})
	if err := os.WriteFile(filepath.Join(root, "20261013", "b-reduce-openai-meeting-summary.txt"), []byte("s"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func search(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runSearch(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestSearchCommand(t *testing.T) {
	root := searchRecords(t)

	// Flags after terms still apply (the flag package alone would stop at "dark").
	out, _, code := search(t, "dark", "-root", root)
	if code != 0 {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "2026-10-13") || !strings.HasPrefix(lines[1], "2026-10-01") {
		t.Errorf("want two meetings newest first plus a count, got:\n%s", out)
	}
	if !strings.Contains(lines[0], filepath.Join("20261013", "b-reduce-transcript.txt")) {
		t.Errorf("result should show the transcript path relative to root:\n%s", out)
	}
	if lines[2] != "2 meeting(s) found" {
		t.Errorf("count line = %q", lines[2])
	}

	out, _, _ = search(t, "-root", root, "person:adri")
	if !strings.Contains(out, "Dark mode rollout") || strings.Contains(out, "Token migration") {
		t.Errorf("person:adri should only find the standup:\n%s", out)
	}

	out, _, _ = search(t, "-root", root, "-since", "2026-10-02", "-paths", "dark")
	if strings.TrimSpace(out) != filepath.Join(root, "20261013", "b-reduce-transcript.txt") {
		t.Errorf("-paths should print only full transcript paths:\n%s", out)
	}

	out, _, _ = search(t, "-root", root, "-v", "ticket:liv")
	for _, want := range []string{"topics: dark-mode, design-tokens", "tickets: LIV-17445", "file: " + filepath.Join("20261013", "b-reduce-openai-meeting-summary.txt")} {
		if !strings.Contains(out, want) {
			t.Errorf("-v output missing %q:\n%s", want, out)
		}
	}
}

func TestSearchCommandExitCodes(t *testing.T) {
	root := searchRecords(t)

	if out, _, code := search(t, "-root", root, "hiring"); code != 1 || !strings.Contains(out, "0 meeting(s) found") {
		t.Errorf("no match: exit = %d, output %q", code, out)
	}
	if _, errOut, code := search(t, "-root", root, "topci:x"); code != 2 || !strings.Contains(errOut, "unknown field") {
		t.Errorf("bad field: exit = %d, stderr %q", code, errOut)
	}
	if _, _, code := search(t, "-root", filepath.Join(root, "missing"), "x"); code != 2 {
		t.Errorf("missing root: exit = %d, want 2", code)
	}
	if _, _, code := search(t, "-h"); code != 0 {
		t.Errorf("-h: exit = %d, want 0", code)
	}
}

func TestSearchCommandRootFromConfig(t *testing.T) {
	root := searchRecords(t)
	cfg := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(cfg, []byte("tags_root: "+root+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, _, code := search(t, "-config", cfg, "tokens")
	if code != 0 || !strings.Contains(out, "Token migration") {
		t.Errorf("tags_root from config not used: exit = %d\n%s", code, out)
	}
}

func TestShorten(t *testing.T) {
	if got := shorten("short", 80); got != "short" {
		t.Errorf("shorten kept = %q", got)
	}
	long := strings.Repeat("é", 100)
	got := shorten(long, 80)
	if len([]rune(got)) != 80 || !strings.HasSuffix(got, "…") {
		t.Errorf("shorten(100 runes) = %d runes %q", len([]rune(got)), got)
	}
}
