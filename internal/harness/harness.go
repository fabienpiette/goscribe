// Package harness runs post-processing through a local coding-agent CLI
// (Claude Code or Codex) instead of calling an LLM API directly. It uses the
// CLI's own login and default model, and gives it no tools.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"goscribe/internal/util"
	"goscribe/pkg/config"
)

// Binaries can be overridden for tests.
var (
	ClaudeBinary = "claude"
	CodexBinary  = "codex"
	HermesBinary = "hermes"
)

// codexDisabledFeatures turns off every Codex capability that lets the model act
// (shell, browser, computer use, apps, plugins, hooks). The transcript is untrusted
// input: without this, a spoken prompt injection could make Codex read local files
// (a read-only sandbox still allows reads) and leak them into the action output.
var codexDisabledFeatures = []string{
	"shell_tool", "unified_exec", "apps", "browser_use", "browser_use_external",
	"computer_use", "in_app_browser", "plugins", "remote_plugin", "hooks", "tool_suggest",
}

// Timeout bounds a single harness call.
var Timeout = 20 * time.Minute

// Models picks the model per harness ("claude", "codex", "hermes"); a harness missing
// from it uses its CLI's default model. Set from the config at startup.
var Models = map[string]string{}

func IsHarness(name string) bool {
	return name == "claude" || name == "codex" || name == "hermes"
}

// Run sends prompt on stdin to the named harness and returns its final answer.
// It runs from an empty temp dir so no project instructions or files are picked up.
func Run(name, prompt string) (string, error) {
	switch name {
	case "claude":
		bin, err := exec.LookPath(ClaudeBinary)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH (install Claude Code)", ClaudeBinary)
		}
		// No tools, no MCP servers, no user/project settings (hooks, plugins), no saved session.
		args := []string{"-p",
			"--output-format", "text",
			"--tools", "",
			"--strict-mcp-config",
			"--no-session-persistence",
			"--setting-sources", "",
			"--disable-slash-commands",
		}
		if m := Models[name]; m != "" {
			args = append(args, "--model", m)
		}
		return runCLI(name, bin, prompt, Timeout, "", func(string) []string { return args })
	case "codex":
		bin, err := exec.LookPath(CodexBinary)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH (install Codex CLI)", CodexBinary)
		}
		// No tools, no user config (MCP servers, profiles), no web search, no saved session.
		return runCLI(name, bin, prompt, Timeout, "answer.txt", func(workDir string) []string {
			args := []string{"exec",
				"--skip-git-repo-check",
				"--sandbox", "read-only",
				"--ephemeral",
				"--ignore-user-config",
				"-c", `web_search="disabled"`,
				"--color", "never",
				"--output-last-message", filepath.Join(workDir, "answer.txt"),
			}
			for _, f := range codexDisabledFeatures {
				args = append(args, "--disable", f)
			}
			if m := Models[name]; m != "" {
				args = append(args, "-m", m)
			}
			return append(args, "-")
		})
	case "hermes":
		bin, err := exec.LookPath(HermesBinary)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH (install Hermes Agent)", HermesBinary)
		}
		// No toolsets: Hermes has no "none" toolset, and an unknown name enables
		// nothing (an empty -t would enable them all). No AGENTS.md, SOUL.md or
		// memory; sessions are tagged as a tool's, out of the user's lists.
		args := []string{"chat", "--query-file", "-", "--oneshot",
			"-t", "none",
			"--ignore-rules",
			"--source", "tool",
			"--format", "stream-json",
		}
		if m := Models[name]; m != "" {
			args = append(args, "-m", m)
		}
		out, err := runCLI(name, bin, prompt, Timeout, "", func(string) []string { return args })
		if err != nil {
			return "", err
		}
		return hermesAnswer(out)
	default:
		return "", fmt.Errorf("unknown harness '%s' (valid: claude, codex, hermes)", name)
	}
}

// hermesEvent is one line of hermes --format stream-json.
type hermesEvent struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Text     string `json:"text"`
	ExitCode int    `json:"exit_code"`
}

