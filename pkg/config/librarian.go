package config

import (
	"fmt"
	"path"
	"regexp"
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
	// false turns the librarian off although allowed_tools is set; default on
	// when allowed_tools is set.
	Enabled *bool `yaml:"enabled,omitempty"`
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
	// Load the user's Claude settings (connectors, plugins, hooks) in the
	// librarian session. Off by default: the session is isolated.
	UseClaudeSettings bool `yaml:"use_claude_settings,omitempty"`
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

// Wanted reports whether "goscribe assist" should start the librarian without
// being asked: tools are listed and enabled is not false.
func (l Librarian) Wanted() bool {
	return len(l.AllowedTools) > 0 && (l.Enabled == nil || *l.Enabled)
}

// writeVerbs are substrings that mark a tool rule as a possible write. They are
// matched against the lowercased rule, so createIssue and chat_sendMessage count.
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
	"status": true, "ls-files": true,
}

// Built-in file tools: only accepted with a folder scope.
var scopedBuiltins = map[string]bool{"Read": true, "Grep": true, "Glob": true, "LS": true}

// mcpTool is one exact MCP tool name: mcp__<server>__<tool>.
var mcpTool = regexp.MustCompile(`^mcp__[A-Za-z0-9_-]+__[A-Za-z0-9_-]+$`)

// Bash prefixes (first word) that run arbitrary commands (shells, interpreters,
// wrappers), reach the network, or change files.
var bannedBashCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true, "ksh": true,
	"env": true, "xargs": true, "curl": true, "wget": true,
	"python": true, "python3": true, "node": true, "perl": true, "ruby": true, "php": true,
	"awk": true, "sed": true, "find": true,
	"rm": true, "mv": true, "cp": true, "tee": true, "dd": true,
	"sudo": true, "doas": true, "timeout": true, "nohup": true, "nice": true,
	"command": true, "exec": true, "eval": true,
	"ssh": true, "scp": true, "docker": true, "kubectl": true,
}

// gh commands refused whatever follows: they print or change credentials and
// settings.
var bannedGh = map[string]bool{"auth": true, "config": true}

// Validate checks the provider, the limits and the allowed tools.
//
// The tool guard is a heuristic, not a proof of read-only: it accepts one rule
// per entry, only for Bash, Read, Grep, Glob, LS and exact MCP tools; it
// refuses entries whose lowercased text CONTAINS a write verb (substring match,
// so it can also refuse harmless names), requires MCP tools to carry a read
// verb, and refuses broad Bash prefixes. Review allowed_tools yourself too.
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
// Claude Code reads --allowedTools as a comma- or space-separated list, so an
// entry must hold exactly one rule: no ',' or whitespace outside its
// parentheses, and its arguments in a single (...) at the end.
func unsafeTool(rule string) string {
	lower := strings.ToLower(rule)
	for _, v := range writeVerbs {
		if strings.Contains(lower, v) {
			return fmt.Sprintf("contains write verb '%s'", v)
		}
	}

	name, args, hasArgs := strings.Cut(rule, "(")
	if name == "" || strings.ContainsFunc(name, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		return "one tool rule per entry, with no ',' or spaces outside the parentheses"
	}
	if hasArgs {
		if strings.Count(rule, "(") != 1 || strings.Count(rule, ")") != 1 || !strings.HasSuffix(rule, ")") {
			return "a rule's arguments go in one (...) at the end of the entry"
		}
		args = strings.TrimSuffix(args, ")")
	}

	switch {
	case strings.HasPrefix(name, "mcp__"):
		if hasArgs || !mcpTool.MatchString(rule) {
			return "name the exact MCP tools (mcp__server__tool), no server-wide or wildcard entries"
		}
		parts := strings.Split(rule, "__")
		for _, w := range splitWords(parts[len(parts)-1]) {
			if readVerbs[w] {
				return ""
			}
		}
		return "not a known read-only tool name"
	case name == "WebFetch" || name == "WebSearch":
		return "web tools can exfiltrate data"
	case name == "Bash":
		if !hasArgs {
			return "unrestricted Bash; scope it, e.g. Bash(gh search:*)"
		}
		return unsafeBashPrefix(args)
	case scopedBuiltins[name]:
		if !hasArgs {
			return "name a folder, e.g. " + name + "(~/notes/**); a bare " + name + " can read any file"
		}
		if strings.Contains(args, "..") {
			return "scope must not contain '..'"
		}
		if strings.Trim(args, "/*~. ") == "" {
			return "scope is empty or too broad; name a folder, e.g. " + name + "(~/notes/**)"
		}
		return ""
	}
	return "not an allowed tool (only Bash, Read, Grep, Glob, LS and mcp__server__tool)"
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
	switch cmd := path.Base(f[0]); {
	case bannedBashCommands[cmd]:
		return "Bash prefix '" + f[0] + "' can run arbitrary commands or change files"
	case cmd == "gh":
		if len(f) > 1 && bannedGh[f[1]] {
			return "gh " + f[1] + " handles credentials and settings"
		}
		for _, w := range f[1:min(len(f), 3)] {
			if readOnlyGh[w] {
				return ""
			}
		}
		return "gh rules must use a read-only subcommand (search, view, list, status, diff, checks)"
	case cmd == "git":
		if len(f) > 1 && readOnlyGit[f[1]] {
			return ""
		}
		return "git rules must use a read-only subcommand (status, ls-files; log, show, diff, grep and blame take options that run commands, write files or print any file)"
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
