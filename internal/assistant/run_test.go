package assistant

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
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

// fakeResearch records librarian prompts and answers with reply.
type fakeResearch struct {
	mu      sync.Mutex
	prompts []string
	reply   func() (string, error)
	refused []string // tool calls the fake librarian reports as refused
}

func (f *fakeResearch) research(prompt string) (ResearchReply, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	reply, refused := f.reply, f.refused
	f.mu.Unlock()
	a, err := reply()
	return ResearchReply{Answer: a, Refused: refused}, err
}

func (f *fakeResearch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.prompts)
}

func (f *fakeResearch) prompt(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prompts[i]
}

func lookupReply(query string) func() (string, error) {
	return answer(`{"summary":"s","lookups":[{"query":"` + query + `","why":"asked now"}]}`)
}

const foundT1 = `{"found":true,"answer":"Acme chose the flag rollout.","sources":[{"title":"T1","link":"https://example.com/1"}]}`

func TestRunLibrarianLookup(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){
		lookupReply("What did Acme decide on PROJ-123 last week?"),
		answer(`{"summary":"s2"}`),
	}}
	r := &fakeResearch{reply: answer(foundT1)}
	out := &syncBuffer{}
	notes := filepath.Join(t.TempDir(), "notes.md")
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Notes: notes, Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
			Research: r.research, LibrarianInstructions: "Search the tickets.", MaxLookups: 3}, events)
	}()
	events <- line("others", twelveWords)
	waitUntil(t, "the finding in the feed", func() bool { return strings.Contains(out.String(), "Librarian:") })
	if p := r.prompt(0); !strings.Contains(p, "Question: What did Acme decide on PROJ-123 last week?") || !strings.Contains(p, "Search the tickets.") {
		t.Errorf("librarian prompt:\n%s", p)
	}
	events <- line("me", twelveWords)
	waitUntil(t, "second cycle", func() bool { return f.count() == 2 })
	instr, _ := f.call(1)
	if !strings.Contains(instr, "What did Acme decide on PROJ-123 last week? → Acme chose the flag rollout. (T1 <https://example.com/1>)") {
		t.Errorf("the next cycle must carry the finding:\n%s", instr)
	}
	if !strings.Contains(instr, `"lookups"`) {
		t.Errorf("with the librarian, the cycle asks for lookups:\n%s", instr)
	}
	close(events)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	feed := out.String()
	look := strings.Index(feed, "🔎 00:00:00 Looking up: What did Acme decide on PROJ-123 last week?")
	found := strings.Index(feed, "📚 00:00:00 Librarian: Acme chose the flag rollout. — T1\n")
	if look < 0 || found < 0 || look > found {
		t.Errorf("feed must show 🔎 then 📚:\n%s", feed)
	}
	if r.count() != 1 {
		t.Errorf("research calls = %d", r.count())
	}
	b, _ := os.ReadFile(notes)
	if !strings.Contains(string(b), "## Research") || !strings.Contains(string(b), "Acme chose the flag rollout.") {
		t.Errorf("notes must have the research:\n%s", b)
	}
}

// runThreeLookups asks three lookups in three cycles while the first one
// hangs until release is closed: the second waits, the third replaces it. It
// returns once the third reply is applied.
func runThreeLookups(t *testing.T) (*fakeResearch, *syncBuffer, chan struct{}, chan<- Event, func()) {
	t.Helper()
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		lookupReply("When was the Globex contract signed?"),
		lookupReply("Who owns the billing migration ticket?"),
		answer(`{"summary":"s","decisions":["third applied"],"lookups":[{"query":"What latency did the March load test show?","why":"asked now"}]}`),
		answer(`{"summary":"final"}`),
	}}
	r := &fakeResearch{reply: func() (string, error) { <-release; return foundT1, nil }}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
			Research: r.research}, events)
	}()
	for i := 1; i <= 3; i++ {
		events <- line("me", twelveWords)
		waitUntil(t, "cycle", func() bool { return f.count() == i })
		if i == 1 {
			waitUntil(t, "first lookup", func() bool { return r.count() == 1 })
		}
	}
	// The decision is printed while the third reply is applied; its lookup is
	// queued in the same step, before Run handles anything else.
	waitUntil(t, "third reply applied", func() bool { return strings.Contains(out.String(), "Decision: third applied") })
	return r, out, release, events, func() { close(events); <-done }
}

func TestRunLibrarianOneAtATime(t *testing.T) {
	fastTimers(t)
	r, out, release, _, stop := runThreeLookups(t)
	if n := r.count(); n != 1 {
		t.Fatalf("research calls while one runs = %d", n)
	}
	close(release)
	waitUntil(t, "the newest pending lookup", func() bool { return r.count() == 2 })
	if p := r.prompt(1); !strings.Contains(p, "What latency did the March load test show?") {
		t.Errorf("the newest pending lookup replaces older ones:\n%s", p)
	}
	waitUntil(t, "second finding", func() bool { return strings.Count(out.String(), "Librarian:") == 2 })
	stop()
	// Lookups start (and print 🔎) inside Run: once it returns, the feed is final.
	if n := strings.Count(out.String(), "🔎"); n != 2 {
		t.Errorf("lookups started = %d, want 2:\n%s", n, out.String())
	}
}

func TestRunLibrarianDropsStalePending(t *testing.T) {
	fastTimers(t)
	orig := LookupMaxWait
	LookupMaxWait = time.Millisecond
	t.Cleanup(func() { LookupMaxWait = orig })
	r, out, release, events, stop := runThreeLookups(t)
	// Run takes this event only after the third reply is fully applied, so the
	// third lookup is pending from before the sleep, which makes it stale.
	events <- line("me", "ok")
	time.Sleep(10 * LookupMaxWait)
	close(release)
	waitUntil(t, "first finding", func() bool { return strings.Contains(out.String(), "Librarian:") })
	stop()
	if n := r.count(); n != 1 {
		t.Errorf("a stale pending lookup must be dropped; research calls = %d", n)
	}
	// Dropped silently: one 🔎 line, and no other librarian line.
	feed := out.String()
	if strings.Count(feed, "🔎") != 1 || strings.Count(feed, "📚") != 1 || strings.Contains(feed, "⚠") {
		t.Errorf("a stale drop must be silent:\n%s", feed)
	}
}

