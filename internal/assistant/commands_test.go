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
	for _, in := range []string{"/help", "/mute me", "/recap", "hello"} {
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
