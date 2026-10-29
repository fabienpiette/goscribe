package assistant

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in      string
		want    Command
		wantErr string
		isEmpty bool
	}{
		{in: "what now?", want: Command{Name: "question", Text: "what now?"}},
		{in: "  /help ", want: Command{Name: "help"}},
		{in: "/mute me", want: Command{Name: "mute", Arg: "me"}},
		{in: "/mute others", want: Command{Name: "mute", Arg: "others"}},
		{in: "/mute", wantErr: "usage: /mute me|others"},
		{in: "/mute you", wantErr: "usage: /mute me|others"},
		{in: "/recap", want: Command{Name: "recap"}},
		{in: "/recap 10m", want: Command{Name: "recap", Window: 10 * time.Minute}},
		{in: "/recap -1m", wantErr: "usage: /recap [duration], e.g. /recap 10m"},
		{in: "/recap soon", wantErr: "usage: /recap [duration], e.g. /recap 10m"},
		{in: "/search when did PROJ-123 ship?", want: Command{Name: "search", Text: "when did PROJ-123 ship?"}},
		{in: "/search", wantErr: "usage: /search <question>"},
		{in: "/search   ", wantErr: "usage: /search <question>"},
		{in: "/fix", want: Command{Name: "fix", Op: "list"}},
		{in: "/fix cash cache", want: Command{Name: "fix", Op: "add", Wrong: "cash", Right: "cache"}},
		{in: `/fix "dark mood" "dark mode"`, want: Command{Name: "fix", Op: "add", Wrong: "dark mood", Right: "dark mode"}},
		{in: `/fix "yes" "yeah"`, want: Command{Name: "fix", Op: "add", Wrong: "yes", Right: "yeah"}},
		{in: `/fix "rm" cache`, want: Command{Name: "fix", Op: "add", Wrong: "rm", Right: "cache"}},
		{in: "/fix rm 2", want: Command{Name: "fix", Op: "rm", N: 2}},
		{in: "/fix rm", wantErr: "usage: /fix rm <n>"},
		{in: "/fix rm 0", wantErr: "usage: /fix rm <n>"},
		{in: "/fix rm x", wantErr: "usage: /fix rm <n>"},
		{in: "/fix yes", want: Command{Name: "fix", Op: "yes"}},
		{in: "/fix no", want: Command{Name: "fix", Op: "no"}},
		{in: "/fix yes 2", want: Command{Name: "fix", Op: "yes", N: 2}},
		{in: "/fix no 3", want: Command{Name: "fix", Op: "no", N: 3}},
		{in: "/fix yes x", wantErr: "usage: /fix yes|no [n]"},
		{in: "/fix no 0", wantErr: "usage: /fix yes|no [n]"},
		{in: "/fix yes 1 2", wantErr: "usage: /fix yes|no [n]"},
		{in: `/fix "dark mood dark mode`, wantErr: "usage: /fix <wrong> <right>"},
		{in: "/fix cash", wantErr: "usage: /fix <wrong> <right>"},
		{in: "/fix a b c", wantErr: "usage: /fix <wrong> <right>"},
		{in: "/fix a a", wantErr: "the fix changes nothing"},
		{in: `/fix cash ""`, wantErr: "usage: /fix <wrong> <right>"},
		{in: "/brief", want: Command{Name: "brief", Op: "list"}},
		{in: "/brief Sam is the PM", want: Command{Name: "brief", Op: "add", Text: "Sam is the PM"}},
		{in: "/brief   focus  on risks ", want: Command{Name: "brief", Op: "add", Text: "focus  on risks"}},
		{in: "/brief rm 2", want: Command{Name: "brief", Op: "rm", N: 2}},
		{in: "/brief rm", wantErr: "usage: /brief rm <n>"},
		{in: "/brief rm x", wantErr: "usage: /brief rm <n>"},
		{in: "/brief rm 0", wantErr: "usage: /brief rm <n>"},
		{in: "/brief " + strings.Repeat("é", 301), wantErr: "a brief is at most 300 characters"},
		{in: "/frobnicate", wantErr: "unknown command /frobnicate: type /help"},
		{in: "/ what", wantErr: "unknown command /: type /help"},
		{in: "", isEmpty: true},
		{in: "   ", isEmpty: true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseCommand(c.in)
			switch {
			case c.isEmpty:
				if !errors.Is(err, ErrEmpty) {
					t.Fatalf("err = %v, want ErrEmpty", err)
				}
			case c.wantErr != "":
				if err == nil || err.Error() != c.wantErr {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != c.want {
					t.Fatalf("got %+v, want %+v", got, c.want)
				}
			}
		})
	}
}

func TestCommandsHelpListsEveryCommand(t *testing.T) {
	for _, in := range []string{"/help", "/mute me", "/recap", "/fix cash cache", "/fix", "/fix yes", "/fix rm 1", "/brief x", "/brief", "/add x.md", "/add", "/context x.md", "hello"} {
		cmd, err := ParseCommand(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		name := "/" + cmd.Name
		if cmd.Name == "question" {
			name = "<text>"
		}
		found := false
		for _, h := range Commands {
			if strings.Contains(h.Usage, name) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s accepted by ParseCommand but missing from Commands", name)
		}
	}
}

func TestParseAdd(t *testing.T) {
	for _, tc := range []struct {
		in         string
		op, target string
		err        string
	}{
		{"/add notes/a b.md", "add", "notes/a b.md", ""},
		{`/context "x y.md"`, "add", "x y.md", ""},
		{"/add", "list", "", ""},
		{"/context", "list", "", ""},
		{"/add https://example.com/a", "add", "https://example.com/a", ""},
		{"/add ~/notes", "add", "~/notes", ""},
		{"/add ftp://x", "", "", "usage: /add <path or http(s) url>"},
		{"/add file:///x", "", "", "usage: /add <path or http(s) url>"},
		{`/add ""`, "", "", "usage: /add <path or http(s) url>"},
	} {
		cmd, err := ParseCommand(tc.in)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("%q: err = %v, want %q", tc.in, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if cmd.Name != "add" || cmd.Op != tc.op || cmd.Text != tc.target {
			t.Errorf("%q = %+v, want add/%s/%q", tc.in, cmd, tc.op, tc.target)
		}
	}
}

func TestCommandsHelpHasAddRows(t *testing.T) {
	var usages []string
	for _, h := range Commands {
		usages = append(usages, h.Usage)
	}
	joined := strings.Join(usages, "|")
	for _, want := range []string{"/add <path|url>", "/add"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Commands lacks %q", want)
		}
	}
}

func TestCommandsHelpHasSearchRow(t *testing.T) {
	for _, h := range Commands {
		if h.Usage == "/search <question>" {
			return
		}
	}
	t.Error(`Commands lacks the row "/search <question>"`)
}