// A replaced lookup never ran: it frees its slot and can be asked again.
func TestRunLibrarianReplacedCanBeAskedAgain(t *testing.T) {
	fastTimers(t)
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		lookupReply("When was the Globex contract signed?"),
		lookupReply("Who owns the billing migration ticket?"),
		answer(`{"summary":"s","decisions":["third applied"],"lookups":[{"query":"What latency did the March load test show?","why":"asked now"}]}`),
		lookupReply("Who owns the billing migration ticket?"),
		answer(`{"summary":"final"}`),
	}}
	r := &fakeResearch{reply: func() (string, error) { <-release; return foundT1, nil }}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
			Research: r.research, MaxLookups: 3}, events)
	}()
	for i := 1; i <= 3; i++ {
		events <- line("me", twelveWords)
		waitUntil(t, "cycle", func() bool { return f.count() == i })
	}
	waitUntil(t, "third reply applied", func() bool { return strings.Contains(out.String(), "Decision: third applied") })
	close(release)
	waitUntil(t, "the third lookup", func() bool { return r.count() == 2 })
	waitUntil(t, "second finding", func() bool { return strings.Count(out.String(), "Librarian:") == 2 })
	events <- line("me", twelveWords)
	waitUntil(t, "the replaced query, asked again", func() bool { return r.count() == 3 })
	if p := r.prompt(2); !strings.Contains(p, "Question: Who owns the billing migration ticket?") {
		t.Errorf("third research prompt:\n%s", p)
	}
	close(events)
	<-done
	if strings.Contains(out.String(), "lookup limit reached") {
		t.Errorf("three lookups ran with MaxLookups 3; the replaced one must not count:\n%s", out.String())
	}
}

// A failed lookup ran: it still counts toward the limit.
func TestRunLibrarianFailedLookupCounts(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){
		lookupReply("When was the Globex contract signed?"),
		lookupReply("Who owns the billing migration ticket?"),
		answer(`{"summary":"final"}`),
	}}
	r := &fakeResearch{reply: func() (string, error) { return "", errors.New("exit status 1: not logged in") }}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
			Research: r.research, MaxLookups: 1}, events)
	}()
	events <- line("me", twelveWords)
	waitUntil(t, "failure line", func() bool {
		return strings.Contains(out.String(), "⚠ librarian failed: exit status 1: not logged in\n")
	})
	events <- line("me", twelveWords)
	waitUntil(t, "limit line", func() bool { return strings.Contains(out.String(), "📚 lookup limit reached (1)") })
	close(events)
	<-done
	if n := r.count(); n != 1 {
		t.Errorf("research calls = %d, want 1", n)
	}
}

func TestRunLibrarianFailure(t *testing.T) {
	for name, reply := range map[string]func() (string, error){
		"error": func() (string, error) { return "", errors.New("exit status 1\x1b[2J") },
		"prose": answer("I could not find anything, sorry."),
	} {
		t.Run(name, func(t *testing.T) {
			fastTimers(t)
			f := &fakeThink{replies: []func() (string, error){
				lookupReply("When was the Globex contract signed?"),
				answer(`{"summary":"s2"}`),
			}}
			r := &fakeResearch{reply: reply}
			out := &syncBuffer{}
			notes := filepath.Join(t.TempDir(), "notes.md")
			events := make(chan Event)
			done := make(chan error, 1)
			go func() {
				done <- Run(Config{Notes: notes, Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
					Research: r.research}, events)
			}()
			events <- line("me", twelveWords)
			waitUntil(t, "failure line", func() bool { return strings.Contains(out.String(), "⚠ librarian failed: ") })
			events <- line("me", twelveWords)
			waitUntil(t, "second cycle", func() bool { return f.count() == 2 })
			close(events)
			<-done
			if instr, _ := f.call(1); strings.Contains(instr, "signed? →") {
				t.Errorf("a failed lookup must not reach the memory:\n%s", instr)
			}
			if strings.Contains(out.String(), "\x1b") {
				t.Errorf("control characters reached the feed:\n%q", out.String())
			}
			b, _ := os.ReadFile(notes)
			if strings.Contains(string(b), "## Research") {
				t.Errorf("notes:\n%s", b)
			}
		})
	}
}

func TestRunLibrarianLimit(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){
		lookupReply("When was the Globex contract signed?"),
		lookupReply("Who owns the billing migration ticket?"),
		lookupReply("What latency did the March load test show?"),
	}}
	r := &fakeResearch{reply: answer(`{"found":false,"answer":"","sources":[]}`)}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
			Research: r.research, MaxLookups: 1}, events)
	}()
	events <- line("me", twelveWords)
	waitUntil(t, "nothing found", func() bool {
		return strings.Contains(out.String(), "📚 nothing found for When was the Globex contract signed?")
	})
	events <- line("me", twelveWords)
	waitUntil(t, "limit line", func() bool { return strings.Contains(out.String(), "📚 lookup limit reached (1)") })
	events <- line("me", "short")
	waitUntil(t, "third cycle", func() bool { return f.count() == 3 })
	time.Sleep(30 * time.Millisecond)
	close(events)
	<-done
	if n := strings.Count(out.String(), "lookup limit reached"); n != 1 {
		t.Errorf("limit lines = %d:\n%s", n, out.String())
	}
	if n := r.count(); n != 1 {
		t.Errorf("research calls = %d", n)
	}
}

func TestRunLibrarianEndDoesNotWait(t *testing.T) {
	fastTimers(t)
	hang := make(chan struct{})
	defer close(hang)
	f := &fakeThink{replies: []func() (string, error){
		lookupReply("When was the Globex contract signed?"),
		answer(`{"summary":"Final."}`),
	}}
	r := &fakeResearch{reply: func() (string, error) { <-hang; return foundT1, nil }}
	notes := filepath.Join(t.TempDir(), "notes.md")
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Notes: notes, Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: &syncBuffer{},
			Research: r.research}, events)
	}()
	events <- line("me", twelveWords+" "+twelveWords+" "+twelveWords)
	waitUntil(t, "lookup running", func() bool { return r.count() == 1 })
	close(events)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the end must not wait for a lookup")
	}
	b, _ := os.ReadFile(notes)
	if !strings.Contains(string(b), "## Final summary\n\nFinal.") {
		t.Errorf("final notes:\n%s", b)
	}
}

