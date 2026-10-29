//go:build unix

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"goscribe/internal/assistant"
	"goscribe/internal/harness"
	"goscribe/internal/realtime"
	"goscribe/internal/recorder"
	"goscribe/internal/tui"
	"goscribe/pkg/config"
)

func assist(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runAssist(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestAssistName(t *testing.T) {
	if got := assistName(time.Date(2026, 10, 15, 9, 5, 0, 0, time.Local)); got != "assist-2026-10-15_09-05-00.md" {
		t.Errorf("assistName = %q", got)
	}
}

func TestAssistUsageErrors(t *testing.T) {
	fakeAudio(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "el-key")
	fakeClaude(t, `{"summary":"s"}`)
	dir := t.TempDir()
	existing := filepath.Join(dir, "notes.md")
	mustWrite(t, existing, []byte("x"), 0644)

	for name, c := range map[string]struct {
		args []string
		env  string
		want string
	}{
		"exists":        {[]string{"-provider", "claude", "-o", existing}, "el-key", "already exists"},
		"no folder":     {[]string{"-provider", "claude", "-o", filepath.Join(dir, "nope", "n.md")}, "el-key", "folder does not exist"},
		"no el key":     {[]string{"-provider", "claude"}, "", "ElevenLabs API key is required"},
		"bad provider":  {[]string{"-provider", "mistral"}, "el-key", "invalid provider"},
		"no openai key": {[]string{"-provider", "openai"}, "el-key", "needs an OpenAI key"},
		"bad context":   {[]string{"-provider", "claude", "-context", filepath.Join(dir, "missing.md")}, "el-key", "context:"},
		"bad interval":  {[]string{"-provider", "claude", "-interval", "-5s"}, "el-key", "-interval must be positive"},
		"bad source":    {[]string{"-provider", "claude", "-source", "radio"}, "el-key", "source"},
		"extra arg":     {[]string{"meeting.mp3"}, "el-key", "unexpected argument"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ELEVENLABS_API_KEY", c.env)
			_, stderr, code := assist(t, c.args...)
			if code != 2 || !strings.Contains(stderr, c.want) {
				t.Errorf("exit = %d, stderr = %q, want %q", code, stderr, c.want)
			}
		})
	}
}

func TestAssistListSources(t *testing.T) {
	fakeAudio(t)
	stdout, _, code := assist(t, "-list-sources")
	if code != 0 || !strings.Contains(stdout, "* usb-mic") {
		t.Errorf("exit = %d, stdout:\n%s", code, stdout)
	}
}

// syncBuf is safe to read while runAssist writes.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// End to end: fake pactl/ffmpeg capture both sides, a fake ElevenLabs server
// transcribes each session, a fake claude answers, and Ctrl+C ends the
// meeting with the final notes.
func TestAssistEndToEnd(t *testing.T) {
	fakeAudio(t)
	guardSignals(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "el-key")

	// A live device: some PCM, then waiting until killed.
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	mustWrite(t, ffmpeg, []byte("#!/bin/sh\nhead -c 12800 /dev/zero\nsleep 30\n"), 0755)
	recorder.Binary = ffmpeg

	var mu sync.Mutex
	var keys, terms []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		mu.Lock()
		keys = append(keys, r.Header.Get("xi-api-key"))
		terms = r.URL.Query()["keyterms"]
		n := len(keys)
		mu.Unlock()
		ctx := r.Context()
		said := []string{
			"we ship the migration on monday unless the rollback plan is not ready by friday",
			"I will write the rollback runbook today and share it with the whole team tonight",
		}[(n-1)%2]
		var msg map[string]any
		if wsjson.Read(ctx, c, &msg) != nil {
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{"message_type": "committed_transcript", "text": said})
		for wsjson.Read(ctx, c, &msg) == nil {
		}
	}))
	defer srv.Close()
	origURL, origFlush := realtime.BaseURL, realtime.FlushTimeout
	realtime.BaseURL, realtime.FlushTimeout = "ws"+strings.TrimPrefix(srv.URL, "http"), 200*time.Millisecond
	defer func() { realtime.BaseURL, realtime.FlushTimeout = origURL, origFlush }()
	origTick := assistant.Tick
	assistant.Tick = 10 * time.Millisecond
	defer func() { assistant.Tick = origTick }()

	t.Cleanup(func() { harness.Models = map[string]string{} })
	claudePrompt := fakeClaude(t, `{"summary":"Shipping Monday; runbook by tonight.","decisions":["Ship on Monday"],"open_questions":[],"action_items":["me: write the rollback runbook"],"suggestions":[{"type":"question","text":"Who reviews the runbook?","why":"nobody named"}]}`)

	notes := filepath.Join(t.TempDir(), "standup.md")
	ctxFile := filepath.Join(t.TempDir(), "agenda.md")
	mustWrite(t, ctxFile, []byte("# Agenda\nMigration go/no-go.\nkeyterms: Acme, Globex, Acme rollback runbook review"), 0644)

	out := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		done <- runAssist([]string{"-provider", "claude", "-model", "opus-x", "-context", ctxFile, "-o", notes, "-interval", "20ms"}, out, &syncBuf{})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "Ask: Who reviews the runbook?") {
		if time.Now().After(deadline) {
			t.Fatalf("no suggestion in time; output:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("assist did not stop after Ctrl+C:\n%s", out.String())
	}

	feed := out.String()
	for _, want := range []string{
		"Assist me+others · claude (opus-x), updates every ≥20ms · context: 1 file(s), 1 KB · 2 keyterm(s) from the context → " + notes,
		"we ship the migration on monday", "I will write the rollback runbook",
		"✅ ", "Decision: Ship on Monday",
		"✓ Final notes written to " + notes,
	} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%s", want, feed)
		}
	}
	// Both sides connect at once, so which one gets which sentence varies;
	// each speaker must still get exactly one line.
	if strings.Count(feed, ":00 me     ") != 1 || strings.Count(feed, ":00 others ") != 1 {
		t.Errorf("one line per speaker expected:\n%s", feed)
	}
	if args, _ := os.ReadFile(claudePrompt + ".args"); !strings.Contains(string(args), "--model opus-x") {
		t.Errorf("-model must reach claude: args = %q", args)
	}
	b, _ := os.ReadFile(notes)
	for _, want := range []string{"## Final summary", "- me: write the rollback runbook", "❓ Ask: Who reviews the runbook?"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("notes missing %q:\n%s", want, b)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] != "el-key" {
		t.Errorf("one realtime session per side with the key; got %q", keys)
	}
	if !strings.Contains(feed, "⚠ 1 keyterm(s) over 20 characters left out (live speech-to-text refuses them): Acme rollback runbook review") {
		t.Errorf("feed must warn about the long keyterm:\n%s", feed)
	}
	if strings.Join(terms, ",") != "Acme,Globex" {
		t.Errorf("keyterms from the context = %q", terms)
	}
}

