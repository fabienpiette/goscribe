package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"goscribe/internal/util"
)

// ResearchOptions configures a read-only Claude Code session that may look
// things up with a strict tool allowlist.
type ResearchOptions struct {
	AllowedTools []string // e.g. "Bash(gh search:*)", "Read", "mcp__srv__tool"
	Timeout      time.Duration
	Model        string
	MCPConfig    string // path to an MCP config file; empty means no MCP servers
}

const defaultResearchTimeout = 3 * time.Minute

// researchArgs builds the claude flags: only the allowed tools exist, anything
// else is denied without prompting, and no session, slash commands or (unless
// an MCP config is given) MCP servers and settings are loaded.
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

	args := []string{"-p", "--output-format", "text",
		"--tools", strings.Join(builtins, ","),
		"--allowedTools", strings.Join(o.AllowedTools, ","),
		"--disallowedTools", "WebFetch,WebSearch",
		"--permission-mode", "dontAsk",
		"--no-session-persistence",
		"--disable-slash-commands",
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.MCPConfig != "" {
		args = append(args, "--mcp-config", o.MCPConfig, "--strict-mcp-config", "--setting-sources", "")
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
	workDir, err := os.MkdirTemp("", "goscribe-research-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultResearchTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, researchArgs(o)...)
	cmd.Dir = workDir
	cmd.WaitDelay = time.Second // do not wait on grandchildren holding the pipes
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("research timed out after %s", timeout)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("research failed: %v: %s", err, util.TruncateString(detail, 500))
	}
	answer := strings.TrimSpace(stdout.String())
	if answer == "" {
		return "", fmt.Errorf("research returned an empty answer")
	}
	return answer, nil
}