func TestRunWithoutLibrarian(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){lookupReply("When was the Globex contract signed?")}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out}, events)
	}()
	events <- line("me", twelveWords)
	waitUntil(t, "cycle", func() bool { return f.count() == 1 })
	time.Sleep(20 * time.Millisecond)
	close(events)
	<-done
	if strings.Contains(out.String(), "🔎") || strings.Contains(out.String(), "📚") {
		t.Errorf("no lookups without the librarian:\n%s", out.String())
	}
	if instr, _ := f.call(0); strings.Contains(instr, `"lookups"`) {
		t.Errorf("without the librarian, the cycle must not ask for lookups:\n%s", instr)
	}
}

// A new lookup that is refused (here: a question that already ran) leaves the
// waiting one in place and in Asked; one like the waiting question replaces it.
func TestQueueLookupKeepsOrReplacesWaiting(t *testing.T) {
	s := &state{c: Config{Research: func(string) (ResearchReply, error) { return ResearchReply{}, nil }, Out: &syncBuffer{}}, looking: true}
	s.mem.Asked = []string{"When was the Globex contract signed?"} // ran already
	s.queueLookup(Reply{Lookups: Lookups{{Query: "Who owns the billing migration ticket?"}}}, 0)
	s.queueLookup(Reply{Lookups: Lookups{{Query: "when was the Globex contract signed"}}}, 0)
	if s.pending == nil || s.pending.Query != "Who owns the billing migration ticket?" {
		t.Errorf("pending = %+v", s.pending)
	}
	want := []string{"When was the Globex contract signed?", "Who owns the billing migration ticket?"}
	if !slices.Equal(s.mem.Asked, want) {
		t.Errorf("asked = %q, want %q", s.mem.Asked, want)
	}
	s.queueLookup(Reply{Lookups: Lookups{{Query: "who owns the billing migration ticket now"}}}, 0)
	want = []string{"When was the Globex contract signed?", "who owns the billing migration ticket now"}
	if s.pending == nil || s.pending.Query != want[1] || !slices.Equal(s.mem.Asked, want) {
		t.Errorf("pending = %+v, asked = %q", s.pending, s.mem.Asked)
	}
}

// Calls the allowlist refused are shown once per lookup, so the user can fix
// librarian.allowed_tools; the answer still counts.
func TestRunLibrarianShowsRefusedCalls(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){lookupReply("Who owns PROJ-123?"), answer(`{"summary":"s2"}`)}}
	r := &fakeResearch{reply: answer(foundT1), refused: []string{"Bash(tracker list | head -5)", "mcp__chat__search_users", "Read(/etc/hosts)", "Glob(**)"}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 5 * time.Millisecond, Think: f.think, Out: out,
			Research: r.research, LibrarianTools: []string{"Bash(tracker get:*)"}, MaxLookups: 3}, events)
	}()
	events <- line("others", twelveWords)
	waitUntil(t, "the finding", func() bool { return strings.Contains(out.String(), "Librarian:") })
	close(events)
	<-done
	feed := out.String()
	want := "📚 refused 4 tool call(s) not in librarian.allowed_tools: Bash(tracker list | head -5), mcp__chat__search_users, Read(/etc/hosts), …"
	if !strings.Contains(feed, want) {
		t.Errorf("feed missing %q:\n%s", want, feed)
	}
	if p := r.prompt(0); !strings.Contains(p, "- Bash(tracker get:*)") {
		t.Errorf("the librarian prompt must list its tools:\n%s", p)
	}
}

// Harness errors already start with "<engine> failed:"; the feed must not say it twice.
func TestRunFailureLabelOnce(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) {
			return "", errors.New("fake failed: exit status 1: ERROR: stream disconnected")
		},
	}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, Out: out}, events)
	}()
	events <- line("me", twelveWords)
	waitUntil(t, "failure line", func() bool { return strings.Contains(out.String(), "⚠ ") })
	close(events)
	<-done
	feed := out.String()
	if !strings.Contains(feed, "⚠ fake failed: exit status 1: ERROR: stream disconnected; next try in") || strings.Contains(feed, "fake failed: fake failed") {
		t.Errorf("feed:\n%s", feed)
	}
}

