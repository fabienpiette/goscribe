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

// Timeout bounds a single harness call.
var Timeout = 20 * time.Minute

func IsHarness(name string) bool {
	return name == "claude" || name == "codex"
}

// Run sends prompt on stdin to the named harness and returns its final answer.
// It runs from an empty temp dir so no project instructions or files are picked up.
func Run(name, prompt string) (string, error) {
	workDir, err := os.MkdirTemp("", "goscribe-harness-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	var cmd *exec.Cmd
	outFile := ""
	switch name {
	case "claude":
		bin, err := exec.LookPath(ClaudeBinary)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH (install Claude Code)", ClaudeBinary)
		}
		// No tools, no MCP servers, no user/project settings (hooks, plugins), no saved session.
		cmd = exec.CommandContext(ctx, bin, "-p",
			"--output-format", "text",
			"--tools", "",
			"--strict-mcp-config",
			"--no-session-persistence",
			"--setting-sources", "",
			"--disable-slash-commands",
		)
	case "codex":
		bin, err := exec.LookPath(CodexBinary)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH (install Codex CLI)", CodexBinary)
		}
		outFile = filepath.Join(workDir, "answer.txt")
		cmd = exec.CommandContext(ctx, bin, "exec",
			"--skip-git-repo-check",
			"--sandbox", "read-only",
			"--ephemeral",
			"--color", "never",
			"--output-last-message", outFile,
			"-",
		)
	default:
		return "", fmt.Errorf("unknown harness '%s' (valid: claude, codex)", name)
	}

	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%s timed out after %s", name, Timeout)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s failed: %v: %s", name, err, util.TruncateString(detail, 500))
	}

	answer := stdout.String()
	if outFile != "" {
		data, err := os.ReadFile(outFile)
		if err != nil {
			return "", fmt.Errorf("reading %s answer: %w", name, err)
		}
		answer = string(data)
	}

	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("%s returned an empty answer", name)
	}
	return answer, nil
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