func mustWrite(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, perm); err != nil {
		t.Fatal(err)
	}
}

func TestAssistLibrarianUsageErrors(t *testing.T) {
	fakeAudio(t)
	fakeClaude(t, `{"summary":"s"}`)
	dir := t.TempDir()
	none := configWith(t, dir, "none.yml", "")
	web := configWith(t, dir, "web.yml", "librarian:\n  allowed_tools: [WebFetch]\n")
	for name, tc := range map[string]struct{ cfg, want string }{
		"no config": {none, "librarian.allowed_tools"},
		"web fetch": {web, "WebFetch"},
	} {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := assist(t, "-provider", "claude", "-librarian", "-elevenlabs-key", "k", "-config", tc.cfg,
				"-o", filepath.Join(dir, name+".md"))
			if code != 2 || !strings.Contains(errOut, tc.want) {
				t.Errorf("code = %d, stderr = %q; want 2 and %q", code, errOut, tc.want)
			}
		})
	}
}

// The librarian runs as a second claude call with its own tool list; the
// answer shows in the feed and the notes.
func TestAssistLibrarianEndToEnd(t *testing.T) {
	args := librarianRun(t, "")
	for _, want := range []string{"[--disallowedTools][WebFetch,WebSearch]", "[--setting-sources][]"} {
		if !strings.Contains(args, want) {
			t.Errorf("librarian args missing %q: %s", want, args)
		}
	}
	if strings.Contains(args, "--mcp-config") {
		t.Errorf("unexpected --mcp-config: %s", args)
	}
}

// Without -librarian, allowed_tools in the config is enough.
func TestAssistLibrarianDefaultOn(t *testing.T) {
	args := librarianRunFlag(t, "", "")
	if !strings.Contains(args, "[--allowedTools]") {
		t.Errorf("librarian args = %s", args)
	}
}

// A tool the check refuses only warns when -librarian was not passed, and
// assist still starts.
func TestAssistLibrarianDefaultWarns(t *testing.T) {
	fakeAudio(t)
	guardSignals(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "el-key")
	fakeClaude(t, `{"summary":"s"}`)
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	mustWrite(t, ffmpeg, []byte("#!/bin/sh\nhead -c 12800 /dev/zero\nsleep 30\n"), 0755)
	recorder.Binary = ffmpeg
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		var msg map[string]any
		for wsjson.Read(r.Context(), c, &msg) == nil {
		}
	}))
	defer srv.Close()
	origURL, origFlush := realtime.BaseURL, realtime.FlushTimeout
	realtime.BaseURL, realtime.FlushTimeout = "ws"+strings.TrimPrefix(srv.URL, "http"), 200*time.Millisecond
	defer func() { realtime.BaseURL, realtime.FlushTimeout = origURL, origFlush }()

	dir := t.TempDir()
	cfgFile := configWith(t, dir, "config.yml", "librarian:\n  allowed_tools: [WebFetch]\n")
	out := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		done <- runAssist([]string{"-provider", "claude", "-config", cfgFile, "-o", filepath.Join(dir, "notes.md"), "-interval", "20ms"}, out, &syncBuf{})
	}()
	deadline := time.Now().Add(8 * time.Second)
	for !strings.Contains(out.String(), "⚠ librarian off:") {
		select {
		case code := <-done:
			t.Fatalf("assist exited with %d before the warning:\n%s", code, out.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("no warning in time; output:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("assist did not stop:\n%s", out.String())
	}
	feed := out.String()
	if !strings.Contains(feed, "web tools can exfiltrate data") || strings.Contains(feed, "librarian: claude") {
		t.Errorf("feed:\n%s", feed)
	}
}

func TestResolveLibrarian(t *testing.T) {
	no, yes := false, true
	tools := []string{"mcp__tracker__get_issue"}
	pass := func(l config.Librarian) (config.Librarian, error) { return l, nil }
	failWith := func(msg string) func(config.Librarian) (config.Librarian, error) {
		return func(l config.Librarian) (config.Librarian, error) { return l, errors.New(msg) }
	}
	validate := func(l config.Librarian) (config.Librarian, error) { return l, l.Validate() }
	for _, tc := range []struct {
		name             string
		explicit, flagOn bool
		tools            []string
		enabled          *bool
		check            func(config.Librarian) (config.Librarian, error)
		on               bool
		off, err         string
		warn             bool
	}{
		{"default, tools, ok", false, false, tools, nil, pass, true, "", "", false},
		{"default, no tools", false, false, nil, nil, pass, false, "librarian.allowed_tools", "", false},
		{"default, refused tool", false, false, tools, nil, failWith("entry 'X' is refused"), false, "entry 'X' is refused", "", true},
		{"default, no claude CLI", false, false, tools, nil, failWith("needs the claude CLI"), false, "needs the claude CLI", "", true},
		{"config enabled: false", false, false, tools, &no, pass, false, "librarian.enabled: false", "", false},
		{"config enabled: true", false, false, tools, &yes, pass, true, "", "", false},
		{"-librarian=false", true, false, tools, nil, pass, false, "-librarian=false", "", false},
		{"-librarian, no tools", true, true, nil, nil, validate, false, "", "librarian.allowed_tools is empty", false},
		{"-librarian, refused tool", true, true, tools, nil, failWith("refused"), false, "", "refused", false},
		{"-librarian beats enabled: false", true, true, tools, &no, pass, true, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, on, off, warn, err := resolveLibrarian(tc.explicit, tc.flagOn, config.Librarian{AllowedTools: tc.tools, Enabled: tc.enabled}, tc.check)
			if on != tc.on || !strings.Contains(off, tc.off) || (tc.off == "" && off != "") {
				t.Errorf("on = %v, off = %q; want %v, %q", on, off, tc.on, tc.off)
			}
			if warn != tc.warn {
				t.Errorf("warn = %v, want %v", warn, tc.warn)
			}
			if tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Errorf("err = %v, want %q", err, tc.err)
			}
		})
	}
}