// Mute events log each change, drop the muted side's words in progress and
// show the muted side and the keys on the status line.
func TestRunMuteEvents(t *testing.T) {
	fastTimers(t)
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: (&fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`)}}).think,
			Out: out, Interactive: true, MuteHint: "m/s"}, events)
	}()
	waitUntil(t, "status line", func() bool { return strings.Contains(out.String(), "m/s: mute · Ctrl+C: stop") })
	events <- Event{Kind: PartialEvent, Speaker: "me", Text: "half a sentence"}
	events <- Event{Kind: MuteEvent, Speaker: "me", Muted: true}
	waitUntil(t, "muted status", func() bool { return strings.Contains(out.String(), "me (muted)") })
	// The live session lags behind the audio: words said before the key press
	// arrive while muted, and are dropped like the rest.
	events <- Event{Kind: PartialEvent, Speaker: "me", Text: "late partial"}
	events <- line("me", "a late committed line from before the mute")
	events <- line("others", "the other side still counts")
	events <- Event{Kind: MuteEvent, Speaker: "me", Muted: false}
	events <- line("me", "back after the unmute")
	close(events)
	<-done
	feed := out.String()
	for _, bad := range []string{"late partial", "a late committed line"} {
		if strings.Contains(feed, bad) {
			t.Errorf("text from a muted side must be dropped (%q):\n%q", bad, feed)
		}
	}
	for _, want := range []string{"others the other side still counts", "me     back after the unmute"} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%q", want, feed)
		}
	}
	for _, want := range []string{"🔇 me muted at 00:00:00", "🔊 me unmuted at 00:00:00"} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%q", want, feed)
		}
	}
	muted := feed[strings.Index(feed, "🔇 me muted"):]
	if end := strings.Index(muted, "🔊"); end > 0 {
		muted = muted[:end]
	}
	if strings.Contains(muted, "half a sentence") {
		t.Errorf("a muted side's words in progress must not stay on the status line:\n%q", muted)
	}
}

// Without mute keys (not a terminal, or not Linux), the status line keeps its usual hint.
func TestRunStatusWithoutMuteKeys(t *testing.T) {
	fastTimers(t)
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: (&fakeThink{}).think, Out: out, Interactive: true}, events)
	}()
	waitUntil(t, "status line", func() bool { return strings.Contains(out.String(), "(Ctrl+C to stop)") })
	close(events)
	<-done
	if strings.Contains(out.String(), "mute") {
		t.Errorf("no mute hint without keys:\n%q", out.String())
	}
}

// r asks for a recap of the last minutes: one model call, shown in the feed.
func TestRunRecap(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){
		answer(`{"recap":["Sam owns the rollback.","Ship on Monday\u001b[2J."]}`),
	}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, Out: out, RecapWindow: 5 * time.Minute}, events)
	}()
	events <- line("others", "Sam owns the rollback")
	events <- Event{Kind: RecapEvent}
	waitUntil(t, "recap", func() bool { return strings.Contains(out.String(), "Ship on Monday") })
	close(events)
	<-done
	instr, tr := f.call(0)
	if !strings.Contains(instr, `"recap"`) || !strings.Contains(tr, "others: Sam owns the rollback") {
		t.Errorf("recap prompt:\n%s\n%s", instr, tr)
	}
	feed := out.String()
	for _, want := range []string{"📝 00:00:00 Recap of the last 5 min:", "  • Sam owns the rollback.", "  • Ship on Monday[2J."} {
		if !strings.Contains(feed, want) {
			t.Errorf("feed missing %q:\n%q", want, feed)
		}
	}
}

// A recap asked during an update runs right after it, whatever the interval;
// a second press meanwhile is not queued twice.
func TestRunRecapWaitsForTheUpdate(t *testing.T) {
	fastTimers(t)
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) { <-release; return `{"summary":"s"}`, nil },
		answer(`{"recap":["The rollback is owned."]}`),
	}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, Out: out, RecapWindow: time.Minute}, events)
	}()
	events <- line("others", twelveWords)
	waitUntil(t, "update in flight", func() bool { return f.count() == 1 })
	events <- Event{Kind: RecapEvent}
	events <- Event{Kind: RecapEvent}
	waitUntil(t, "second press noted", func() bool { return strings.Contains(out.String(), "a recap is already on its way") })
	close(release)
	waitUntil(t, "recap", func() bool { return strings.Contains(out.String(), "The rollback is owned.") })
	close(events)
	<-done
	if n := f.count(); n != 2 {
		t.Errorf("model calls = %d, want the update then one recap", n)
	}
	if !strings.Contains(out.String(), "Recap of the last 1 min:") {
		t.Errorf("feed:\n%s", out.String())
	}
}

func TestRunRecapNothingSaidOrFailed(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){func() (string, error) { return "", errors.New("fake failed: boom") }}}
	out := &syncBuffer{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, Out: out, RecapWindow: 90 * time.Second}, events)
	}()
	events <- Event{Kind: RecapEvent}
	waitUntil(t, "nothing said", func() bool { return strings.Contains(out.String(), "📝 nothing was said in the last 1m30s") })
	if f.count() != 0 {
		t.Error("no model call without lines")
	}
	events <- line("me", "hello")
	events <- Event{Kind: RecapEvent}
	waitUntil(t, "failure", func() bool { return strings.Contains(out.String(), "⚠ recap failed: boom") })
	close(events)
	<-done
}

func TestRecapPrompt(t *testing.T) {
	lines := []Line{
		{Speaker: "others", At: time.Minute, Text: "old talk"},
		{Speaker: "me", At: 9 * time.Minute, Text: "recent talk"},
	}
	instr, tr := RecapPrompt(Memory{Summary: "We plan the release."}, lines, 10*time.Minute, 5*time.Minute)
	if strings.Contains(tr, "old talk") || !strings.Contains(tr, "me: recent talk") {
		t.Errorf("transcript must hold only the window:\n%s", tr)
	}
	for _, want := range []string{"untrusted", "Summary: We plan the release.", `{"recap": [`, "last 5 min", "language of the meeting"} {
		if !strings.Contains(instr, want) {
			t.Errorf("instructions missing %q:\n%s", want, instr)
		}
	}
}

func TestRunInputEventsAreHandled(t *testing.T) {
	fastTimers(t)
	out := &syncBuffer{}
	events := make(chan Event)
	input := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: (&fakeThink{}).think, Out: out, Input: input}, events)
	}()
	input <- Event{Kind: MuteEvent, Speaker: "me", Muted: true}
	waitUntil(t, "mute notice", func() bool { return strings.Contains(out.String(), "🔇 me muted at") })
	events <- line("me", twelveWords)
	close(input) // a closed Input does not end the run
	events <- line("others", "still running")
	waitUntil(t, "later line", func() bool { return strings.Contains(out.String(), "still running") })
	close(events)
	<-done
	if strings.Contains(out.String(), twelveWords) {
		t.Errorf("a line from the muted side must be dropped:\n%s", out.String())
	}
}

func TestRunRecapCustomWindow(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){answer(`{"recap":["Sam owns the rollback."]}`)}}
	out := &syncBuffer{}
	events := make(chan Event)
	input := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, Out: out, Input: input, RecapWindow: time.Minute}, events)
	}()
	events <- line("others", "Sam owns the rollback")
	input <- Event{Kind: RecapEvent, Window: 10 * time.Minute}
	waitUntil(t, "recap", func() bool { return strings.Contains(out.String(), "Sam owns the rollback.") })
	close(events)
	<-done
	instr, _ := f.call(0)
	if want := "the last " + WindowLabel(10*time.Minute); !strings.Contains(instr, want) {
		t.Errorf("recap instructions miss %q:\n%s", want, instr)
	}
	if !strings.Contains(out.String(), "Recap of the last 10 min") {
		t.Errorf("feed:\n%s", out.String())
	}
}

func TestRunFinalCallback(t *testing.T) {
	fastTimers(t)
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"Final."}`)}}
	notes := filepath.Join(t.TempDir(), "notes.md")
	events := make(chan Event)
	var mu sync.Mutex
	var calls int
	var gotMem Memory
	var gotNotes string
	var written bool
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, Out: &syncBuffer{}, Notes: notes,
			Final: func(m Memory, n string, w bool) {
				mu.Lock()
				calls, gotMem, gotNotes, written = calls+1, m, n, w
				mu.Unlock()
			}}, events)
	}()
	events <- line("me", twelveWords+" "+twelveWords+" "+twelveWords)
	close(events)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || gotNotes != notes || gotMem.Summary != "Final." || !written {
		t.Errorf("Final calls=%d notes=%q summary=%q written=%v", calls, gotNotes, gotMem.Summary, written)
	}
}

func hasItem(v *recordingView, ok func(Item) bool) bool {
	for _, it := range v.Items() {
		if ok(it) {
			return true
		}
	}
	return false
}

