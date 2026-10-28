package assistant

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Command is one parsed line of the chat input.
type Command struct {
	Name   string        // "help", "mute", "recap" or "question"
	Arg    string        // "me" or "others" for mute
	Window time.Duration // recap window, 0 means the default
	Text   string        // the question
}

// ErrEmpty means the input was blank; callers ignore it.
var ErrEmpty = errors.New("empty input")

// CommandHelp is one line of the /help table.
type CommandHelp struct{ Usage, Summary string }

// Commands drives /help, in display order.
var Commands = []CommandHelp{
	{"/help", "list the commands and keys"},
	{"/mute me|others", "mute or unmute your mic or the computer sound (also Alt+m, Alt+s)"},
	{"/recap [duration]", "recap of the last minutes, e.g. /recap 10m (also Alt+w)"},
	{"<text>", "ask a question about the meeting"},
}

const (
	muteUsage  = "usage: /mute me|others"
	recapUsage = "usage: /recap [duration], e.g. /recap 10m"
)

// ParseCommand reads a chat line: plain text is a question, a leading / starts a command.
func ParseCommand(input string) (Command, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Command{}, ErrEmpty
	}
	if !strings.HasPrefix(input, "/") {
		return Command{Name: "question", Text: input}, nil
	}
	fields := strings.Fields(input)
	name, args := strings.TrimPrefix(fields[0], "/"), fields[1:]
	switch name {
	case "help":
		return Command{Name: "help"}, nil
	case "mute":
		if len(args) != 1 || (args[0] != "me" && args[0] != "others") {
			return Command{}, errors.New(muteUsage)
		}
		return Command{Name: "mute", Arg: args[0]}, nil
	case "recap":
		if len(args) == 0 {
			return Command{Name: "recap"}, nil
		}
		d, err := time.ParseDuration(args[0])
		if len(args) > 1 || err != nil || d <= 0 {
			return Command{}, errors.New(recapUsage)
		}
		return Command{Name: "recap", Window: d}, nil
	}
	return Command{}, fmt.Errorf("unknown command /%s: type /help", name)
}
