package assistant

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuffer is an io.Writer the test can read while Run writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeThink records prompts and answers from a script of replies.
type fakeThink struct {
	mu      sync.Mutex
	calls   []string // transcript parts
	instr   []string
	replies []func() (string, error)
	active  int
	maxSeen int
}

func (f *fakeThink) think(instructions, transcript string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, transcript)
	f.instr = append(f.instr, instructions)
	i := len(f.calls) - 1
	f.active++
	f.maxSeen = max(f.maxSeen, f.active)
	reply := f.replies[min(i, len(f.replies)-1)]
	f.mu.Unlock()
	defer func() { f.mu.Lock(); f.active--; f.mu.Unlock() }()
	return reply()
}

func (f *fakeThink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeThink) call(i int) (instructions, transcript string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.instr[i], f.calls[i]
}

func answer(s string) func() (string, error) { return func() (string, error) { return s, nil } }

func fastTimers(t *testing.T) {
	origTick, origWatch := Tick, MinWatchdog
	Tick, MinWatchdog = 5*time.Millisecond, time.Hour
	t.Cleanup(func() { Tick, MinWatchdog = origTick, origWatch })
}

func line(speaker, text string) Event { return Event{Kind: LineEvent, Speaker: speaker, Text: text} }

const twelveWords = "we should ship the migration on monday if the rollback plan holds"

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestRunCycleSuggestsAndWritesNotes(t *testing.T) {
	fastTimers(t)
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) {
			<-release // keep the first call in flight
			return `{"summary":"Shipping plan.","decisions":["Ship Monday"],"suggestions":[{"type":"question","text":"Who owns the rollback?","why":"not named yet"},` +
				`{"type":"answer","priority":"high","text":"The rollback is the feature flag.","source":"spec.md"}]}`, nil
		},
		answer(`{"summary":"Shipping plan, rollback owner named.","suggestions":[]}`),
		answer(`{"summary":"Final.","action_items":["Sam: write the rollback runbook"]}`),
	}}
	out := &syncBuffer{}
	notes := filepath.Join(t.TempDir(), "notes.md")
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Context: "### file: spec.md\n\nRollback = feature flag.", Notes: notes, Header: "mic · fake", Engine: "fake", Interval: 20 * time.Millisecond,
			Think: f.think, Out: out}, events)
	}()

	events <- line("others", twelveWords)
	waitUntil(t, "first call", func() bool { return f.count() == 1 })
	events <- line("me", "Sam owns the rollback and he will write the runbook today for us all before the freeze starts")
	time.Sleep(60 * time.Millisecond) // several intervals: still only one call in flight
	if n := f.count(); n != 1 {
		t.Fatalf("calls while one is in flight = %d", n)
	}
	close(release)
	waitUntil(t, "second call", func() bool { return f.count() == 2 })
	_, tr := f.call(1)
	if !strings.Contains(tr, "New since the last update:\n[") || !strings.Contains(tr, "me: Sam owns the rollback") ||
		strings.Contains(tr[strings.Index(tr, "New since"):], "others: we should ship") {
		t.Errorf("second update must send only the line that arrived during the first call:\n%s", tr)
	}
	instr, _ := f.call(1)
	if !strings.Contains(instr, "Summary: Shipping plan.") || !strings.Contains(instr, "- [question] Who owns the rollback?") {
		t.Errorf("second update must carry the memory:\n%s", instr)
	}

	close(events)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.maxSeen != 1 {
		t.Errorf("max calls in flight = %d", f.maxSeen)
	}
	feed := out.String()
	for _, want := range []string{
		"others we should ship",
		"❓ ", "Ask: Who owns the rollback? — not named yet",
		"‼ 💬 ", "Answer: The rollback is the feature flag. — spec.md",
		"✅ ", "Decision: Ship Monday",
		"⏳ writing the final notes (fake)…",
		"✓ Final notes written to " + notes,
	} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%s", want, feed)
		}
	}
	if strings.Contains(feed, "\r") || strings.Contains(feed, "\033") {
		t.Errorf("non-interactive output must have no terminal codes:\n%q", feed)
	}
	b, _ := os.ReadFile(notes)
	for _, want := range []string{"mic · fake · started " + time.Now().Format("2006-01-02"), "## Final summary\n\nFinal.",
		"- Sam: write the rollback runbook", "- Ship Monday", "❓ Ask: Who owns the rollback?"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("notes missing %q:\n%s", want, b)
		}
	}
	if _, final := f.call(2); !strings.HasPrefix(final, "<transcript>\n[") {
		t.Errorf("final call must get the whole transcript: %q", final)
	}
}