func startQuestionRun(t *testing.T, f *fakeThink, v *recordingView) (chan Event, chan Event, chan error) {
	t.Helper()
	fastTimers(t)
	events, input := make(chan Event), make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, View: v, Input: input}, events)
	}()
	return events, input, done
}

func TestRunQuestionRunsInParallel(t *testing.T) {
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) { <-release; return `{"summary":"s"}`, nil },
		answer(`{"answer":"Friday.","source":"research"}`),
	}}
	v := &recordingView{}
	events, input, done := startQuestionRun(t, f, v)
	events <- line("others", twelveWords)
	waitUntil(t, "update in flight", func() bool { return f.count() == 1 })
	input <- Event{Kind: QuestionEvent, Text: "what did Alex say about the deadline?"}
	waitUntil(t, "second call", func() bool { return f.count() == 2 })
	waitUntil(t, "answer", func() bool { return hasItem(v, func(it Item) bool { return it.Badge == "ANSWER" }) })
	f.mu.Lock()
	seen := f.maxSeen
	f.mu.Unlock()
	if seen != 2 {
		t.Errorf("maxSeen = %d, want 2", seen)
	}
	var q, a = -1, -1
	for i, it := range v.Items() {
		if it.Kind == ItemTranscript && it.Speaker == "you" && it.Text == "what did Alex say about the deadline?" {
			q = i
		}
		if it.Kind == ItemAssist && it.Badge == "ANSWER" && strings.HasPrefix(it.Plain, "💬 ") {
			a = i
		}
	}
	if q < 0 || a < q {
		t.Errorf("question at %d, answer at %d:\n%+v", q, a, v.Items())
	}
	close(release)
	close(events)
	<-done
}

func TestRunSecondQuestionWaits(t *testing.T) {
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) { <-release; return `{"answer":"one","source":"general"}`, nil },
		answer(`{"answer":"two","source":"general"}`),
	}}
	v := &recordingView{}
	events, input, done := startQuestionRun(t, f, v)
	input <- Event{Kind: QuestionEvent, Text: "first?"}
	waitUntil(t, "first call", func() bool { return f.count() == 1 })
	input <- Event{Kind: QuestionEvent, Text: "second?"}
	time.Sleep(50 * time.Millisecond) // several ticks: the second must not start
	if n := f.count(); n != 1 {
		t.Fatalf("calls = %d while the first question is in flight", n)
	}
	close(release)
	waitUntil(t, "second answer", func() bool { return hasItem(v, func(it Item) bool { return it.Text == "two" }) })
	if f.maxSeen != 1 {
		t.Errorf("maxSeen = %d, want 1", f.maxSeen)
	}
	close(events)
	<-done
}

func TestRunQuestionGeneralIsLabelled(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"answer":"Service level objective","source":"general"}`)}}
	v := &recordingView{}
	notes := filepath.Join(t.TempDir(), "notes.md")
	fastTimers(t)
	events, input := make(chan Event), make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, View: v, Input: input, Notes: notes}, events)
	}()
	input <- Event{Kind: QuestionEvent, Text: "What is an SLO?"}
	waitUntil(t, "answer", func() bool {
		return hasItem(v, func(it Item) bool { return it.Badge == "ANSWER" && it.Note == "general knowledge" })
	})
	waitUntil(t, "notes", func() bool {
		b, _ := os.ReadFile(notes)
		return strings.Contains(string(b), "## Questions") && strings.Contains(string(b), "Q: What is an SLO?")
	})
	close(events)
	<-done
}

func TestRunQuestionFailureAndAfterEnd(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){func() (string, error) { return "", errors.New("fake failed: boom") }}}
	v := &recordingView{}
	events, input, done := startQuestionRun(t, f, v)
	input <- Event{Kind: QuestionEvent, Text: "why?"}
	waitUntil(t, "failure", func() bool {
		return hasItem(v, func(it Item) bool { return it.Kind == ItemNotice && it.Text == "⚠ question failed: boom" })
	})
	close(events)
	<-done
}

func TestRunQuestionAfterEndRefused(t *testing.T) {
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){func() (string, error) { <-release; return `{"summary":"s"}`, nil }}}
	v := &recordingView{}
	events, input, done := startQuestionRun(t, f, v)
	events <- line("others", twelveWords)
	waitUntil(t, "update in flight", func() bool { return f.count() == 1 })
	close(events)
	waitUntil(t, "ending", func() bool {
		return hasItem(v, func(it Item) bool { return strings.Contains(it.Text, "finishing the current update") })
	})
	input <- Event{Kind: QuestionEvent, Text: "too late?"}
	input <- Event{Kind: RecapEvent}
	input <- Event{Kind: MuteEvent, Speaker: "me", Muted: true}
	input <- Event{Kind: FixEvent, Fix: FixRequest{Op: "add", Wrong: "cash", Right: "cache"}}
	close(release)
	<-done
	n := 0
	for _, it := range v.Items() {
		if it.Kind == ItemInfo && strings.Contains(it.Text, "the meeting is over") {
			n++
		}
	}
	if n != 3 {
		t.Errorf("refusals = %d, want 3 (question, recap and fix)", n)
	}
	if f.count() != 1 {
		t.Errorf("model calls = %d, want only the update", f.count())
	}
}

// A question in flight when the meeting ends is answered into the feed and
// the notes; one still queued is counted as unanswered, never started.
func TestRunEndWaitsForQuestionInFlight(t *testing.T) {
	fastTimers(t)
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		func() (string, error) { <-release; return `{"answer":"Friday.","source":"general"}`, nil },
	}}
	v := &recordingView{}
	notes := filepath.Join(t.TempDir(), "notes.md")
	events, input := make(chan Event), make(chan Event)
	var mu sync.Mutex
	finals, written := 0, true
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, View: v, Input: input, Notes: notes,
			Final: func(_ Memory, _ string, w bool) { mu.Lock(); finals, written = finals+1, w; mu.Unlock() }}, events)
	}()
	input <- Event{Kind: QuestionEvent, Text: "when do we ship?"}
	waitUntil(t, "question call", func() bool { return f.count() == 1 })
	input <- Event{Kind: QuestionEvent, Text: "who owns the runbook?"}
	close(events)
	waitUntil(t, "ending", func() bool {
		return hasItem(v, func(it Item) bool { return strings.Contains(it.Text, "finishing the current answer") })
	})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !hasItem(v, func(it Item) bool { return it.Badge == "ANSWER" && it.Text == "Friday." }) {
		t.Errorf("answer missing from the feed: %+v", v.Items())
	}
	if !hasItem(v, func(it Item) bool { return it.Kind == ItemInfo && it.Text == "1 question(s) left unanswered" }) {
		t.Errorf("no unanswered line: %+v", v.Items())
	}
	b, _ := os.ReadFile(notes)
	if !strings.Contains(string(b), "## Questions") || !strings.Contains(string(b), "Q: when do we ship? — A: Friday.") {
		t.Errorf("notes miss the answer:\n%s", b)
	}
	if f.count() != 1 {
		t.Errorf("model calls = %d, want only the first question", f.count())
	}
	mu.Lock()
	defer mu.Unlock()
	if finals != 1 || written {
		t.Errorf("Final calls=%d written=%v, want 1 call without final notes", finals, written)
	}
}

// The status keeps ticking while the final notes are written.
func TestRunEndRefreshesStatus(t *testing.T) {
	fastTimers(t)
	release := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){func() (string, error) { <-release; return `{"summary":"Final."}`, nil }}}
	v := &recordingView{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: f.think, View: v}, events)
	}()
	events <- line("me", twelveWords+" "+twelveWords+" "+twelveWords)
	close(events)
	waitUntil(t, "final call", func() bool { return f.count() == 1 })
	waitUntil(t, "a thinking status after the final call started", func() bool {
		v.mu.Lock()
		defer v.mu.Unlock()
		last := v.statuses[len(v.statuses)-1]
		return last.State == "thinking" && last.Busy >= 20*time.Millisecond
	})
	close(release)
	<-done
}

func TestRunWritesTranscript(t *testing.T) {
	fastTimers(t)
	path := filepath.Join(t.TempDir(), "t-transcript.md")
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Header: "HDR", Transcript: path, Out: &syncBuffer{},
			Think: (&fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`)}}).think}, events)
	}()
	events <- line("others", "hello from them")
	events <- Event{Kind: MuteEvent, Speaker: "me", Muted: true}
	events <- line("me", "muted words")
	events <- Event{Kind: MuteEvent, Speaker: "me", Muted: false}
	events <- line("me", "hello from me")
	close(events)
	<-done
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{"# Transcript\n\nHDR", "] others: hello from them", "] me: hello from me"} {
		if !strings.Contains(s, want) {
			t.Errorf("transcript missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "muted words") {
		t.Errorf("muted line must not be saved:\n%s", s)
	}
}

// startFixRun runs with a recording view, notes and a transcript file.
func startFixRun(t *testing.T, f *fakeThink, v *recordingView) (events, input chan Event, done chan error, notes, transcript string) {
	t.Helper()
	fastTimers(t)
	dir := t.TempDir()
	notes, transcript = filepath.Join(dir, "notes.md"), filepath.Join(dir, "notes-transcript.md")
	events, input = make(chan Event), make(chan Event)
	done = make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: 20 * time.Millisecond, Think: f.think, View: v, Input: input,
			Notes: notes, Transcript: transcript}, events)
	}()
	return events, input, done, notes, transcript
}

