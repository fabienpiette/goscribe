package assistant

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Command is one parsed line of the chat input.
type Command struct {
	Name   string        // "help", "mute", "recap", "fix", "brief" or "question"
	Arg    string        // "me" or "others" for mute
	Window time.Duration // recap window, 0 means the default
	Text   string        // the question
	// Op is the /fix operation: "add", "list", "rm", "yes" or "no".
	Op           string
	Wrong, Right string // /fix add: what was misheard and what it should read
	N            int    // /fix rm, yes, no: the fix number; 0 means the latest or none given
	// /brief reuses Op ("add", "list" or "rm"), Text and N.
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
	{"/fix <wrong> <right>", `correct a misheard word from now on and in what was said; quote phrases: /fix "dark mood" "dark mode"`},
	{"/fix yes|no [n]", "accept or reject a suggested fix (FIX?), the latest by default"},
	{"/fix [rm n]", "list the fixes, or stop fix n for new lines"},
	{"/brief <text>", "add a standing instruction or fact for the assistant, e.g. /brief Sam is the PM"},
	{"/brief [rm n]", "list the briefs, or remove brief n"},
	{"<text>", "ask a question about the meeting"},
}

const (
	muteUsage   = "usage: /mute me|others"
	recapUsage  = "usage: /recap [duration], e.g. /recap 10m"
	fixUsage    = "usage: /fix <wrong> <right>"
	fixRmUsage  = "usage: /fix rm <n>"
	fixAnsUsage = "usage: /fix yes|no [n]"

	briefRmUsage = "usage: /brief rm <n>"
	maxBriefLen  = maxItem
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
	case "fix":
		return parseFix(input[len(fields[0]):])
	case "brief":
		return parseBrief(strings.TrimSpace(input[len(fields[0]):]))
	}
	return Command{}, fmt.Errorf("unknown command /%s: type /help", name)
}

// fixArg is one /fix argument; quoted ones are never an operation word.
type fixArg struct {
	text   string
	quoted bool
}

// splitFixArgs splits on spaces, with double quotes grouping words into one
// argument; ok is false when a quote is left open.
func splitFixArgs(s string) (args []fixArg, ok bool) {
	var cur strings.Builder
	inWord, quoted, inQuote := false, false, false
	flush := func() {
		if inWord {
			args = append(args, fixArg{cur.String(), quoted})
		}
		cur.Reset()
		inWord, quoted = false, false
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, inWord, quoted = !inQuote, true, true
		case !inQuote && unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inQuote {
		return nil, false
	}
	flush()
	return args, true
}

// parseFix reads what follows /fix.
func parseFix(rest string) (Command, error) {
	args, ok := splitFixArgs(rest)
	if !ok {
		return Command{}, errors.New(fixUsage)
	}
	if len(args) == 0 {
		return Command{Name: "fix", Op: "list"}, nil
	}
	if first := args[0]; !first.quoted {
		switch first.text {
		case "rm":
			n, err := strconv.Atoi(argText(args, 1))
			if len(args) != 2 || err != nil || n <= 0 {
				return Command{}, errors.New(fixRmUsage)
			}
			return Command{Name: "fix", Op: "rm", N: n}, nil
		case "yes", "no":
			n := 0
			if len(args) > 2 {
				return Command{}, errors.New(fixAnsUsage)
			}
			if len(args) == 2 {
				var err error
				if n, err = strconv.Atoi(args[1].text); err != nil || n <= 0 {
					return Command{}, errors.New(fixAnsUsage)
				}
			}
			return Command{Name: "fix", Op: first.text, N: n}, nil
		}
	}
	if len(args) != 2 {
		return Command{}, errors.New(fixUsage)
	}
	wrong, right := strings.TrimSpace(args[0].text), strings.TrimSpace(args[1].text)
	if err := ValidateFix(wrong, right); err != nil {
		return Command{}, err
	}
	return Command{Name: "fix", Op: "add", Wrong: wrong, Right: right}, nil
}

func argText(args []fixArg, i int) string {
	if i < len(args) {
		return args[i].text
	}
	return ""
}

// ValidateBrief checks the text a user typed for /brief.
func ValidateBrief(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("usage: /brief <instruction or fact>")
	}
	if len([]rune(text)) > maxBriefLen {
		return fmt.Errorf("a brief is at most %d characters", maxBriefLen)
	}
	return nil
}

// parseBrief reads what follows /brief: nothing lists the briefs, "rm n"
// removes one, anything else is a new brief.
func parseBrief(rest string) (Command, error) {
	if rest == "" {
		return Command{Name: "brief", Op: "list"}, nil
	}
	if f := strings.Fields(rest); f[0] == "rm" {
		if len(f) != 2 {
			return Command{}, errors.New(briefRmUsage)
		}
		n, err := strconv.Atoi(f[1])
		if err != nil || n <= 0 {
			return Command{}, errors.New(briefRmUsage)
		}
		return Command{Name: "brief", Op: "rm", N: n}, nil
	}
	text := strings.TrimSpace(Clean(rest))
	if err := ValidateBrief(text); err != nil {
		return Command{}, err
	}
	return Command{Name: "brief", Op: "add", Text: text}, nil
}