func TestAssistLibrarianMCPConfigArgs(t *testing.T) {
	mcp := filepath.Join(t.TempDir(), "mcp.json")
	mustWrite(t, mcp, []byte(`{"mcpServers":{}}`), 0644)
	args := librarianRun(t, "  mcp_config: "+mcp+"\n")
	for _, want := range []string{"[--mcp-config][" + mcp + "]", "[--strict-mcp-config]", "[--setting-sources][]"} {
		if !strings.Contains(args, want) {
			t.Errorf("librarian args missing %q: %s", want, args)
		}
	}
}

func TestAssistLibrarianUserSettingsArgs(t *testing.T) {
	claudeSettingsPath = func() (string, error) { return filepath.Join(t.TempDir(), "missing.json"), nil }
	t.Cleanup(func() { claudeSettingsPath = origSettingsPath })
	args := librarianRun(t, "  use_claude_settings: true\n")
	if !strings.Contains(args, "[--setting-sources][user]") {
		t.Errorf("librarian args = %s", args)
	}
}

func TestAssistLibrarianSettingsAndPathErrors(t *testing.T) {
	fakeAudio(t)
	fakeClaude(t, `{"summary":"s"}`)
	dir := t.TempDir()
	cfg := configWith(t, dir, "c.yml", "librarian:\n  allowed_tools: [mcp__tracker__get_issue]\n  use_claude_settings: true\n")
	t.Cleanup(func() { claudeSettingsPath = origSettingsPath })
	for name, tc := range map[string]struct{ settings, want string }{
		"allow rules": {`{"permissions":{"allow":["Bash(rm:*)"]}}`, "permissions.allow"},
		"invalid":     {`{nope`, "not valid JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			sp := filepath.Join(dir, name+".json")
			mustWrite(t, sp, []byte(tc.settings), 0644)
			claudeSettingsPath = func() (string, error) { return sp, nil }
			_, errOut, code := assist(t, "-provider", "claude", "-librarian", "-elevenlabs-key", "k", "-config", cfg, "-o", filepath.Join(dir, name+".md"))
			if code != 2 || !strings.Contains(errOut, tc.want) {
				t.Errorf("code = %d, stderr = %q; want 2 and %q", code, errOut, tc.want)
			}
		})
	}
	// Settings without allow rules are fine: it gets as far as the claude check.
	sp := filepath.Join(dir, "ok.json")
	mustWrite(t, sp, []byte(`{"permissions":{"allow":[]},"model":"x"}`), 0644)
	claudeSettingsPath = func() (string, error) { return sp, nil }
	harness.ClaudeBinary = "no-such-claude-binary"
	cfg = configWith(t, dir, "ok.yml", "librarian:\n  allowed_tools: [mcp__tracker__get_issue]\n  use_claude_settings: true\n")
	b, _ := os.ReadFile(cfg)
	mustWrite(t, cfg, []byte(strings.Replace(string(b), `openai_api_key: ""`, `openai_api_key: "k"`, 1)), 0644)
	_, errOut, code := assist(t, "-provider", "openai", "-librarian", "-elevenlabs-key", "k", "-config", cfg, "-o", filepath.Join(dir, "ok.md"))
	if code != 2 || strings.Contains(errOut, "permissions.allow") || !strings.Contains(errOut, "librarian: needs") {
		t.Errorf("code = %d, stderr = %q", code, errOut)
	}
}

var origSettingsPath = claudeSettingsPath

func TestResolveMCPConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	mustWrite(t, filepath.Join(home, "mcp.json"), []byte(`{}`), 0644)
	if p, err := resolveMCPConfig("~/mcp.json"); err != nil || p != filepath.Join(home, "mcp.json") {
		t.Errorf("~/mcp.json = %q, %v", p, err)
	}
	t.Chdir(home)
	if p, err := resolveMCPConfig("mcp.json"); err != nil || p != filepath.Join(home, "mcp.json") {
		t.Errorf("mcp.json = %q, %v", p, err)
	}
	if _, err := resolveMCPConfig("~/missing.json"); err == nil || !strings.Contains(err.Error(), "mcp_config") {
		t.Errorf("missing file: err = %v", err)
	}
}

func TestAssistLibrarianMissingMCPConfig(t *testing.T) {
	fakeAudio(t)
	fakeClaude(t, `{"summary":"s"}`)
	dir := t.TempDir()
	cfg := configWith(t, dir, "c.yml", "librarian:\n  allowed_tools: [mcp__tracker__get_issue]\n  mcp_config: ~/no-such-mcp.json\n")
	_, errOut, code := assist(t, "-provider", "claude", "-librarian", "-elevenlabs-key", "k", "-config", cfg, "-o", filepath.Join(dir, "n.md"))
	if code != 2 || !strings.Contains(errOut, "mcp_config") || !strings.Contains(errOut, "no-such-mcp.json") {
		t.Errorf("code = %d, stderr = %q", code, errOut)
	}
}

