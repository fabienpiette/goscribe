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
		AllowedTools: []string{"Bash(gh search:*)", "Read(~/notes/**)", "mcp__linear__get_issue"},
		Model:        "sonnet",
	}
	want := []string{"-p", "--output-format", "text",
		"--tools", "Bash,Read",
		"--allowedTools", "Bash(gh search:*),Read(~/notes/**),mcp__linear__get_issue",
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
	got := researchArgs(ResearchOptions{AllowedTools: []string{"mcp__linear__get_issue"}, UserSettings: true})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--setting-sources user") || strings.Contains(joined, "--mcp-config") {
		t.Errorf("args = %q", joined)
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
	start := time.Now()
	_, err := Research("x", ResearchOptions{Timeout: 100 * time.Millisecond})
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("returned after %s, want well under the fake's 5s sleep", d)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v, want timed out", err)
	}
}