func fixEvent(op, wrong, right string, n int) Event {
	return Event{Kind: FixEvent, Fix: FixRequest{Op: op, Wrong: wrong, Right: right, N: n}}
}

func (v *recordingView) Boards() []Board {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Board(nil), v.boards...)
}

func (v *recordingView) Statuses() []Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Status(nil), v.statuses...)
}

func hasInfo(v *recordingView, text string) bool {
	return hasItem(v, func(it Item) bool { return it.Kind == ItemInfo && it.Text == text })
}

func TestRunFixRewritesEverything(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"cash pass"}`), answer(`{"summary":"cache pass, more"}`)}}
	v := &recordingView{}
	events, input, done, notes, transcript := startFixRun(t, f, v)
	events <- line("others", "the cash work")
	events <- line("me", "cash again")
	waitUntil(t, "first update", func() bool {
		b := v.Boards()
		return len(b) > 0 && b[len(b)-1].Summary == "cash pass"
	})
	input <- fixEvent("add", "cash", "cache", 0)
	events <- line("me", "more cash")
	events <- Event{Kind: PartialEvent, Speaker: "others", Text: "cash now"}
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	instr, tr := f.call(1)
	if !strings.Contains(tr, "cache") || strings.Contains(tr, "cash") {
		t.Errorf("the update must see the fixed transcript:\n%s", tr)
	}
	if !strings.Contains(instr, "- cash → cache") {
		t.Errorf("the update must be told about the fix:\n%s", instr)
	}
	var fixed *Board
	for _, b := range v.Boards() {
		if len(b.Fixes) > 0 {
			fixed = &b
			break
		}
	}
	if fixed == nil || !strings.Contains(fixed.Summary, "cache pass") || !slices.Equal(fixed.Fixes, []string{"cash → cache"}) {
		t.Errorf("board after the fix = %+v", fixed)
	}
	if !hasItem(v, func(it Item) bool {
		return it.Kind == ItemAssist && it.Badge == "FIX" && it.Text == "cash → cache" && it.Note == "2 lines" &&
			strings.HasPrefix(it.Plain, "✏️ ") && strings.HasSuffix(it.Plain, " Fix: cash → cache (2 lines)")
	}) {
		t.Errorf("no FIX item:\n%+v", v.Items())
	}
	if !hasItem(v, func(it Item) bool { return it.Kind == ItemTranscript && it.Text == "more cache" }) {
		t.Error("a new line must be fixed before it is shown")
	}
	waitUntil(t, "partial shown", func() bool {
		st := v.Statuses()
		return st[len(st)-1].Partial == "cache now"
	})
	for _, st := range v.Statuses() {
		if strings.Contains(st.Partial, "cash") {
			t.Errorf("partial shown unfixed: %q", st.Partial)
		}
	}
	close(events)
	<-done
	if b := v.Boards()[len(v.Boards())-1]; !slices.Equal(b.Fixes, []string{"cash → cache"}) || !strings.Contains(b.Summary, "cache pass") {
		t.Errorf("last board = %+v", b)
	}
	tb, _ := os.ReadFile(transcript)
	if strings.Contains(string(tb), "cash") || !strings.Contains(string(tb), "others: the cache work") || !strings.Contains(string(tb), "me: more cache") {
		t.Errorf("transcript file:\n%s", tb)
	}
	nb, _ := os.ReadFile(notes)
	if !strings.Contains(string(nb), "## Fixes\n\n- `cash` → `cache`") {
		t.Errorf("notes:\n%s", nb)
	}
}

// An update in flight when a fix arrives was asked without it: its reply is
// fixed before it is applied, so the memory does not go back to the misheard
// word, the fields it leaves out are not fixed twice, and a decision it
// repeats with the old word is not shown again.
func TestRunFixDuringUpdate(t *testing.T) {
	rel1, rel2 := make(chan struct{}), make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){
		answer(`{"summary":"film plan","decisions":["ship the film"]}`),
		func() (string, error) { <-rel1; return `{"summary":"film pass"}`, nil },
		func() (string, error) {
			<-rel2
			return `{"summary":"short film done","decisions":["ship the short film"]}`, nil
		},
	}}
	v := &recordingView{}
	events, input, done, _, _ := startFixRun(t, f, v)
	lastBoard := func() Board { b := v.Boards(); return b[len(b)-1] }
	events <- line("others", "first part")
	waitUntil(t, "first update", func() bool { return lastBoard().Summary == "film plan" })
	events <- line("others", "second part")
	waitUntil(t, "second update in flight", func() bool { return f.count() == 2 })
	input <- fixEvent("add", "film", "short film", 0)
	close(rel1)
	waitUntil(t, "second update", func() bool { return lastBoard().Summary == "short film pass" })
	if d := lastBoard().Decisions; !slices.Equal(d, []string{"ship the short film"}) {
		t.Errorf("decisions after a summary-only reply = %q", d)
	}
	events <- line("others", "third part")
	waitUntil(t, "third update in flight", func() bool { return f.count() == 3 })
	input <- fixEvent("add", "ship", "send", 0)
	close(rel2)
	waitUntil(t, "third update", func() bool { return lastBoard().Summary == "short film done" })
	if d := lastBoard().Decisions; !slices.Equal(d, []string{"send the short film"}) {
		t.Errorf("decisions after a reply with the old word = %q", d)
	}
	n := 0
	for _, it := range v.Items() {
		if it.Badge == "DECISION" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("DECISION items = %d, want 1:\n%+v", n, v.Items())
	}
	close(events)
	<-done
}

func TestRunFixReportsCountAndRm(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`)}}
	v := &recordingView{}
	events, input, done, _, _ := startFixRun(t, f, v)
	for _, l := range []string{"it is done", "is it", "what is that", "nothing here"} {
		events <- line("others", l)
	}
	input <- fixEvent("add", "is", "this", 0)
	waitUntil(t, "fix", func() bool { return hasItem(v, func(it Item) bool { return it.Badge == "FIX" }) })
	if !hasItem(v, func(it Item) bool { return it.Badge == "FIX" && it.Note == "3 lines" }) {
		t.Errorf("the fix must count the lines it changed:\n%+v", v.Items())
	}
	input <- fixEvent("list", "", "", 0)
	waitUntil(t, "list", func() bool { return hasInfo(v, "1. is → this") })
	input <- fixEvent("rm", "", "", 1)
	waitUntil(t, "rm", func() bool {
		return hasItem(v, func(it Item) bool { return it.Kind == ItemInfo && strings.Contains(it.Text, "past lines keep it") })
	})
	if !hasInfo(v, "fix 1 removed: is → this (past lines keep it)") {
		t.Errorf("rm info:\n%+v", v.Items())
	}
	events <- line("me", "it is late")
	waitUntil(t, "new line", func() bool {
		return hasItem(v, func(it Item) bool { return it.Kind == ItemTranscript && strings.HasSuffix(it.Text, " late") })
	})
	if !hasItem(v, func(it Item) bool { return it.Kind == ItemTranscript && it.Text == "it is late" }) {
		t.Error("a removed fix must not apply to new lines")
	}
	input <- fixEvent("list", "", "", 0)
	waitUntil(t, "empty list", func() bool { return hasInfo(v, "no fixes yet") })
	close(events)
	<-done
}

