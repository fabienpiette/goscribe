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
	for _, want := range []string{"exec", "--sandbox read-only", "--ephemeral", "--skip-git-repo-check",
		"--ignore-user-config", "--disable shell_tool", "--disable unified_exec", "--disable computer_use"} {
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

// A CLI that prints a banner before its error (Codex does) must not lose the
// error to truncation: errors keep the end of stderr.
func TestRunErrorKeepsTheEndOfStderr(t *testing.T) {
	banner := strings.Repeat("banner line with workdir, model and sandbox settings\n", 40)
	fakeHarness(t, "", `printf '%s' "`+banner+`" >&2; echo "ERROR: stream disconnected before completion" >&2; exit 1`)
	_, err := Run("codex", "x")
	if err == nil || !strings.Contains(err.Error(), "stream disconnected before completion") {
		t.Errorf("err = %v, want the last stderr line", err)
	}
	if err != nil && len(err.Error()) > 400 {
		t.Errorf("err is %d bytes, want the end of stderr only", len(err.Error()))
	}
}

// A model set in Models is passed to the CLI; none means the CLI's default.
func TestRunPassesModel(t *testing.T) {
	dir := fakeHarness(t, `echo claude answer`, codexAnswer)
	t.Cleanup(func() { Models = map[string]string{} })

	if _, err := Run("claude", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run("codex", "x"); err != nil {
		t.Fatal(err)
	}
	if a := read(t, dir+"/claude.args"); strings.Contains(a, "--model") {
		t.Errorf("claude args without a model = %q", a)
	}
	if a := read(t, dir+"/codex.args"); strings.Contains(a, " -m ") {
		t.Errorf("codex args without a model = %q", a)
	}

	Models = map[string]string{"claude": "sonnet", "codex": "gpt-x"}
	if _, err := Run("claude", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run("codex", "x"); err != nil {
		t.Fatal(err)
	}
	if a := read(t, dir+"/claude.args"); !strings.Contains(a, "--model sonnet") {
		t.Errorf("claude args = %q, want --model sonnet", a)
	}
	if a := read(t, dir+"/codex.args"); !strings.Contains(a, "-m gpt-x -") {
		t.Errorf("codex args = %q, want -m gpt-x before the stdin dash", a)
	}
}

// fakeHermes installs a stand-in for the hermes CLI that saves its args and
// stdin, then runs body.
func fakeHermes(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	path := filepath.Join(dir, "hermes")
	script := "#!/bin/sh\necho \"$@\" > \"$FAKE_DIR/hermes.args\"\ncat > \"$FAKE_DIR/hermes.stdin\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	orig := HermesBinary
	HermesBinary = path
	t.Cleanup(func() { HermesBinary = orig })
	return dir
}

const hermesStream = `echo "Warning: Unknown toolsets: none"
echo '{"type": "system", "subtype": "init", "model": "m1", "session_id": "s"}'
echo '{"type": "text", "text": "hermes "}'
echo '{"type": "result", "session_id": "s", "exit_code": 0, "text": "hermes summary"}'`

func TestRunHermes(t *testing.T) {
	dir := fakeHermes(t, hermesStream)
	t.Cleanup(func() { Models = map[string]string{} })
	got, err := Run("hermes", "the prompt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hermes summary" {
		t.Errorf("answer = %q", got)
	}
	args := read(t, dir+"/hermes.args")
	if !strings.Contains(args, "chat --query-file - --oneshot -t none --ignore-rules --source tool --format stream-json") || strings.Contains(args, " -m ") {
		t.Errorf("args = %q", args)
	}
	if read(t, dir+"/hermes.stdin") != "the prompt" {
		t.Error("the prompt must go on stdin")
	}
	Models = map[string]string{"hermes": "anthropic/x"}
	if _, err := Run("hermes", "p"); err != nil {
		t.Fatal(err)
	}
	if args := read(t, dir+"/hermes.args"); !strings.HasSuffix(strings.TrimSpace(args), "-m anthropic/x") {
		t.Errorf("args with a model = %q", args)
	}
}

// The transcript is untrusted: an answer produced with a tool is discarded.
func TestRunHermesFailsClosed(t *testing.T) {
	fakeHermes(t, `echo '{"type": "tool_use", "name": "terminal", "input": {"command": "cat ~/.ssh/id_rsa"}}'
echo '{"type": "result", "exit_code": 0, "text": "done"}'`)
	if _, err := Run("hermes", "p"); err == nil || !strings.Contains(err.Error(), "used a tool (terminal)") {
		t.Errorf("err = %v, want the tool named", err)
	}
	fakeHermes(t, `echo '{"type": "result", "exit_code": 1, "text": "rate limited"}'`)
	if _, err := Run("hermes", "p"); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v, want the failed result", err)
	}
	fakeHermes(t, `echo '{"type": "text", "text": "half"}'`)
	if _, err := Run("hermes", "p"); err == nil || !strings.Contains(err.Error(), "no result") {
		t.Errorf("err = %v, want no result", err)
	}
	fakeHermes(t, `echo '{"type": "result", "exit_code": 0, "text": "  "}'`)
	if _, err := Run("hermes", "p"); err == nil || !strings.Contains(err.Error(), "empty answer") {
		t.Errorf("err = %v, want empty answer", err)
	}
}
