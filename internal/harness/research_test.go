package harness

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResearchArgs(t *testing.T) {
	o := ResearchOptions{
		AllowedTools: []string{"Bash(gh search:*)", "Read(~/notes/**)", "mcp__tracker__get_issue"},
		Model:        "sonnet",
	}
	want := []string{"-p", "--output-format", "json",
		"--tools", "Bash,Read",
		"--allowedTools", "Bash(gh search:*),Read(~/notes/**),mcp__tracker__get_issue",
		"--disallowedTools", "WebFetch,WebSearch",
		"--permission-mode", "dontAsk",
		"--no-session-persistence", "--disable-slash-commands",
		"--setting-sources", "",
		"--model", "sonnet",
	}
	if got := researchArgs(o); !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q\nwant %q", got, want)
	}

	o.MCPConfig = "/x.json"
	got := researchArgs(o)
	tail := []string{"--mcp-config", "/x.json", "--strict-mcp-config"}
	if !reflect.DeepEqual(got[len(got)-3:], tail) {
		t.Errorf("mcp flags = %q, want %q", got[len(got)-3:], tail)
	}
	if !reflect.DeepEqual(got[:len(want)], want) {
		t.Errorf("base args changed with MCPConfig: %q", got)
	}
	for _, a := range want {
		if a == "--mcp-config" || a == "--strict-mcp-config" {
			t.Errorf("mcp flag %q present without MCPConfig", a)
		}
	}
}

func TestResearchArgsUserSettings(t *testing.T) {
	got := researchArgs(ResearchOptions{AllowedTools: []string{"mcp__tracker__get_issue"}, UserSettings: true})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--setting-sources user") || strings.Contains(joined, "--mcp-config") {
		t.Errorf("args = %q", joined)
	}

	// Both sources add up: the user's connectors stay with the config's servers.
	got = researchArgs(ResearchOptions{AllowedTools: []string{"mcp__tracker__get_issue"}, UserSettings: true, MCPConfig: "/x.json"})
	joined = strings.Join(got, " ")
	if !strings.HasSuffix(joined, "--setting-sources user --mcp-config /x.json") || strings.Contains(joined, "--strict-mcp-config") {
		t.Errorf("args with MCPConfig = %q", joined)
	}
}

func TestResearchArgsOnlyMCPTools(t *testing.T) {
	got := researchArgs(ResearchOptions{AllowedTools: []string{"mcp__a__b"}})
	for i, a := range got {
		if a == "--tools" {
			if got[i+1] != "" {
				t.Errorf("--tools = %q, want empty", got[i+1])
			}
			return
		}
	}
	t.Error("--tools missing")
}

func TestResearchRunsFromTempDirWithStdin(t *testing.T) {
	dir := fakeHarness(t, `pwd > "$FAKE_DIR/pwd"
echo '{"type":"result","is_error":false,"result":"{\\"found\\":false}"}'`, "")
	reply, err := Research("find the thing", ResearchOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Answer != `{"found":false}` || len(reply.Refused) != 0 {
		t.Errorf("reply = %+v", reply)
	}
	pwd := strings.TrimSpace(read(t, dir+"/pwd"))
	if !strings.HasPrefix(pwd, os.TempDir()) {
		t.Errorf("pwd %q not under %q", pwd, os.TempDir())
	}
	if _, err := os.Stat(pwd); !os.IsNotExist(err) {
		t.Errorf("work dir %q still exists", pwd)
	}
	if got := read(t, dir+"/claude.stdin"); got != "find the thing" {
		t.Errorf("stdin = %q", got)
	}
}

func TestResearchTimeout(t *testing.T) {
	fakeHarness(t, `sleep 5`, "")
	start := time.Now()
	_, err := Research("x", ResearchOptions{Timeout: 100 * time.Millisecond})
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("returned after %s, want well under the fake's 5s sleep", d)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want timed out", err)
	}
}

// Errors carry only the cause: the caller adds the "librarian failed" label.
func TestResearchErrorHasNoLabel(t *testing.T) {
	fakeHarness(t, `echo "not logged in" >&2; exit 1`, "")
	_, err := Research("x", ResearchOptions{Timeout: 10 * time.Second})
	if err == nil || err.Error() != "exit status 1: not logged in" {
		t.Errorf("err = %v, want %q", err, "exit status 1: not logged in")
	}
	fakeHarness(t, `true`, "")
	_, err = Research("x", ResearchOptions{Timeout: 10 * time.Second})
	if err == nil || err.Error() != "returned an empty answer" {
		t.Errorf("err = %v", err)
	}
}

// Claude Code reports a failed session as JSON with is_error; in text mode
// that was only "Execution error", hiding why.
func TestResearchReportsSessionError(t *testing.T) {
	fakeHarness(t, `echo '{"type":"result","subtype":"error_during_execution","is_error":true,"terminal_reason":"api_error","result":"Execution error"}'`, "")
	_, err := Research("x", ResearchOptions{Timeout: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "api_error") || !strings.Contains(err.Error(), "Execution error") {
		t.Errorf("err = %v, want the terminal reason and the message", err)
	}
	fakeHarness(t, `echo 'Execution error'`, "")
	_, err = Research("x", ResearchOptions{Timeout: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "Execution error") {
		t.Errorf("err = %v, want the unexpected output", err)
	}
	fakeHarness(t, `echo '{"type":"result","is_error":false,"result":"  "}'`, "")
	_, err = Research("x", ResearchOptions{Timeout: 10 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "empty answer") {
		t.Errorf("err = %v, want empty answer", err)
	}
}

// Tool calls refused by the allowlist come back, so the user can fix the config.
func TestResearchReturnsRefusedCalls(t *testing.T) {
	fakeHarness(t, `cat <<'JSON'
{"type":"result","is_error":false,"result":"ok","permission_denials":[
 {"tool_name":"Bash","tool_input":{"command":"tracker list | head -5"}},
 {"tool_name":"Read","tool_input":{"file_path":"/etc/hosts"}},
 {"tool_name":"mcp__chat__search_users","tool_input":{"query":"Sam"}}]}
JSON`, "")
	reply, err := Research("x", ResearchOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Bash(tracker list | head -5)", "Read(/etc/hosts)", "mcp__chat__search_users"}
	if reply.Answer != "ok" || !reflect.DeepEqual(reply.Refused, want) {
		t.Errorf("reply = %+v, want refused %q", reply, want)
	}
}