func TestClaudeSettingsPath(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/srv/claude")
	if p, err := claudeSettingsPath(); err != nil || p != "/srv/claude/settings.json" {
		t.Errorf("path = %q, %v", p, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	if _, err := claudeSettingsPath(); err == nil {
		t.Error("no home and no CLAUDE_CONFIG_DIR: want an error")
	}
}

func TestAssistLibrarianConfigDirSettingsRefused(t *testing.T) {
	fakeAudio(t)
	fakeClaude(t, `{"summary":"s"}`)
	dir := t.TempDir()
	cfg := configWith(t, dir, "c.yml", "librarian:\n  allowed_tools: [mcp__tracker__get_issue]\n  use_claude_settings: true\n")
	cd := t.TempDir()
	mustWrite(t, filepath.Join(cd, "settings.json"), []byte(`{"permissions":{"allow":["Read"]}}`), 0644)
	t.Setenv("CLAUDE_CONFIG_DIR", cd)
	_, errOut, code := assist(t, "-provider", "claude", "-librarian", "-elevenlabs-key", "k", "-config", cfg, "-o", filepath.Join(dir, "n.md"))
	if code != 2 || !strings.Contains(errOut, "permissions.allow") {
		t.Errorf("code = %d, stderr = %q", code, errOut)
	}
}

// librarianRun runs assist with -librarian against fake claude and ElevenLabs
// until the librarian answers, checks the feed and notes, and returns the
// librarian's arguments, one [arg] each.
func librarianRun(t *testing.T, extraCfg string) string {
	t.Helper()
	return librarianRunFlag(t, extraCfg, "-librarian")
}

// librarianRunFlag is librarianRun with the librarian flag given by the caller
// ("" for none, to test the default).
func librarianRunFlag(t *testing.T, extraCfg, libFlag string) string {
	t.Helper()
	fakeAudio(t)
	guardSignals(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "el-key")

	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	mustWrite(t, ffmpeg, []byte("#!/bin/sh\nhead -c 12800 /dev/zero\nsleep 30\n"), 0755)
	recorder.Binary = ffmpeg

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		var msg map[string]any
		if wsjson.Read(ctx, c, &msg) != nil {
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{"message_type": "committed_transcript", "text": "did PROJ-123 ship already or is it still in review for the Acme release"})
		for wsjson.Read(ctx, c, &msg) == nil {
		}
	}))
	defer srv.Close()
	origURL, origFlush := realtime.BaseURL, realtime.FlushTimeout
	realtime.BaseURL, realtime.FlushTimeout = "ws"+strings.TrimPrefix(srv.URL, "http"), 200*time.Millisecond
	defer func() { realtime.BaseURL, realtime.FlushTimeout = origURL, origFlush }()
	origTick := assistant.Tick
	assistant.Tick = 10 * time.Millisecond
	defer func() { assistant.Tick = origTick }()

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "librarian-args")
	t.Setenv("FAKE_ARGS", argsFile)
	bin := filepath.Join(dir, "fake-claude")
	mustWrite(t, bin, []byte(`#!/bin/sh
cat > /dev/null
case "$*" in
*--allowedTools*)
  for a in "$@"; do printf '[%s]' "$a"; done > "$FAKE_ARGS"
  echo '{"type":"result","is_error":false,"result":"{\"found\":true,\"answer\":\"PROJ-123 shipped on Monday\",\"sources\":[{\"title\":\"PROJ-123\",\"link\":\"https://example.com/PROJ-123\"}]}","permission_denials":[{"tool_name":"Bash","tool_input":{"command":"tracker list | head"}}]}'
  ;;
*)
  echo '{"summary":"Checking PROJ-123.","decisions":[],"open_questions":[],"action_items":[],"suggestions":[],"lookups":[{"query":"Did PROJ-123 ship?","why":"status unknown"}]}'
  ;;
esac
`), 0755)
	orig := harness.ClaudeBinary
	harness.ClaudeBinary = bin
	defer func() { harness.ClaudeBinary = orig }()

	cfgFile := configWith(t, dir, "config.yml", "librarian:\n  allowed_tools: [\"Bash(gh pr view:*)\", mcp__tracker__get_issue]\n  timeout: 20s\n"+extraCfg)
	notes := filepath.Join(dir, "notes.md")

	out := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		args := []string{"-provider", "claude", "-config", cfgFile, "-o", notes, "-interval", "20ms"}
		if libFlag != "" {
			args = append(args, libFlag)
		}
		done <- runAssist(args, out, &syncBuf{})
	}()
	deadline := time.Now().Add(8 * time.Second)
	for !strings.Contains(out.String(), "PROJ-123 shipped on Monday") {
		if time.Now().After(deadline) {
			t.Fatalf("no librarian answer in time; output:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d\n%s", code, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("assist did not stop:\n%s", out.String())
	}

	feed := out.String()
	for _, want := range []string{"librarian: claude, 2 tool(s)", "📚 ", "Librarian: PROJ-123 shipped on Monday", "refused 1 tool call(s) not in librarian.allowed_tools: Bash(tracker list | head)"} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%s", want, feed)
		}
	}
	if b, _ := os.ReadFile(notes); !strings.Contains(string(b), "## Research") {
		t.Errorf("notes missing ## Research:\n%s", b)
	}
	const byDefault = "librarian: claude, 2 tool(s) (on by default; -librarian=false turns it off)"
	if got := strings.Contains(feed, byDefault); got != (libFlag == "") {
		t.Errorf("by-default header present = %v with flag %q:\n%s", got, libFlag, feed)
	}
	args, _ := os.ReadFile(argsFile)
	return string(args)
}

// configWith writes the default config plus extra YAML and returns its path.
func configWith(t *testing.T, dir, name, extra string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := config.CreateDefault(); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".goscribe", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	mustWrite(t, p, append(b, []byte("\n"+extra)...), 0644)
	return p
}

// The gate passes audio through, sends silence of the same length while the
// side is muted, and closes its output when the capture ends.
func TestMuteGate(t *testing.T) {
	in, out := make(chan []byte, 4), make(chan []byte, 4)
	var muted atomic.Bool
	go muteGate(in, out, &muted)
	in <- []byte{1, 2, 3}
	if got := <-out; !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Errorf("unmuted chunk = %v", got)
	}
	muted.Store(true)
	chunk := []byte{4, 5, 6}
	in <- chunk
	if got := <-out; !bytes.Equal(got, []byte{0, 0, 0}) {
		t.Errorf("muted chunk = %v, want silence", got)
	}
	if !bytes.Equal(chunk, []byte{4, 5, 6}) {
		t.Error("the gate must not overwrite the captured chunk")
	}
	close(in)
	if _, ok := <-out; ok {
		t.Error("out must close when the capture ends")
	}
}

