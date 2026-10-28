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
	for _, in := range []string{"/help", "/mute me", "/recap", "/fix cash cache", "/fix", "/fix yes", "/fix rm 1", "hello"} {
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
