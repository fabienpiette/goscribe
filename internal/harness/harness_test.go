package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goscribe/pkg/config"
)

// fakeHarness installs shell scripts standing in for claude and codex.
// Both save their args and stdin to $FAKE_DIR so tests can inspect them.
func fakeHarness(t *testing.T, claudeBody, codexBody string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)

	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		script := "#!/bin/sh\necho \"$@\" > \"$FAKE_DIR/" + name + ".args\"\ncat > \"$FAKE_DIR/" + name + ".stdin\"\n" + body + "\n"
		if err := os.WriteFile(path, []byte(script), 0755); err != nil {
			t.Fatalf("writing fake %s: %v", name, err)
		}
		return path
	}

	origClaude, origCodex := ClaudeBinary, CodexBinary
	ClaudeBinary = write("claude", claudeBody)
	CodexBinary = write("codex", codexBody)
	t.Cleanup(func() { ClaudeBinary, CodexBinary = origClaude, origCodex })
	return dir
}

// codexAnswer writes its answer to the --output-last-message file, like codex exec.
const codexAnswer = `while [ $# -gt 0 ]; do [ "$1" = "--output-last-message" ] && out="$2"; shift; done
echo "codex noise on stdout"
printf 'codex summary\n' > "$out"`

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestProcessClaude(t *testing.T) {
	dir := fakeHarness(t, `echo "  claude summary  "`, codexAnswer)
	action := &config.PostAction{ID: "sum", Prompt: "Summarize this meeting."}

	got, err := Process("we decided things", action, "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "claude summary" {
		t.Errorf("answer = %q", got)
	}

	stdin := read(t, filepath.Join(dir, "claude.stdin"))
	if !strings.HasPrefix(stdin, "Summarize this meeting.") || !strings.Contains(stdin, "we decided things") {
		t.Errorf("prompt = %q, want action prompt and transcript", stdin)
	}
	args := read(t, filepath.Join(dir, "claude.args"))
	for _, want := range []string{"-p", "--tools", "--no-session-persistence", "--strict-mcp-config"} {
		if !strings.Contains(args, want) {
			t.Errorf("claude args %q missing %q", args, want)
		}
	}
}

func TestProcessCodexReadsLastMessage(t *testing.T) {
	dir := fakeHarness(t, `echo unused`, codexAnswer)

	got, err := Process("transcript", &config.PostAction{Prompt: "p"}, "codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "codex summary" {
		t.Errorf("answer = %q, want the last message, not stdout", got)
	}
	args := read(t, filepath.Join(dir, "codex.args"))
	for _, want := range []string{"exec", "--sandbox read-only", "--ephemeral", "--skip-git-repo-check"} {
		if !strings.Contains(args, want) {
			t.Errorf("codex args %q missing %q", args, want)
		}
	}
}

func TestRunFailureIncludesStderr(t *testing.T) {
	fakeHarness(t, `echo "not logged in" >&2; exit 1`, codexAnswer)

	_, err := Run("claude", "hi")
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error = %v, want stderr detail", err)
	}
}

func TestRunEmptyAnswer(t *testing.T) {
	fakeHarness(t, `true`, codexAnswer)

	if _, err := Run("claude", "hi"); err == nil {
		t.Error("expected error for empty answer")
	}
}

func TestRunUnknownHarness(t *testing.T) {
	if _, err := Run("gemini", "hi"); err == nil {
		t.Error("expected error for unknown harness")
	}
}

func TestSelectBestActions(t *testing.T) {
	fakeHarness(t, "echo '\"sum, nope, items\"'", codexAnswer)
	actions := []config.PostAction{{ID: "sum"}, {ID: "items"}}

	got, err := SelectBestActions("transcript", actions, "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(got, ",") != "sum,items" {
		t.Errorf("selected = %v, want [sum items]", got)
	}
}