func TestAssistMuteHint(t *testing.T) {
	for _, c := range []struct {
		sides []assistSide
		want  string
	}{
		{[]assistSide{{"me", "mic0"}, {"others", "mon0"}}, "m/s"},
		{[]assistSide{{"me", "mic0"}}, "m"},
		{[]assistSide{{"others", "mon0"}}, "s"},
	} {
		if got := assistMuteHint(c.sides); got != c.want {
			t.Errorf("assistMuteHint(%v) = %q, want %q", c.sides, got, c.want)
		}
	}
	if sideForKey("mic") != "me" || sideForKey("system") != "others" || sideForKey("x") != "" {
		t.Error("mute key labels must map to the assist speakers")
	}
}

// End to end: pressing the mute key sends silence to the live session (which
// stays open) and back to real audio on the next press, with mute events.
func TestStreamSidesMute(t *testing.T) {
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	mustWrite(t, ffmpeg, []byte("#!/bin/sh\nwhile :; do yes | head -c 6400; sleep 0.05; done\n"), 0755)
	origBin := recorder.Binary
	recorder.Binary = ffmpeg
	defer func() { recorder.Binary = origBin }()

	var mu sync.Mutex
	var kinds []string // "audio" or "silence", per chunk received
	sessions := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		mu.Lock()
		sessions++
		mu.Unlock()
		for {
			var msg struct {
				Audio string `json:"audio_base_64"`
			}
			if wsjson.Read(r.Context(), c, &msg) != nil {
				return
			}
			pcm, _ := base64.StdEncoding.DecodeString(msg.Audio)
			kind := "silence"
			for _, b := range pcm {
				if b != 0 {
					kind = "audio"
					break
				}
			}
			mu.Lock()
			kinds = append(kinds, kind)
			mu.Unlock()
		}
	}))
	defer srv.Close()
	origURL, origFlush := realtime.BaseURL, realtime.FlushTimeout
	realtime.BaseURL, realtime.FlushTimeout = "ws"+strings.TrimPrefix(srv.URL, "http"), 100*time.Millisecond
	defer func() { realtime.BaseURL, realtime.FlushTimeout = origURL, origFlush }()

	last := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(kinds) == 0 {
			return ""
		}
		return kinds[len(kinds)-1]
	}
	waitFor := func(what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for last() != what {
			if time.Now().After(deadline) {
				t.Fatalf("no %s chunk reached the session", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	toggle := make(chan string)
	events := streamSides(ctx, make(chan struct{}), []assistSide{{"me", "mic0"}}, realtime.Options{APIKey: "k"}, realtime.LocalOptions{}, toggle)
	var got []assistant.Event
	collected := make(chan struct{})
	go func() {
		for e := range events {
			got = append(got, e)
		}
		close(collected)
	}()

	waitFor("audio")
	toggle <- "mic"
	waitFor("silence")
	toggle <- "system" // not captured here: ignored
	toggle <- "mic"
	waitFor("audio")
	toggle <- "recap"
	cancel()
	<-collected
	recaps := 0
	for _, e := range got {
		if e.Kind == assistant.RecapEvent {
			recaps++
		}
	}
	if recaps != 1 {
		t.Errorf("recap events = %d, want 1", recaps)
	}

	var mutes []string
	for _, e := range got {
		if e.Kind == assistant.MuteEvent {
			mutes = append(mutes, fmt.Sprintf("%s:%v", e.Speaker, e.Muted))
		}
	}
	if strings.Join(mutes, ",") != "me:true,me:false" {
		t.Errorf("mute events = %v", mutes)
	}
	mu.Lock()
	defer mu.Unlock()
	if sessions != 1 {
		t.Errorf("sessions = %d, want one kept open across mute and unmute", sessions)
	}
}

func TestAssistRecapFlag(t *testing.T) {
	fakeClaude(t, "") // CI has no claude CLI; -provider claude must get past its check
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ELEVENLABS_API_KEY", "k")
	stderr := &syncBuf{}
	if code := runAssist([]string{"-provider", "claude", "-recap", "0s"}, &syncBuf{}, stderr); code != 2 || !strings.Contains(stderr.String(), "-recap must be positive") {
		t.Errorf("exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestAssistHermesNeedsCLI(t *testing.T) {
	orig := harness.HermesBinary
	harness.HermesBinary = "goscribe-no-such-hermes"
	t.Cleanup(func() { harness.HermesBinary = orig })
	t.Setenv("HOME", t.TempDir())
	stderr := &syncBuf{}
	if code := runAssist([]string{"-provider", "hermes", "-elevenlabs-key", "k"}, &syncBuf{}, stderr); code != 2 || !strings.Contains(stderr.String(), "-provider hermes needs the goscribe-no-such-hermes CLI on PATH") {
		t.Errorf("exit = %d, stderr = %q", code, stderr.String())
	}
}

// When ElevenLabs is out of quota, a side switches to local transcription.
func TestStreamSidesQuotaFallsBackToLocal(t *testing.T) {
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	mustWrite(t, ffmpeg, []byte("#!/bin/sh\nwhile :; do yes | head -c 6400; sleep 0.05; done\n"), 0755)
	origBin := recorder.Binary
	recorder.Binary = ffmpeg
	defer func() { recorder.Binary = origBin }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		var msg map[string]any
		if wsjson.Read(r.Context(), c, &msg) != nil {
			return
		}
		_ = wsjson.Write(r.Context(), c, map[string]any{"message_type": "quota_exceeded", "error": "You have exceeded your quota."})
		for wsjson.Read(r.Context(), c, &msg) == nil {
		}
	}))
	defer srv.Close()
	origURL := realtime.BaseURL
	realtime.BaseURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	origMin, origMax, origVoiced := realtime.MinLocalSegment, realtime.MaxLocalSegment, realtime.MinVoiced
	realtime.MinLocalSegment, realtime.MaxLocalSegment, realtime.MinVoiced = 200*time.Millisecond, 400*time.Millisecond, 100*time.Millisecond
	defer func() {
		realtime.BaseURL = origURL
		realtime.MinLocalSegment, realtime.MaxLocalSegment, realtime.MinVoiced = origMin, origMax, origVoiced
	}()

	local := realtime.LocalOptions{Transcribe: func(string) (string, error) { return "local words", nil }}
	ctx, cancel := context.WithCancel(context.Background())
	events := streamSides(ctx, make(chan struct{}), []assistSide{{"me", "mic0"}}, realtime.Options{APIKey: "k"}, local, nil)
	var mu sync.Mutex
	var got []assistant.Event
	collected := make(chan struct{})
	go func() {
		for e := range events {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}
		close(collected)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		lines := 0
		for _, e := range got {
			if e.Kind == assistant.LineEvent && e.Text == "local words" {
				lines++
			}
		}
		mu.Unlock()
		if lines > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no local line; events: %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-collected
	notice := false
	for _, e := range got {
		if e.Kind == assistant.NoticeEvent && strings.Contains(e.Text, "me: ElevenLabs is out of quota: switching to local transcription") {
			notice = true
		}
		if e.Kind == assistant.NoticeEvent && strings.Contains(e.Text, "live transcription stopped") {
			t.Errorf("the quota error must not stop the side: %q", e.Text)
		}
	}
	if !notice {
		t.Errorf("no switch notice; events: %+v", got)
	}
}

func TestUseTUI(t *testing.T) {
	if useTUI(true, os.Stdin, os.Stdout) {
		t.Error("-plain must turn the full-screen view off")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if useTUI(false, os.Stdin, w) {
		t.Error("a piped stdout must get the plain output")
	}
}

func TestAssistPlainFlag(t *testing.T) {
	_, stderr, code := assist(t, "-h")
	if code != 0 || !strings.Contains(stderr, "-plain") {
		t.Errorf("exit = %d, -h must list -plain:\n%s", code, stderr)
	}
	t.Setenv("HOME", t.TempDir())
	_, stderr, code = assist(t, "-plain", "-provider", "mistral")
	if code != 2 || !strings.Contains(stderr, "invalid provider") {
		t.Errorf("-plain must be accepted: exit = %d, stderr = %q", code, stderr)
	}
}

func TestStartupNotices(t *testing.T) {
	got := startupNotices("me · fake", "n.md", "n.transcript.md", "Ctrl+C to stop", []string{"Acme rollback runbook review"}, assistant.WarnContextBytes+2048)
	want := []string{
		"Assist me · fake → n.md, transcript n.transcript.md (Ctrl+C to stop)",
		"⚠ 1 keyterm(s) over 20 characters left out (live speech-to-text refuses them): Acme rollback runbook review",
		fmt.Sprintf("⚠ the context is %d KB: every update resends it, which is slower and costlier", (assistant.WarnContextBytes+2048)>>10),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("notices:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := startupNotices("h", "n.md", "t.md", "k", nil, 10); len(got) != 1 {
		t.Errorf("no warnings expected: %q", got)
	}
}

func TestPrintEndSummary(t *testing.T) {
	var b bytes.Buffer
	printEndSummary(&b, assistant.Memory{Summary: "Shipping Monday.", ActionItems: []string{"Sam: write the runbook", "Alex: review it"}}, true, "n.md", "n.transcript.md")
	want := "Shipping Monday.\n\nAction items:\n- Sam: write the runbook\n- Alex: review it\n\nNotes: n.md\nTranscript: n.transcript.md\n"
	if b.String() != want {
		t.Errorf("summary:\n%q\nwant:\n%q", b.String(), want)
	}
	b.Reset()
	printEndSummary(&b, assistant.Memory{}, false, "n.md", "n.transcript.md")
	if want := "Notes: n.md\nTranscript: n.transcript.md\n"; b.String() != want {
		t.Errorf("empty summary:\n%q\nwant:\n%q", b.String(), want)
	}
	b.Reset()
	printEndSummary(&b, assistant.Memory{Summary: "Shipping Monday."}, false, "n.md", "")
	if want := "Live summary (final notes not written):\nShipping Monday.\n\nNotes: n.md\n"; b.String() != want {
		t.Errorf("live summary:\n%q\nwant:\n%q", b.String(), want)
	}
	// Model output reaches the real terminal here: no escape sequences.
	b.Reset()
	printEndSummary(&b, assistant.Memory{Summary: "Ship\x1b]52;c;aGk=\a Monday.\r", ActionItems: []string{"Sam:\x1b[2J write it"}}, true, "n.md", "")
	if strings.ContainsAny(b.String(), "\x1b\a\r") {
		t.Errorf("escape sequences printed: %q", b.String())
	}
}

// fakeProgram stands for the Bubble Tea program: it records the messages and
// runs until Run's end is announced.
type fakeProgram struct {
	model  tea.Model
	mu     sync.Mutex
	msgs   []tea.Msg
	ended  chan struct{}
	once   sync.Once
	onExit func() // called when Run returns
	// panicOn, when set, makes Send panic on the messages it accepts.
	panicOn func(tea.Msg) bool
	// runErr, when set, makes Run fail at once, like a screen that cannot start.
	runErr error
}

func (p *fakeProgram) Send(msg tea.Msg) {
	if p.panicOn != nil && p.panicOn(msg) {
		panic("boom in the view")
	}
	p.mu.Lock()
	p.msgs = append(p.msgs, msg)
	p.mu.Unlock()
	if _, ok := msg.(tui.EndedMsg); ok {
		p.once.Do(func() { close(p.ended) })
	}
}

func (p *fakeProgram) Run() (tea.Model, error) {
	if p.runErr != nil {
		return p.model, p.runErr
	}
	<-p.ended
	if p.onExit != nil {
		p.onExit()
	}
	return p.model, nil
}

func (p *fakeProgram) items() []assistant.Item {
	p.mu.Lock()
	defer p.mu.Unlock()
	var items []assistant.Item
	for _, m := range p.msgs {
		if it, ok := m.(tui.ItemMsg); ok {
			items = append(items, assistant.Item(it))
		}
	}
	return items
}

func newFakeProgram(p *fakeProgram) func(tea.Model) teaProgram {
	return func(m tea.Model) teaProgram {
		p.model, p.ended = m, make(chan struct{})
		return p
	}
}

// Review Focus 1: while the screen is on, the startup lines go to the feed and
// nothing is written to stdout; the end summary comes after.
func TestAssistTUIStartupNoticesGoToFeed(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "n.md")
	var stdout bytes.Buffer
	var atExit string
	p := &fakeProgram{onExit: func() { atExit = stdout.String() }}
	events := make(chan assistant.Event)
	close(events)
	notices := startupNotices("me · fake", notes, assistant.TranscriptPath(notes), "k", []string{"Acme rollback runbook review"}, 0)
	s := tuiSession{
		cfg:       assistant.Config{Notes: notes, Transcript: assistant.TranscriptPath(notes), Engine: "fake", Interval: time.Second, Think: func(_, _ string) (string, error) { return "", nil }},
		notices:   notices,
		sides:     []string{"me"},
		toggle:    make(chan string, 1),
		interrupt: func() {},
		start:     newFakeProgram(p),
	}
	if err := s.run(events, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if atExit != "" {
		t.Errorf("stdout written while the screen was on: %q", atExit)
	}
	items := p.items()
	if len(items) < len(notices) {
		t.Fatalf("items = %+v", items)
	}
	for i, n := range notices {
		if items[i].Kind != assistant.ItemNotice || items[i].Text != n {
			t.Errorf("item %d = %+v, want the notice %q", i, items[i], n)
		}
	}
	if !strings.Contains(stdout.String(), "Notes: "+notes) {
		t.Errorf("end summary missing:\n%s", stdout.String())
	}
}

// Review Focus 2: Ctrl+C twice while the model never answers ends at once and
// leaves the live notes on disk.
func TestTUIInterruptTwiceForces(t *testing.T) {
	origTick := assistant.Tick
	assistant.Tick = 10 * time.Millisecond
	defer func() { assistant.Tick = origTick }()

	ctx, force, interrupt, stop := watchSignalsInterruptible()
	defer stop()
	thinking := make(chan struct{}, 1)
	never := make(chan struct{})
	defer close(never)
	think := func(_, _ string) (string, error) {
		select {
		case thinking <- struct{}{}:
		default:
		}
		<-never
		return "", nil
	}

	events := make(chan assistant.Event, 4)
	events <- assistant.Event{Kind: assistant.LineEvent, Speaker: "me", Text: "we ship the migration on monday unless the rollback plan is not ready by friday"}
	go func() {
		<-ctx.Done()
		close(events)
	}()

	notes := filepath.Join(t.TempDir(), "n.md")
	p := &fakeProgram{}
	s := tuiSession{
		cfg:       assistant.Config{Notes: notes, Transcript: assistant.TranscriptPath(notes), Engine: "fake", Interval: 5 * time.Millisecond, Think: think, Force: force},
		sides:     []string{"me"},
		toggle:    make(chan string, 1),
		interrupt: interrupt,
		start:     newFakeProgram(p),
	}
	done := make(chan error, 1)
	var stdout bytes.Buffer
	go func() { done <- s.run(events, &stdout, io.Discard) }()

	select {
	case <-thinking:
	case <-time.After(5 * time.Second):
		t.Fatal("no update started")
	}
	ctrlC := tea.KeyMsg{Type: tea.KeyCtrlC}
	p.model.Update(ctrlC)
	p.model.Update(ctrlC)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second Ctrl+C did not end the run")
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("live notes missing: %v", err)
	}
}

// A stray print while the screen is on (an API retry message) goes to the feed.
func TestCaptureStdio(t *testing.T) {
	origOut, origErr := os.Stdout, os.Stderr
	var lines []string
	restore := captureStdio(func(line string) { lines = append(lines, line) })
	fmt.Println("  ⚠ Request failed, retrying in 1s")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "stderr too")
	restore()
	restore()
	if os.Stdout != origOut || os.Stderr != origErr {
		t.Fatal("stdout and stderr not restored")
	}
	if strings.Join(lines, "|") != "⚠ Request failed, retrying in 1s|stderr too" {
		t.Errorf("lines = %q", lines)
	}
}

// A panic while Run shows the meeting must end the screen (EndedMsg restores
// the terminal) and come back as an error, with the stack on stderr after.
func TestTUIRunPanicEndsScreen(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "n.md")
	var stdout, stderr bytes.Buffer
	var stderrAtExit string
	p := &fakeProgram{
		panicOn: func(m tea.Msg) bool { _, ok := m.(tui.BoardMsg); return ok },
		onExit:  func() { stderrAtExit = stderr.String() },
	}
	events := make(chan assistant.Event)
	s := tuiSession{
		cfg:       assistant.Config{Notes: notes, Engine: "fake", Interval: time.Second, Think: func(_, _ string) (string, error) { return "", nil }},
		toggle:    make(chan string, 1),
		interrupt: func() {},
		start:     newFakeProgram(p),
	}
	done := make(chan error, 1)
	go func() { done <- s.run(events, &stdout, &stderr) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a panic in Run left the screen on")
	}
	if err == nil || !strings.Contains(err.Error(), "boom in the view") {
		t.Errorf("err = %v", err)
	}
	if stderrAtExit != "" {
		t.Errorf("stderr written while the screen was on: %q", stderrAtExit)
	}
	if !strings.Contains(stderr.String(), "panic: boom in the view") || !strings.Contains(stderr.String(), "goroutine") {
		t.Errorf("panic and stack missing on stderr:\n%s", stderr.String())
	}
}

// When the screen cannot run, the meeting ends as with Ctrl+C and the reason
// goes to the injected stderr.
func TestTUIScreenFailureEndsMeeting(t *testing.T) {
	notes := filepath.Join(t.TempDir(), "n.md")
	events := make(chan assistant.Event)
	var once sync.Once
	p := &fakeProgram{runErr: errors.New("no tty")}
	s := tuiSession{
		cfg:       assistant.Config{Notes: notes, Engine: "fake", Interval: time.Second, Think: func(_, _ string) (string, error) { return "", nil }},
		toggle:    make(chan string, 1),
		interrupt: func() { once.Do(func() { close(events) }) },
		start: func(m tea.Model) teaProgram {
			p.model, p.ended = m, make(chan struct{})
			return p
		},
	}
	var stdout, stderr bytes.Buffer
	if err := s.run(events, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "The screen stopped (no tty)") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Notes: "+notes) {
		t.Errorf("stdout = %q", stdout.String())
	}
}

func TestAssistRestoresFromNamedNotes(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "standup.md")
	mustWrite(t, notes, []byte(assistant.Render(assistant.Memory{
		Briefs: []assistant.Brief{{Text: "Sam is the PM"}},
		Fixes:  []assistant.Fix{{Wrong: "filming", Right: "rendering"}},
	}, "h", false, 0)), 0644)
	r, from, terms, err := restoreFromContext(notes)
	if err != nil || r == nil {
		t.Fatalf("restore = %v, err = %v", r, err)
	}
	if from != "standup.md" || len(r.Briefs) != 1 || len(r.Fixes) != 1 {
		t.Errorf("from = %q, restored = %+v", from, r)
	}
	if !slices.Contains(terms, "rendering") {
		t.Errorf("keyterms = %q, want rendering", terms)
	}
	// A notes file with a forged second section is plain context, with the error.
	bad := filepath.Join(dir, "bad.md")
	mustWrite(t, bad, []byte(assistant.Render(assistant.Memory{Fixes: []assistant.Fix{{Wrong: "a", Right: "b"}}}, "h", false, 0)+"\n## Fixes\n\n- `c` → `d`\n"), 0644)
	r, _, _, err = restoreFromContext(bad)
	if r != nil || err == nil || !strings.Contains(err.Error(), `two "## Fixes" sections`) {
		t.Errorf("bad notes: %v, %v", r, err)
	}
}

