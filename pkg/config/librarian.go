package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Librarian configures the read-only Claude Code session that "goscribe assist"
// can ask to look things up (issues, PRs, docs) while it runs.
type Librarian struct {
	// Only "claude" is supported (default).
	Provider string `yaml:"provider,omitempty"`
	// Claude Code tool rules the librarian may use. Required, and every entry
	// must be read-only (see Validate).
	AllowedTools []string `yaml:"allowed_tools,omitempty"`
	// Extra instructions appended to the librarian's prompt.
	Instructions string `yaml:"instructions,omitempty"`
	// Upper bound for one lookup (default 3m).
	Timeout time.Duration `yaml:"timeout,omitempty"`
	// Upper bound on lookups per assist session (default 8).
	MaxLookups int `yaml:"max_lookups,omitempty"`
	// Model override passed to the Claude CLI.
	Model string `yaml:"model,omitempty"`
	// Path to an MCP config file for the librarian session.
	MCPConfig string `yaml:"mcp_config,omitempty"`
}

const (
	defaultLibrarianProvider   = "claude"
	defaultLibrarianTimeout    = 3 * time.Minute
	defaultLibrarianMaxLookups = 8
)

// WithDefaults fills an empty Provider, Timeout and MaxLookups.
func (l Librarian) WithDefaults() Librarian {
	if l.Provider == "" {
		l.Provider = defaultLibrarianProvider
	}
	if l.Timeout == 0 {
		l.Timeout = defaultLibrarianTimeout
	}
	if l.MaxLookups == 0 {
		l.MaxLookups = defaultLibrarianMaxLookups
	}
	return l
}

// Verbs that make an MCP tool or a gh/git subcommand a write operation.
const writeVerbs = "send|save|create|update|delete|remove|post|write|add|edit|merge|close|reopen|comment|review|assign|archive|upload|push|set|move|transfer|label|unlabel|reply|publish|approve"

var (
	mcpWriteRe = regexp.MustCompile(`(^|_)(` + writeVerbs + `)(_|$)`)
	// A Bash rule whose command is a gh or git write subcommand.
	bashWriteRe = regexp.MustCompile(`(^|[\s;&|(])(gh|git)\s+(\S+\s+)*(` + writeVerbs + `)(\s|:|\)|$)`)
)

// Validate checks the provider, the limits and that every allowed tool is
// read-only. The guard is deliberately strict: unrestricted Bash, web tools and
// anything that looks like a write (mcp__*__send_*, gh pr comment, ...) is refused.
func (l Librarian) Validate() error {
	if l.Provider != "" && l.Provider != defaultLibrarianProvider {
		return fmt.Errorf("librarian.provider '%s' is not supported (only claude)", l.Provider)
	}
	if l.MaxLookups < 0 {
		return fmt.Errorf("librarian.max_lookups %d is invalid (must be >= 0)", l.MaxLookups)
	}
	if l.Timeout < 0 {
		return fmt.Errorf("librarian.timeout %s is invalid (must be >= 0)", l.Timeout)
	}
	if len(l.AllowedTools) == 0 {
		return fmt.Errorf("librarian.allowed_tools is empty (list the read-only tools the librarian may use)")
	}
	for _, tool := range l.AllowedTools {
		if reason := unsafeTool(tool); reason != "" {
			return fmt.Errorf("librarian.allowed_tools entry '%s' is refused: %s", tool, reason)
		}
	}
	return nil
}

// unsafeTool returns why a tool rule is not read-only, or "" if it passes.
func unsafeTool(rule string) string {
	t := strings.TrimSpace(rule)
	lower := strings.ToLower(t)

	switch {
	case t == "Bash" || t == "Bash()" || strings.HasPrefix(t, "Bash(*"):
		return "unrestricted Bash; scope it, e.g. Bash(gh search:*)"
	case t == "WebFetch" || t == "WebSearch" || strings.HasPrefix(t, "WebFetch(") || strings.HasPrefix(t, "WebSearch("):
		return "web tools can exfiltrate data"
	case strings.HasPrefix(t, "Bash("):
		if bashWriteRe.MatchString(lower) {
			return "looks like a write command"
		}
	case strings.HasPrefix(t, "mcp__"):
		parts := strings.SplitN(lower, "__", 3)
		tool := parts[len(parts)-1]
		if len(parts) < 3 || tool == "" || strings.Contains(tool, "*") {
			return "name the exact MCP tools, no wildcards"
		}
		if mcpWriteRe.MatchString(tool) {
			return "looks like a write tool"
		}
	case t == "Write" || t == "Edit" || t == "MultiEdit" || t == "NotebookEdit":
		return "file edits are not read-only"
	}
	return ""
}
