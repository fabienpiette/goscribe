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
	ok := Librarian{AllowedTools: []string{
		"Bash(gh search:*)", "Bash(gh pr view:*)", "Bash(gh issue list:*)", "Bash(gh pr checks:*)",
		"Bash(git status:*)", "Bash(git ls-files:*)", "Read(~/notes/**)", "Grep(//srv/docs/**)",
		"mcp__tracker__list_issues", "mcp__wiki__wiki-search", "mcp__x__getIssue",
		"Bash(ls:*)", "Bash(gh pr list --limit 5:*)", "Bash(/usr/bin/gh search:*)",
	}}
	if err := ok.WithDefaults().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, bad := range []string{
		"Bash", "Bash(*)", "WebFetch", "WebSearch",
		"mcp__chat__chat_send_message", "mcp__tracker__save_issue",
		"Bash(gh pr comment:*)",
		"mcp__chat__chat_schedule_message", "mcp__tracker__share_issue",
		"mcp__x__trash_thread", "mcp__x__add_reaction", "mcp__x__createIssue",
		"mcp__x__issue-sendMessage", "mcp__x__do_thing", "mcp__x", "mcp__x__*",
		"Write(/tmp/*)", "Edit(src/*)", "MultiEdit(src/*)", "NotebookEdit(a.ipynb)",
		"Bash()", "Bash( gh:*)", "Bash(*:*)", "Bash(gh api:*)", "Bash(gh:*)",
		"Bash(curl:*)", "Bash(git:*)", "Bash(git push:*)", "Bash(gh pr review:*)",
		"Bash(git blame:*)", "Read(~/notes/../../../**)", "Grep(//srv/../etc/**)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git diff:*)", "Bash(git grep:*)",
		"Read", "Grep", "Glob", "LS", "Read(/**)", "Read(~/**)", "Glob(**)", "Read(/)", "Read(//**)", "Read(~)", "Read(~/)", "Grep(*)", "Read()",
		"Bash(gh pr close:*)", "Bash(gh issue edit:*)", "Bash(git checkout:*)", " Bash", "Bash(sh:*)", "Bash(env:*)",
		// several rules smuggled into one entry (claude splits on ',' and spaces)
		"Bash(ls),Bash", "Bash(ls) Bash", "Read(~/notes/**),Read", "mcp__srv__get_x,mcp__srv",
		"Bash(ls:*),WebFetch", "Read(~/notes/**) Bash", "Bash(ls))", "Bash((ls)", "Bash(ls)x",
		" Bash(ls:*)", "Bash(ls:*) ", "Read (~/notes/**)", "mcp__srv__get_x mcp__srv__get_y",
		"mcp__srv__get_x(arg)", "mcp__srv__get.x", "mcp__srv____get_x2,Bash",
		// unlisted built-ins
		"PowerShell", "PowerShell(Get-Item:*)", "Task", "Agent", "bash", "bash(ls:*)", "read(~/notes/**)",
		"TodoRead", "KillShell", "BashOutput",
		// shells, interpreters, wrappers and coreutils that change files
		"Bash(zsh:*)", "Bash(dash:*)", "Bash(fish:*)", "Bash(ksh:*)", "Bash(python:*)", "Bash(python3 -c:*)",
		"Bash(node:*)", "Bash(perl:*)", "Bash(ruby:*)", "Bash(php:*)", "Bash(awk:*)", "Bash(sed:*)",
		"Bash(find:*)", "Bash(rm:*)", "Bash(mv:*)", "Bash(cp:*)", "Bash(tee:*)", "Bash(dd:*)",
		"Bash(sudo:*)", "Bash(doas:*)", "Bash(timeout:*)", "Bash(nohup:*)", "Bash(nice:*)",
		"Bash(command:*)", "Bash(exec:*)", "Bash(eval:*)", "Bash(ssh:*)", "Bash(scp:*)",
		"Bash(docker:*)", "Bash(kubectl:*)", "Bash(/bin/sh:*)", "Bash(xargs:*)", "Bash(wget:*)",
		"Bash(gh auth status:*)", "Bash(gh auth token:*)", "Bash(gh config list:*)", "Bash(gh config get:*)",
	} {
		l := Librarian{AllowedTools: []string{"Read(~/notes/**)", bad}}.WithDefaults()
		err := l.Validate()
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("entry %q: error = %v, want one naming it", bad, err)
		}
	}

	err := Librarian{}.WithDefaults().Validate()
	if err == nil || !strings.Contains(err.Error(), "librarian.allowed_tools is empty") {
		t.Errorf("empty list: error = %v", err)
	}

	err = Librarian{Provider: "codex", AllowedTools: []string{"Read(~/notes/**)"}}.WithDefaults().Validate()
	if err == nil || !strings.Contains(err.Error(), "only claude") {
		t.Errorf("codex: error = %v", err)
	}

	err = Librarian{AllowedTools: []string{"Read(~/notes/**)"}, MaxLookups: -1}.WithDefaults().Validate()
	if err == nil {
		t.Error("negative max_lookups: want error")
	}
}

