package config

import (
	"fmt"
	"strings"
	"time"
	"unicode"
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

// writeVerbs are substrings that mark a tool rule as a possible write. They are
// matched against the lowercased rule, so createIssue and slack_sendMessage count.
var writeVerbs = []string{
	"send", "post", "create", "update", "delete", "save", "write", "edit",
	"comment", "merge", "share", "trash", "upload", "react", "schedule",
}

// readVerbs: an MCP tool name must contain at least one as a word.
var readVerbs = map[string]bool{
	"get": true, "list": true, "search": true, "read": true, "view": true,
	"fetch": true, "query": true, "find": true, "retrieve": true, "lookup": true,
}

// Read-only gh words (2nd or 3rd word of the rule) and git subcommands.
var readOnlyGh = map[string]bool{
	"search": true, "view": true, "list": true, "status": true, "diff": true, "checks": true,
}
var readOnlyGit = map[string]bool{
	"log": true, "show": true, "diff": true, "status": true, "blame": true, "grep": true, "ls-files": true,
}

// Bash prefixes that can run arbitrary or networked commands.
var bannedBashCommands = map[string]bool{
	"sh": true, "bash": true, "env": true, "xargs": true, "curl": true, "wget": true,
}

// Validate checks the provider, the limits and the allowed tools.
//
// The tool guard is a heuristic, not a proof of read-only: it refuses entries
// whose lowercased text CONTAINS a write verb (substring match, so it can also
// refuse harmless names), requires MCP tools to carry a read verb, and refuses
// broad Bash prefixes and web tools. Review allowed_tools yourself too.
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

// unsafeTool returns why a tool rule is not accepted, or "" if it passes.
func unsafeTool(rule string) string {
	rule = strings.TrimSpace(rule)
	lower := strings.ToLower(rule)
	for _, v := range writeVerbs {
		if strings.Contains(lower, v) {
			return fmt.Sprintf("contains write verb '%s'", v)
		}
	}

	name, args, hasArgs := strings.Cut(rule, "(")
	switch {
	case name == "WebFetch" || name == "WebSearch":
		return "web tools can exfiltrate data"
	case name == "Bash":
		if !hasArgs {
			return "unrestricted Bash; scope it, e.g. Bash(gh search:*)"
		}
		return unsafeBashPrefix(strings.TrimSuffix(args, ")"))
	case strings.HasPrefix(rule, "mcp__"):
		parts := strings.Split(rule, "__")
		last := parts[len(parts)-1]
		if len(parts) < 3 || last == "" || strings.Contains(rule, "*") {
			return "name the exact MCP tools, no server-wide or wildcard entries"
		}
		for _, w := range splitWords(last) {
			if readVerbs[w] {
				return ""
			}
		}
		return "not a known read-only tool name"
	}
	return ""
}

func unsafeBashPrefix(prefix string) string {
	prefix = strings.TrimSuffix(prefix, "*")
	prefix = strings.TrimSuffix(prefix, ":")
	if prefix == "" || strings.HasPrefix(prefix, "*") {
		return "empty or wildcard Bash prefix"
	}
	if strings.TrimLeftFunc(prefix, unicode.IsSpace) != prefix {
		return "Bash prefix must not start with whitespace"
	}
	f := strings.Fields(prefix)
	switch {
	case bannedBashCommands[f[0]]:
		return "Bash prefix '" + f[0] + "' can run arbitrary commands"
	case f[0] == "gh":
		for _, w := range f[1:min(len(f), 3)] {
			if readOnlyGh[w] {
				return ""
			}
		}
		return "gh rules must use a read-only subcommand (search, view, list, status, diff, checks)"
	case f[0] == "git":
		if len(f) > 1 && readOnlyGit[f[1]] {
			return ""
		}
		return "git rules must use a read-only subcommand (log, show, diff, status, blame, grep, ls-files)"
	}
	return ""
}

// splitWords lowercases s and splits it on '_', '-' and camelCase boundaries.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	for _, r := range s {
		switch {
		case r == '_' || r == '-':
			flush()
		case unicode.IsUpper(r):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}
