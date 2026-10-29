package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
// Without final notes (forced, failed or too short), the summary is the live
// one and says so.
func printEndSummary(w io.Writer, m assistant.Memory, final bool, notes, transcript string) {
	if m.Summary != "" {
		if !final {
			fmt.Fprintln(w, "Live summary (final notes not written):")
		}
		fmt.Fprintf(w, "%s\n\n", assistant.Clean(m.Summary)) // model output: no control characters on the terminal
	}
	if len(m.ActionItems) > 0 {
		fmt.Fprintln(w, "Action items:")
		for _, a := range m.ActionItems {
			fmt.Fprintf(w, "- %s\n", assistant.Clean(a))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Notes: %s\n", notes)
	if transcript != "" {
		fmt.Fprintf(w, "Transcript: %s\n", transcript)
	}
	if dir := assistant.SourcesDir(notes); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			fmt.Fprintf(w, "Sources: %s\n", dir)
		}
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
		// The adaptive colors ask the terminal for its background (OSC 11) on
		// first use. Ask now: inside Run, Bubble Tea's raw mode and stdin
		// reader would race for the reply (a slow first frame, or the reply
		// read as keys).
		lipgloss.ColorProfile()
		lipgloss.HasDarkBackground()
		return tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithOutput(out), tea.WithoutSignalHandler())
	}
}

// run shows the meeting until Run returns, then prints the end summary on
// stdout. Nothing is written to stdout or stderr while the screen is on.
func (s tuiSession) run(events <-chan assistant.Event, stdout, stderr io.Writer) error {
	inputs := make(chan assistant.Event, 16)
	// Both never block: Run reads Input only while it runs, and the toggle
	// reader in streamSides stops with ctx (first Ctrl+C) or force.
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
	var screenGone atomic.Bool // the lines that come later go to stderr
	if s.stdio != nil {
		restore = s.stdio(func(line string) {
			if screenGone.Load() {
				fmt.Fprintln(stderr, line)
				return
			}
			p.Send(tui.ItemMsg(assistant.Item{Kind: assistant.ItemNotice, Text: line, Plain: line}))
		})
	}
	defer restore()

	var mem assistant.Memory
	var final bool
	c := s.cfg
	c.View, c.Input, c.Out, c.Interactive = tui.NewView(p.Send), inputs, io.Discard, false
	c.Final = func(m assistant.Memory, _ string, written bool) { mem, final = m, written }
	done := make(chan error, 1)
	var crash string // a panic in Run and its stack, printed once the screen is gone
	go func() {
		var err error
		// Bubble Tea recovers only its own goroutines: a panic here would
		// leave the terminal in raw mode on the alternate screen.
		defer func() {
			if r := recover(); r != nil {
				crash = fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack())
				err = fmt.Errorf("assist crashed: %v", r)
			}
			done <- err
			p.Send(tui.EndedMsg{})
		}()
		for _, n := range s.notices {
			c.View.Show(assistant.Item{Kind: assistant.ItemNotice, Text: n, Plain: n})
		}
		err = assistant.Run(c, events)
	}()

	_, perr := p.Run()
	screenGone.Store(true)
	restore()
	var err error
	select {
	case err = <-done:
	default:
		// The screen failed before the meeting ended: end it, as Ctrl+C would.
		fmt.Fprintf(stderr, "The screen stopped (%v): writing the final notes, Ctrl+C to quit at once\n", perr)
		s.interrupt()
		err = <-done
	}
	if crash != "" {
		fmt.Fprintln(stderr, crash)
	}
	printEndSummary(stdout, mem, final, c.Notes, c.Transcript)
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