func TestLibrarianWanted(t *testing.T) {
	yes, no := true, false
	tools := []string{"Read(~/notes/**)"}
	for name, tc := range map[string]struct {
		l    Librarian
		want bool
	}{
		"no tools":     {Librarian{}, false},
		"tools":        {Librarian{AllowedTools: tools}, true},
		"tools, off":   {Librarian{AllowedTools: tools, Enabled: &no}, false},
		"tools, on":    {Librarian{AllowedTools: tools, Enabled: &yes}, true},
		"no tools, on": {Librarian{Enabled: &yes}, false},
	} {
		if got := tc.l.Wanted(); got != tc.want {
			t.Errorf("%s: Wanted() = %v, want %v", name, got, tc.want)
		}
	}
}

func TestLibrarianYAML(t *testing.T) {
	var cfg Config
	src := "librarian:\n  allowed_tools: [Read(~/notes/**)]\n  use_claude_settings: true\n  timeout: 90s\n  max_lookups: 3\n"
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Librarian.Timeout != 90*time.Second {
		t.Errorf("Timeout = %v, want 90s", cfg.Librarian.Timeout)
	}
	if !cfg.Librarian.UseClaudeSettings {
		t.Error("UseClaudeSettings = false, want true")
	}
	if cfg.Librarian.MaxLookups != 3 {
		t.Errorf("MaxLookups = %d, want 3", cfg.Librarian.MaxLookups)
	}
	if cfg.Librarian.Enabled != nil {
		t.Errorf("Enabled = %v, want nil when unset", *cfg.Librarian.Enabled)
	}
	var off Config
	if err := yaml.Unmarshal([]byte("librarian:\n  enabled: false\n"), &off); err != nil {
		t.Fatal(err)
	}
	if off.Librarian.Enabled == nil || *off.Librarian.Enabled {
		t.Errorf("Enabled = %v, want false", off.Librarian.Enabled)
	}
}

func TestHarnessModels(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("codex_model: gpt-x\nclaude_model: sonnet\n"), &c); err != nil {
		t.Fatal(err)
	}
	got := c.HarnessModels()
	if got["codex"] != "gpt-x" || got["claude"] != "sonnet" || len(got) != 2 {
		t.Errorf("HarnessModels = %v", got)
	}
	if len((&Config{}).HarnessModels()) != 0 {
		t.Error("no models configured → empty map")
	}
}

func TestHermesProviderAndModel(t *testing.T) {
	if !IsValidProvider("hermes") {
		t.Error("hermes must be a valid provider")
	}
	c := Config{HermesModel: "anthropic/x"}
	if c.HarnessModels()["hermes"] != "anthropic/x" {
		t.Errorf("HarnessModels = %v", c.HarnessModels())
	}
}