func TestRunWaitsForEnoughWords(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`)}}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 50 * time.Millisecond, Think: f.think, Out: &syncBuffer{}}, events)
	}()
	events <- line("me", "short one")
	time.Sleep(40 * time.Millisecond)
	if f.count() != 0 {
		t.Fatal("a few words must wait for more, or for 2×Interval")
	}
	waitUntil(t, "update after 2×Interval", func() bool { return f.count() == 1 })
	close(events)
	<-done
	if f.count() != 1 {
		t.Errorf("under %d words in total there is no final call; calls = %d", minFinalWords, f.count())
	}
}

func TestRunBacksOffAfterFailure(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) { return "", errors.New("exit status 1\nstderr: boom\x1b[2J") },
		answer("not json"),
		answer(`{"summary":"ok"}`),
	}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	interval := 30 * time.Millisecond
	go func() {
		done <- Run(Config{Engine: "fake", Interval: interval, Think: f.think, Out: out}, events)
	}()
	events <- line("me", twelveWords)
	waitUntil(t, "first call", func() bool { return f.count() == 1 })
	start := time.Now()
	waitUntil(t, "retry", func() bool { return f.count() == 2 })
	if d := time.Since(start); d < 2*interval-5*time.Millisecond {
		t.Errorf("retry after %s, want ≥ 2×interval", d)
	}
	waitUntil(t, "second retry", func() bool { return f.count() == 3 })
	if d := time.Since(start); d < 6*interval-10*time.Millisecond {
		t.Errorf("second retry after %s, want ≥ 2×+4×interval", d)
	}
	close(events)
	<-done
	feed := out.String()
	if strings.Contains(feed, "\x1b") {
		t.Errorf("error text from the model call reached the terminal with control characters:\n%q", feed)
	}
	if !strings.Contains(feed, "⚠ fake failed: exit status 1 stderr: boom[2J; next try in") || !strings.Contains(feed, "no JSON object") {
		t.Errorf("failures must be reported on one line each:\n%s", feed)
	}
}

func TestRunWatchdogSkipsHungCall(t *testing.T) {
	fastTimers(t)
	MinWatchdog = 30 * time.Millisecond
	hang := make(chan struct{})
	defer close(hang)
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) { <-hang; return `{"summary":"late"}`, nil },
		answer(`{"summary":"fresh"}`),
	}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out}, events)
	}()
	events <- line("me", twelveWords)
	waitUntil(t, "a new call after the hung one", func() bool { return f.count() == 2 })
	if !strings.Contains(out.String(), "⚠ fake did not answer in") {
		t.Errorf("feed:\n%s", out.String())
	}
	close(events)
	<-done
}

func TestRunForceSkipsFinalNotes(t *testing.T) {
	fastTimers(t)
	hang := make(chan struct{})
	defer close(hang)
	f := &fakeThink{replies: []func() (string, error){
		answer(`{"summary":"Live summary."}`),
		func() (string, error) { <-hang; return "", nil },
	}}
	notes := filepath.Join(t.TempDir(), "notes.md")
	force := make(chan struct{})
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Notes: notes, Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think,
			Out: &syncBuffer{}, Force: force}, events)
	}()
	events <- line("me", twelveWords+" "+twelveWords+" "+twelveWords)
	waitUntil(t, "first update", func() bool {
		b, _ := os.ReadFile(notes)
		return strings.Contains(string(b), "Live summary.")
	})
	close(events)
	waitUntil(t, "final call", func() bool { return f.count() == 2 })
	close(force)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Force must stop the wait for the final notes")
	}
	b, _ := os.ReadFile(notes)
	if !strings.Contains(string(b), "## Summary so far\n\nLive summary.") {
		t.Errorf("notes must keep the live summary:\n%s", b)
	}
}

func TestRunInteractiveStatusAndCleaning(t *testing.T) {
	fastTimers(t)
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: (&fakeThink{}).think, Out: out, Interactive: true}, events)
	}()
	events <- Event{Kind: PartialEvent, Speaker: "others", Text: "so the next step\x1b[2J"}
	waitUntil(t, "status line", func() bool { return strings.Contains(out.String(), `others ▸ "so the next step[2J"`) })
	events <- line("others", "so the next step is the freeze\x07")
	events <- Event{Kind: NoticeEvent, Text: "↻ others: session limit reached"}
	close(events)
	<-done
	feed := out.String()
	if !strings.Contains(feed, "● ASSIST 00:00:00") || !strings.Contains(feed, "· listening") {
		t.Errorf("status line missing:\n%q", feed)
	}
	if !strings.Contains(feed, "\033[2m  00:00:00 others so the next step is the freeze\033[0m") {
		t.Errorf("transcript lines are dimmed and cleaned:\n%q", feed)
	}
	if strings.Contains(feed, "\x1b[2J") || strings.Contains(feed, "\x07") {
		t.Errorf("control characters from the transcript reached the terminal:\n%q", feed)
	}
	if !strings.Contains(feed, "↻ others: session limit reached") {
		t.Errorf("notices are shown:\n%q", feed)
	}
}
