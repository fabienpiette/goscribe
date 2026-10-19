package config

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLibrarianDefaults(t *testing.T) {
	l := Librarian{}.WithDefaults()
	if l.Provider != "claude" {
		t.Errorf("Provider = %q, want claude", l.Provider)
	}
	if l.Timeout != 3*time.Minute {
		t.Errorf("Timeout = %v, want 3m", l.Timeout)
	}
	if l.MaxLookups != 8 {
		t.Errorf("MaxLookups = %d, want 8", l.MaxLookups)
	}
}

func TestLibrarianValidate(t *testing.T) {
	ok := Librarian{AllowedTools: []string{"Bash(gh search:*)", "mcp__linear__list_issues", "Read"}}
	if err := ok.WithDefaults().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, bad := range []string{
		"Bash", "Bash(*)", "WebFetch", "WebSearch",
		"mcp__slack__slack_send_message", "mcp__linear__save_issue",
		"Bash(gh pr comment:*)",
	} {
		l := Librarian{AllowedTools: []string{"Read", bad}}.WithDefaults()
		err := l.Validate()
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("entry %q: error = %v, want one naming it", bad, err)
		}
	}

	err := Librarian{}.WithDefaults().Validate()
	if err == nil || !strings.Contains(err.Error(), "librarian.allowed_tools is empty") {
		t.Errorf("empty list: error = %v", err)
	}

	err = Librarian{Provider: "codex", AllowedTools: []string{"Read"}}.WithDefaults().Validate()
	if err == nil || !strings.Contains(err.Error(), "only claude") {
		t.Errorf("codex: error = %v", err)
	}

	err = Librarian{AllowedTools: []string{"Read"}, MaxLookups: -1}.WithDefaults().Validate()
	if err == nil {
		t.Error("negative max_lookups: want error")
	}
}

func TestLibrarianYAML(t *testing.T) {
	var cfg Config
	src := "librarian:\n  allowed_tools: [Read]\n  timeout: 90s\n  max_lookups: 3\n"
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Librarian.Timeout != 90*time.Second {
		t.Errorf("Timeout = %v, want 90s", cfg.Librarian.Timeout)
	}
	if cfg.Librarian.MaxLookups != 3 {
		t.Errorf("MaxLookups = %d, want 3", cfg.Librarian.MaxLookups)
	}
}
