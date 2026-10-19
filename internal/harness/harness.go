// Package harness runs post-processing through a local coding-agent CLI
// (Claude Code or Codex) instead of calling an LLM API directly. It uses the
// CLI's own login and default model, and gives it no tools.
package harness

import (
	"bytes"
	"context"
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

func IsHarness(name string) bool {
	return name == "claude" || name == "codex"
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
			return append(args, "-")
		})
	default:
		return "", fmt.Errorf("unknown harness '%s' (valid: claude, codex)", name)
	}
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
			return "", fmt.Errorf("%v: %s", err, util.TruncateString(detail, 500))
		}
		return "", fmt.Errorf("%s failed: %v: %s", label, err, util.TruncateString(detail, 500))
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
