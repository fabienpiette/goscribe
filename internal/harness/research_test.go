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
		AllowedTools: []string{"Bash(gh search:*)", "Read", "mcp__linear__get_issue"},
		Model:        "sonnet",
	}
	want := []string{"-p", "--output-format", "text",
		"--tools", "Bash,Read",
		"--allowedTools", "Bash(gh search:*),Read,mcp__linear__get_issue",
		"--disallowedTools", "WebFetch,WebSearch",
		"--permission-mode", "dontAsk",
		"--no-session-persistence", "--disable-slash-commands",
		"--model", "sonnet",
	}
	if got := researchArgs(o); !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q\nwant %q", got, want)
	}

	o.MCPConfig = "/x.json"
	got := researchArgs(o)
	tail := []string{"--mcp-config", "/x.json", "--strict-mcp-config", "--setting-sources", ""}
	if !reflect.DeepEqual(got[len(got)-5:], tail) {
		t.Errorf("mcp flags = %q, want %q", got[len(got)-5:], tail)
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
echo '{"found":false}'`, "")
	answer, err := Research("find the thing", ResearchOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if answer != `{"found":false}` {
		t.Errorf("answer = %q", answer)
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
	_, err := Research("x", ResearchOptions{Timeout: 100 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want timed out", err)
	}
}
