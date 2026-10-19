package harness

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ResearchOptions configures a read-only Claude Code session that may look
// things up with a strict tool allowlist.
type ResearchOptions struct {
	AllowedTools []string // e.g. "Bash(gh search:*)", "Read(~/notes/**)", "mcp__srv__tool"
	Timeout      time.Duration
	Model        string
	MCPConfig    string // path to an MCP config file; empty means no MCP servers
	// UserSettings loads the user's Claude settings (--setting-sources user):
	// connectors, plugins, hooks. Off by default.
	UserSettings bool
}

const defaultResearchTimeout = 3 * time.Minute

// researchArgs builds the claude flags: only the allowed tools exist, anything
// else is denied without prompting, and no session or slash commands are loaded.
// User settings are not loaded by default (--setting-sources ""): their
// permissions.allow rules would widen the allowlist, and their hooks and plugins
// would run. Then plugin and connector MCP servers are unavailable, so MCP tools
// need an MCPConfig file. With UserSettings they are loaded (--setting-sources user),
// and an MCPConfig adds its servers to them (no --strict-mcp-config).
func researchArgs(o ResearchOptions) []string {
	seen := map[string]bool{}
	var builtins []string
	for _, t := range o.AllowedTools {
		if strings.HasPrefix(t, "mcp__") {
			continue
		}
		name, _, _ := strings.Cut(t, "(")
		if !seen[name] {
			seen[name] = true
			builtins = append(builtins, name)
		}
	}
	sort.Strings(builtins)

	sources := ""
	if o.UserSettings {
		sources = "user"
	}
	args := []string{"-p", "--output-format", "text",
		"--tools", strings.Join(builtins, ","),
		"--allowedTools", strings.Join(o.AllowedTools, ","),
		"--disallowedTools", "WebFetch,WebSearch",
		"--permission-mode", "dontAsk",
		"--no-session-persistence",
		"--disable-slash-commands",
		"--setting-sources", sources,
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.MCPConfig != "" {
		args = append(args, "--mcp-config", o.MCPConfig)
		if !o.UserSettings {
			args = append(args, "--strict-mcp-config")
		}
	}
	return args
}

// Research runs prompt (on stdin) through a restricted Claude Code session
// from an empty temp dir and returns its trimmed answer.
func Research(prompt string, o ResearchOptions) (string, error) {
	bin, err := exec.LookPath(ClaudeBinary)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH (install Claude Code)", ClaudeBinary)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultResearchTimeout
	}
	args := researchArgs(o)
	// No label: the caller says "librarian failed: <cause>".
	return runCLI("", bin, prompt, timeout, "", func(string) []string { return args })
}