// hermesAnswer reads the final text from a Hermes event stream. It fails
// closed: the transcript is untrusted, so an answer reached with any tool is
// discarded, as -t none should have left none. Non-JSON lines (warnings) are
// skipped.
func hermesAnswer(stream string) (string, error) {
	var result *hermesEvent
	for _, line := range strings.Split(stream, "\n") {
		var e hermesEvent
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &e) != nil {
			continue
		}
		switch e.Type {
		case "tool_use":
			return "", fmt.Errorf("hermes used a tool (%s) although none is allowed; answer discarded", e.Name)
		case "result":
			result = &e
		}
	}
	if result == nil {
		return "", fmt.Errorf("hermes gave no result: %s", lastLines(stream, maxErrorDetail))
	}
	text := strings.TrimSpace(result.Text)
	if result.ExitCode != 0 {
		return "", fmt.Errorf("hermes failed (exit code %d): %s", result.ExitCode, lastLines(text, maxErrorDetail))
	}
	if text == "" {
		return "", fmt.Errorf("hermes returned an empty answer")
	}
	return text, nil
}

// runCLI runs bin with prompt on stdin from a fresh empty temp dir (removed
// afterwards) and returns the trimmed answer: stdout, or the file outName in
// the temp dir when outName is set. label names the caller in errors; with an
// empty label the errors give only the cause, for a caller that adds its own.
func runCLI(label, bin, prompt string, timeout time.Duration, outName string, args func(workDir string) []string) (string, error) {
	workDir, err := os.MkdirTemp("", "goscribe-harness-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args(workDir)...)
	cmd.Dir = workDir
	cmd.WaitDelay = time.Second // do not wait on grandchildren holding the pipes
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%stimed out after %s", subject(label), timeout)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if label == "" {
			return "", fmt.Errorf("%v: %s", err, lastLines(detail, maxErrorDetail))
		}
		return "", fmt.Errorf("%s failed: %v: %s", label, err, lastLines(detail, maxErrorDetail))
	}

	answer := stdout.String()
	if outName != "" {
		data, err := os.ReadFile(filepath.Join(workDir, outName))
		if err != nil {
			return "", fmt.Errorf("reading the %sanswer: %w", subject(label), err)
		}
		answer = string(data)
	}

	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("%sreturned an empty answer", subject(label))
	}
	return answer, nil
}

// maxErrorDetail bounds the CLI output kept in an error.
const maxErrorDetail = 300

// lastLines keeps the end of a CLI's output, where the error is: Codex, for
// one, prints a long banner first. Whole lines are kept while they fit in max
// bytes, joined with " | "; a single longer line keeps its end.
func lastLines(s string, max int) string {
	var kept []string
	size := 0
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if size+len(l) > max {
			if len(kept) == 0 {
				r := []rune(l)
				kept = append(kept, "…"+string(r[len(r)-min(len(r), max/2):]))
			}
			break
		}
		kept = append([]string{l}, kept...)
		size += len(l) + 3
	}
	return strings.Join(kept, " | ")
}

// subject is label followed by a space, or "" for no label.
func subject(label string) string {
	if label == "" {
		return ""
	}
	return label + " "
}

// Process applies a post-processing action to the whole transcript. Harness
// models have large context windows, so the transcript is not chunked.
func Process(transcript string, action *config.PostAction, name string) (string, error) {
	prompt := action.Prompt + "\n\n" +
		"You are a helpful assistant that processes transcribed text according to user instructions.\n\n" +
		"Transcript:\n" + transcript + "\n\n" +
		"Please process this transcript according to the instructions above. " +
		"Reply with the result only, without any preamble."
	return Run(name, prompt)
}

func SelectBestActions(transcript string, actions []config.PostAction, name string) ([]string, error) {
	var descriptions []string
	for _, action := range actions {
		descriptions = append(descriptions, fmt.Sprintf("- %s: %s", action.ID, action.Description))
	}

	prompt := fmt.Sprintf(`Analyze the following transcript and select the 2-3 most appropriate post-processing actions from the list below.

Available actions:
%s

Transcript preview (first 2000 chars):
%s

Based on the content, which actions would provide the most value? Reply ONLY with a comma-separated list of action IDs (e.g., "openai-meeting-summary,openai-action-items"). Do not include any explanation.`,
		strings.Join(descriptions, "\n"),
		util.TruncateString(transcript, 2000))

	response, err := Run(name, prompt)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	var validIDs []string
	for _, id := range strings.Split(strings.Trim(response, "\"` \n"), ",") {
		id = strings.Trim(strings.TrimSpace(id), "\"`")
		if config.FindAction(actions, id) != nil {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid actions selected by %s", name)
	}
	return validIDs, nil
}