func TestAssistFolderRestoresNothing(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "standup.md"), []byte(assistant.Render(assistant.Memory{
		Fixes: []assistant.Fix{{Wrong: "filming", Right: "rendering"}},
	}, "h", false, 0)), 0644)
	r, from, terms, err := restoreFromContext(dir)
	if r != nil || from != "" || terms != nil || err != nil {
		t.Errorf("folder: %v, %q, %q, %v", r, from, terms, err)
	}
}

func TestLoadContextArgNotesIsADoc(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "standup.md")
	mustWrite(t, notes, []byte(assistant.Render(assistant.Memory{Summary: "Globex wants a refund."}, "h", false, 0)), 0644)
	bg, files, docs, _, err := loadContextArg(notes)
	if err != nil || bg != "" || len(files) != 0 || len(docs) != 1 {
		t.Fatalf("bg = %q, files = %q, docs = %d, err = %v", bg, files, len(docs), err)
	}
	d := docs[0]
	if d.Via != "notes" || d.Trusted() || d.Name != "standup.md" || d.Origin != notes || !strings.Contains(d.Text, "Globex wants a refund.") {
		t.Errorf("doc = %+v", d)
	}
	// A plain file and a folder stay trusted background.
	plain := filepath.Join(dir, "plain.md")
	mustWrite(t, plain, []byte("Acme kickoff notes"), 0644)
	for _, p := range []string{plain, dir} {
		bg, _, docs, _, err = loadContextArg(p)
		if err != nil || bg == "" || len(docs) != 0 {
			t.Errorf("%s: bg = %q, docs = %d, err = %v", p, bg, len(docs), err)
		}
	}
	if bg, _, docs, _, err = loadContextArg(""); bg != "" || docs != nil || err != nil {
		t.Errorf("empty path: %q, %v, %v", bg, docs, err)
	}
}