func TestRunFixAnswerWithoutPending(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`)}}
	v := &recordingView{}
	events, input, done, _, _ := startFixRun(t, f, v)
	input <- fixEvent("yes", "", "", 0)
	input <- fixEvent("no", "", "", 3)
	input <- fixEvent("rm", "", "", 9)
	input <- fixEvent("list", "", "", 0)
	waitUntil(t, "list", func() bool { return hasInfo(v, "no fixes yet") })
	for _, want := range []string{"no suggested fix to accept", "no suggested fix 3", "no fix 9: type /fix to list them"} {
		if !hasInfo(v, want) {
			t.Errorf("no info %q:\n%+v", want, v.Items())
		}
	}
	for _, b := range v.Boards() {
		if len(b.Fixes) > 0 {
			t.Errorf("board changed: %+v", b)
		}
	}
	close(events)
	<-done
}

const guessCash = `{"summary":"cash pass","fixes":[{"wrong":"cash","right":"cache","why":"caching discussion"}]}`

func isGuess(it Item) bool { return it.Kind == ItemAssist && it.Badge == "FIX?" }

func countItems(v *recordingView, ok func(Item) bool) int {
	n := 0
	for _, it := range v.Items() {
		if ok(it) {
			n++
		}
	}
	return n
}

// startGuess runs until the first update proposes cash → cache.
func startGuess(t *testing.T, f *fakeThink, v *recordingView) (events, input chan Event, done chan error, notes, transcript string) {
	t.Helper()
	events, input, done, notes, transcript = startFixRun(t, f, v)
	events <- line("others", "the cash work")
	waitUntil(t, "guess", func() bool { return hasItem(v, isGuess) })
	return events, input, done, notes, transcript
}

func TestGuessedFixShownNotApplied(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(guessCash), answer(`{"summary":"s2"}`)}}
	v := &recordingView{}
	events, _, done, _, _ := startGuess(t, f, v)
	if !hasItem(v, func(it Item) bool {
		return isGuess(it) && it.Text == "cash → cache" && it.Note == "caching discussion · /fix yes 1" &&
			strings.HasPrefix(it.Plain, "✏️ ") && strings.HasSuffix(it.Plain, " Fix? cash → cache — caching discussion (/fix yes 1)")
	}) {
		t.Errorf("no FIX? item:\n%+v", v.Items())
	}
	if b := v.Boards()[len(v.Boards())-1]; !slices.Equal(b.Fixes, []string{"cash → cache ?"}) || b.Summary != "cash pass" {
		t.Errorf("board = %+v", b)
	}
	events <- line("me", "more cash")
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	if _, tr := f.call(1); !strings.Contains(tr, "the cash work") || !strings.Contains(tr, "more cash") {
		t.Errorf("a guess must not change the transcript:\n%s", tr)
	}
	close(events)
	<-done
}

func TestGuessedFixAccepted(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(guessCash), answer(`{"summary":"cache pass, more"}`)}}
	v := &recordingView{}
	events, input, done, notes, transcript := startGuess(t, f, v)
	input <- fixEvent("yes", "", "", 0)
	waitUntil(t, "fix", func() bool { return hasItem(v, func(it Item) bool { return it.Badge == "FIX" }) })
	if !hasItem(v, func(it Item) bool { return it.Badge == "FIX" && it.Text == "cash → cache" && it.Note == "1 line" }) {
		t.Errorf("no FIX item:\n%+v", v.Items())
	}
	if b := v.Boards()[len(v.Boards())-1]; !slices.Equal(b.Fixes, []string{"cash → cache"}) || b.Summary != "cache pass" {
		t.Errorf("board = %+v", b)
	}
	events <- line("me", "more cash")
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	if _, tr := f.call(1); strings.Contains(tr, "cash") || !strings.Contains(tr, "the cache work") || !strings.Contains(tr, "more cache") {
		t.Errorf("an accepted guess works like a typed fix:\n%s", tr)
	}
	input <- fixEvent("yes", "", "", 1)
	waitUntil(t, "answered", func() bool { return hasInfo(v, "fix 1 is already answered") })
	input <- fixEvent("yes", "", "", 0)
	waitUntil(t, "none pending", func() bool { return hasInfo(v, "no suggested fix to accept") })
	close(events)
	<-done
	tb, _ := os.ReadFile(transcript)
	if strings.Contains(string(tb), "cash") || !strings.Contains(string(tb), "others: the cache work") {
		t.Errorf("transcript file:\n%s", tb)
	}
	nb, _ := os.ReadFile(notes)
	if !strings.Contains(string(nb), "## Fixes\n\n- `cash` → `cache` _(suggested)_") {
		t.Errorf("notes:\n%s", nb)
	}
}

func TestGuessedFixRejectedNotReproposed(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(guessCash)}}
	v := &recordingView{}
	events, input, done, _, _ := startGuess(t, f, v)
	input <- fixEvent("no", "", "", 0)
	waitUntil(t, "rejected", func() bool { return hasInfo(v, "fix 1 rejected") })
	if b := v.Boards()[len(v.Boards())-1]; len(b.Fixes) != 0 {
		t.Errorf("a rejected guess leaves the board: %+v", b)
	}
	events <- line("me", "more cash")
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	if instr, _ := f.call(1); !strings.Contains(instr, "Fixes you suggested earlier (your own suggestions, not the user's corrections; never propose these again):\n- cash → cache\n") {
		t.Errorf("the update must be told about the rejected guess:\n%s", instr)
	}
	events <- line("me", "and cash")
	waitUntil(t, "third update", func() bool { return f.count() == 3 })
	input <- fixEvent("no", "", "", 1)
	waitUntil(t, "answered", func() bool { return hasInfo(v, "fix 1 is already answered") })
	if n := countItems(v, isGuess); n != 1 {
		t.Errorf("FIX? items = %d, want 1:\n%+v", n, v.Items())
	}
	close(events)
	<-done
}

// Guesses for a word not said, for a fix in force, and a second guess in one
// reply are dropped; the reason is cleaned.
func TestGuessedFixFiltered(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){
		answer(`{"summary":"one","fixes":[{"wrong":"cachet","right":"cache","why":"x"},{"wrong":"film","right":"short film","why":"x"}]}`),
		answer(`{"summary":"two","fixes":[{"wrong":"cash","right":"cache","why":"x\u001b[2Jy"},{"wrong":"dark mood","right":"dark mode","why":"x"}]}`),
	}}
	v := &recordingView{}
	events, input, done, _, _ := startFixRun(t, f, v)
	lastBoard := func() Board { b := v.Boards(); return b[len(b)-1] }
	input <- fixEvent("add", "film", "short film", 0)
	events <- line("others", "the film and the cash work")
	waitUntil(t, "first update", func() bool { return lastBoard().Summary == "one" })
	if hasItem(v, isGuess) {
		t.Errorf("filtered guesses shown:\n%+v", v.Items())
	}
	events <- line("others", "in dark mood")
	waitUntil(t, "second update", func() bool { return lastBoard().Summary == "two" })
	if n := countItems(v, isGuess); n != 1 {
		t.Errorf("FIX? items = %d, want 1:\n%+v", n, v.Items())
	}
	if !hasItem(v, func(it Item) bool {
		return isGuess(it) && it.Text == "cash → cache" && it.Note == "x[2Jy · /fix yes 1"
	}) {
		t.Errorf("the first guess, cleaned:\n%+v", v.Items())
	}
	if b := lastBoard(); !slices.Equal(b.Fixes, []string{"film → short film", "cash → cache ?"}) {
		t.Errorf("board = %+v", b)
	}
	close(events)
	<-done
}

// A guess the user types meanwhile, in another case, is in force already:
// accepting it must not add it twice, or film → short film would double up.
func TestGuessedFixAlreadyInForce(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){
		answer(`{"summary":"one","fixes":[{"wrong":"film","right":"short film","why":"x"}]}`),
		answer(`{"summary":"two"}`),
	}}
	v := &recordingView{}
	events, input, done, _, _ := startFixRun(t, f, v)
	events <- line("others", "the film work")
	waitUntil(t, "guess", func() bool { return hasItem(v, isGuess) })
	input <- fixEvent("add", "Film", "short film", 0)
	input <- fixEvent("yes", "", "", 0)
	waitUntil(t, "in force", func() bool { return hasInfo(v, "fix 1 is already in force") })
	if n := countItems(v, func(it Item) bool { return it.Badge == "FIX" }); n != 1 {
		t.Errorf("FIX items = %d, want 1:\n%+v", n, v.Items())
	}
	if b := v.Boards()[len(v.Boards())-1]; !slices.Equal(b.Fixes, []string{"Film → short film"}) {
		t.Errorf("board = %+v", b)
	}
	events <- line("me", "a film now")
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	if _, tr := f.call(1); !strings.Contains(tr, "others: the short film work") || !strings.Contains(tr, "me: a short film now") ||
		strings.Contains(tr, "short short") {
		t.Errorf("the fix must apply once:\n%s", tr)
	}
	close(events)
	<-done
}
