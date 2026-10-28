package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"goscribe/internal/assistant"
	"goscribe/internal/tui"
)

// tuiHint ends the first notice of the full-screen view.
const tuiHint = "/help for the commands and keys, Ctrl+C to end the meeting"

// useTUI tells whether assist takes the full screen: not with -plain, and
// only when both stdin and stdout are terminals.
func useTUI(plain bool, stdin, stdout *os.File) bool {
	return !plain && isTerminal(stdin) && isTerminal(stdout)
}

// startupNotices are the lines shown before the meeting starts: where the
// notes and the transcript go (keys is the hint in parentheses), and the
// warnings about the keyterms left out and a large context.
func startupNotices(header, notes, transcript, keys string, tooLong []string, contextBytes int) []string {
	lines := []string{fmt.Sprintf("Assist %s → %s (%s)", header, notes+", transcript "+transcript, keys)}
	if len(tooLong) > 0 {
		lines = append(lines, fmt.Sprintf("⚠ %d keyterm(s) over %d characters left out (live speech-to-text refuses them): %s",
			len(tooLong), assistant.MaxKeytermLen, strings.Join(tooLong, ", ")))
	}
	if contextBytes > assistant.WarnContextBytes {
		lines = append(lines, fmt.Sprintf("⚠ the context is %d KB: every update resends it, which is slower and costlier", contextBytes>>10))
	}
	return lines
}

// printEndSummary is what stays in the terminal once the screen is gone: the
// final summary and action items, when there are some, and the file paths.
func printEndSummary(w io.Writer, m assistant.Memory, notes, transcript string) {
	if m.Summary != "" {
		fmt.Fprintf(w, "%s\n\n", m.Summary)
	}
	if len(m.ActionItems) > 0 {
		fmt.Fprintln(w, "Action items:")
		for _, a := range m.ActionItems {
			fmt.Fprintf(w, "- %s\n", a)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Notes: %s\n", notes)
	if transcript != "" {
		fmt.Fprintf(w, "Transcript: %s\n", transcript)
	}
}

// teaProgram is the part of tea.Program that the session uses (a fake in tests).
type teaProgram interface {
	Send(tea.Msg)
	Run() (tea.Model, error)
}

// tuiSession runs one meeting under the full-screen view.
type tuiSession struct {
	cfg       assistant.Config // View, Input, Final, Out and Interactive are set by run
	notices   []string         // startupNotices, shown first in the feed
	sides     []string         // captured speakers
	toggle    chan<- string    // mute and recap labels for streamSides
	interrupt func()           // Ctrl+C: first ends the meeting, second quits at once
	start     func(tea.Model) teaProgram
	// stdio, when set, points stdout and stderr at notice while the screen
	// is on and returns the function that puts them back.
	stdio func(notice func(string)) (restore func())
}

// newTeaProgram starts the real screen on the terminal. Signals stay with
// watchSignals: Bubble Tea's own handler would quit on SIGTERM.
func newTeaProgram(out *os.File) func(tea.Model) teaProgram {
	return func(m tea.Model) teaProgram {
		return tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithOutput(out), tea.WithoutSignalHandler())
	}
}

// run shows the meeting until Run returns, then prints the end summary on
// stdout. Nothing is written to stdout while the screen is on.
func (s tuiSession) run(events <-chan assistant.Event, stdout io.Writer) error {
	inputs := make(chan assistant.Event, 16)
	// Both never block: Run reads Input only while it runs, and the toggle
	// reader stops with the capture.
	p := s.start(tui.New(tui.Options{
		Send: func(e assistant.Event) {
			select {
			case inputs <- e:
			default:
			}
		},
		Toggle: func(label string) {
			select {
			case s.toggle <- label:
			default:
			}
		},
		Interrupt: s.interrupt,
		Sides:     s.sides,
	}))

	restore := func() {}
	var screenGone atomic.Bool // the lines that come later go to the real stderr
	if s.stdio != nil {
		errOut := os.Stderr
		restore = s.stdio(func(line string) {
			if screenGone.Load() {
				fmt.Fprintln(errOut, line)
				return
			}
			p.Send(tui.ItemMsg(assistant.Item{Kind: assistant.ItemNotice, Text: line, Plain: line}))
		})
	}
	defer restore()

	var mem assistant.Memory
	c := s.cfg
	c.View, c.Input, c.Out, c.Interactive = tui.NewView(p.Send), inputs, io.Discard, false
	c.Final = func(m assistant.Memory, _ string) { mem = m }
	done := make(chan error, 1)
	go func() {
		for _, n := range s.notices {
			c.View.Show(assistant.Item{Kind: assistant.ItemNotice, Text: n, Plain: n})
		}
		done <- assistant.Run(c, events)
		p.Send(tui.EndedMsg{})
	}()

	_, perr := p.Run()
	screenGone.Store(true)
	restore()
	var err error
	select {
	case err = <-done:
	default:
		// The screen failed before the meeting ended: end it, as Ctrl+C would.
		fmt.Fprintf(os.Stderr, "The screen stopped (%v): writing the final notes, Ctrl+C to quit at once\n", perr)
		s.interrupt()
		err = <-done
	}
	printEndSummary(stdout, mem, c.Notes, c.Transcript)
	return err
}

// captureStdio points os.Stdout and os.Stderr at a pipe and hands notice
// each non-empty line written there, so that a stray print (an API retry
// message) lands in the feed instead of on the screen. restore puts them
// back and waits for the last lines; it is safe to call more than once.
func captureStdio(notice func(string)) (restore func()) {
	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				notice(line)
			}
		}
		_, _ = io.Copy(io.Discard, r) // after a too long line: keep the writers from blocking
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			os.Stdout, os.Stderr = origOut, origErr
			_ = w.Close()
			<-done
			_ = r.Close()
		})
	}
}