func TestLoadContextArgPageCopyAndFolder(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "page.md")
	mustWrite(t, cp, []byte(assistant.PageCopyMarker+"\n\n# Acme\n\nHostile text."), 0644)
	bg, files, docs, skipped, err := loadContextArg(cp)
	if err != nil || bg != "" || len(files) != 0 || len(docs) != 1 || len(skipped) != 0 {
		t.Fatalf("bg = %q, files = %q, docs = %d, skipped = %q, err = %v", bg, files, len(docs), skipped, err)
	}
	if d := docs[0]; d.Via != "web" || d.Trusted() || d.Origin != cp || !strings.Contains(d.Text, "Hostile text.") {
		t.Errorf("doc = %+v", d)
	}
	mustWrite(t, filepath.Join(dir, "plain.md"), []byte("Acme kickoff"), 0644)
	bg, _, docs, skipped, err = loadContextArg(dir)
	if err != nil || strings.Contains(bg, "Hostile") || !strings.Contains(bg, "Acme kickoff") || len(docs) != 0 || len(skipped) != 1 {
		t.Errorf("folder: bg = %q, docs = %d, skipped = %q, err = %v", bg, len(docs), skipped, err)
	}
	if n := assistant.LeftOutMessage(len(skipped)); !strings.HasPrefix(n, "left out 1 file(s): earlier notes, transcripts and page copies are not trusted background") {
		t.Errorf("notice = %q", n)
	}
}

func TestLoadContextArgTranscriptIsADoc(t *testing.T) {
	tr := filepath.Join(t.TempDir(), "standup-transcript.md")
	if err := assistant.WriteTranscript(tr, "HDR", []assistant.Line{{Speaker: "others", Text: "Hostile spoken text"}}); err != nil {
		t.Fatal(err)
	}
	bg, files, docs, skipped, err := loadContextArg(tr)
	if err != nil || bg != "" || len(files) != 0 || len(skipped) != 0 || len(docs) != 1 {
		t.Fatalf("bg = %q, files = %q, docs = %d, err = %v", bg, files, len(docs), err)
	}
	if d := docs[0]; d.Via != "transcript" || d.Trusted() || d.Origin != tr || !strings.Contains(d.Text, "Hostile spoken text") {
		t.Errorf("doc = %+v", d)
	}
}
