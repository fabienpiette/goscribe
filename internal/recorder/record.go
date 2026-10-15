package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Overridable for tests.
var (
	Binary      = "ffmpeg"
	StopTimeout = 5 * time.Second
	Tick        = time.Second
)

var ErrNothingRecorded = errors.New("nothing was recorded")

// minAudioBytes separates a real recording from the ~230 bytes of ID3/Xing
// headers ffmpeg writes even when no audio frame was encoded. At 64 kbps,
// 1 KB is under 0.15 s of audio.
const minAudioBytes = 1024

// UI is where the session reports progress. Force, when closed, kills ffmpeg
// at once (second Ctrl+C). Each label received on Toggle mutes or unmutes that
// side in the recording; nil means no mute keys.
type UI struct {
	Status      io.Writer
	Interactive bool
	Force       <-chan struct{}
	Toggle      <-chan string
}

type Result struct {
	Output  string
	Elapsed time.Duration
	Size    int64
	Stopped bool // ended by ctx (Ctrl+C) rather than by ffmpeg itself
}

// Record runs ffmpeg until it ends on its own (-duration, error) or ctx is
// cancelled, in which case it asks ffmpeg to quit ("q") and kills it after
// StopTimeout or when ui.Force closes.
func Record(ctx context.Context, c Config, ui UI) (Result, error) {
	// ffmpeg's Pdeathsig (Linux) is tied to the thread that starts it: keep this
	// goroutine on one thread until the session ends.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	res := Result{Output: c.Output}
	args, err := Args(c)
	if err != nil {
		return res, err
	}
	bin, err := exec.LookPath(Binary)
	if err != nil {
		return res, fmt.Errorf("%s not found on PATH", Binary)
	}

	cmd := exec.Command(bin, args...)
	isolate(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return res, err
	}
	stderr := &tail{max: 20}
	cmd.Stderr = stderr

	// One pipe per recorded side: ffmpeg writes its silence events to fd 3, 4.
	labels := c.Labels()
	var readers []*os.File
	for range labels {
		r, w, err := os.Pipe()
		if err != nil {
			return res, err
		}
		readers = append(readers, r)
		cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	}
	closeAll := func(files []*os.File) {
		for _, f := range files {
			f.Close()
		}
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		closeAll(readers)
		closeAll(cmd.ExtraFiles)
		return res, fmt.Errorf("starting %s: %w", Binary, err)
	}
	closeAll(cmd.ExtraFiles) // ffmpeg holds its own copies

	events := make(chan Event, 16)
	var wg sync.WaitGroup
	for i, r := range readers {
		wg.Add(1)
		go func(r *os.File, label string) {
			defer wg.Done()
			defer r.Close()
			ReadEvents(r, label, events)
		}(r, labels[i])
	}
	go func() { wg.Wait(); close(events) }()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	silence := c.SilenceAfter
	if silence <= 0 {
		silence = DefaultSilenceAfter
	}
	ticker := time.NewTicker(Tick)
	defer ticker.Stop()
	lastLine := start

	toggle := ui.Toggle
	keys := toggle != nil
	muted := map[string]bool{}
	recorded := map[string]bool{}
	for _, l := range labels {
		recorded[l] = true
	}

	var waitErr error
loop:
	for {
		select {
		case waitErr = <-done:
			break loop
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			clearLine(ui)
			fmt.Fprintln(ui.Status, ev.Message(silence))
		case label, ok := <-toggle:
			if !ok {
				toggle = nil
				continue
			}
			if !recorded[label] {
				continue
			}
			muted[label] = !muted[label]
			volume, icon, word := 1, "🔊", "unmuted"
			if muted[label] {
				volume, icon, word = 0, "🔇", "muted"
			}
			// Sets the volume filter of that side; a failed write means ffmpeg is
			// gone, which <-done reports.
			_, _ = fmt.Fprintf(stdin, "cvolume@%s -1 volume %d\n", label, volume)
			clearLine(ui)
			fmt.Fprintf(ui.Status, "%s %s %s at %s\n", icon, label, word, FormatElapsed(time.Since(start)))
		case now := <-ticker.C:
			if ui.Interactive {
				fmt.Fprintf(ui.Status, "\r\033[K%s", statusLine(c, muted, keys, now.Sub(start)))
			} else if now.Sub(lastLine) >= time.Minute {
				fmt.Fprintln(ui.Status, statusLine(c, muted, keys, now.Sub(start)))
				lastLine = now
			}
		case <-ctx.Done():
			res.Stopped = true
			_, _ = io.WriteString(stdin, "q")
			stdin.Close()
			select {
			case waitErr = <-done:
			case <-time.After(StopTimeout):
				kill(cmd)
				waitErr = <-done
			case <-ui.Force:
				kill(cmd)
				waitErr = <-done
			}
			break loop
		}
	}
	clearLine(ui)

	res.Elapsed = time.Since(start)
	if info, err := os.Stat(c.Output); err == nil {
		res.Size = info.Size()
	}
	if res.Size < minAudioBytes {
		os.Remove(c.Output)
		res.Size = 0
	}

	// After our own stop, ffmpeg's exit status (0 on "q", killed after timeout)
	// doesn't matter: what counts is whether audio was written.
	if !res.Stopped && waitErr != nil {
		return res, fmt.Errorf("%s stopped unexpectedly: %v\n%s", Binary, waitErr, stderr.String())
	}
	if res.Size == 0 {
		return res, ErrNothingRecorded
	}
	return res, nil
}

func clearLine(ui UI) {
	if ui.Interactive {
		fmt.Fprint(ui.Status, "\r\033[K")
	}
}

// statusLine shows which sides are muted and, when keys is set, the mute keys
// of the recorded sides.
func statusLine(c Config, muted map[string]bool, keys bool, elapsed time.Duration) string {
	size := int64(0)
	if info, err := os.Stat(c.Output); err == nil {
		size = info.Size()
	}
	var sides []string
	for _, l := range c.Labels() {
		if muted[l] {
			sides = append(sides, l+" (muted)")
		} else {
			sides = append(sides, l)
		}
	}
	hint := "(Ctrl+C to stop)"
	if keys {
		hint = c.MuteHint() + ": mute · Ctrl+C: stop"
	}
	return fmt.Sprintf("● REC  %s  %s  %s -> %s   %s", FormatElapsed(elapsed), FormatSize(size), strings.Join(sides, " + "), c.Output, hint)
}

func FormatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s%3600/60, s%60)
}

func FormatSize(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// tail keeps the last max lines written to it (ffmpeg's stderr).
type tail struct {
	mu    sync.Mutex
	max   int
	lines []string
	part  string
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.part += string(p)
	for {
		line, rest, ok := strings.Cut(t.part, "\n")
		if !ok {
			break
		}
		t.part = rest
		// ffmpeg's replies to our mute commands are not errors.
		if strings.HasPrefix(line, "Enter command:") || strings.HasPrefix(line, "Command reply") || line == "" {
			continue
		}
		t.lines = append(t.lines, line)
		if len(t.lines) > t.max {
			t.lines = t.lines[1:]
		}
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := t.lines
	if t.part != "" {
		lines = append(append([]string{}, lines...), t.part)
	}
	return strings.Join(lines, "\n")
}
