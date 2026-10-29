package assistant

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goscribe/internal/webpage"
)

func addEvent(op, target string) Event {
	return Event{Kind: AddEvent, Add: AddRequest{Op: op, Target: target}}
}

// startAddRun is startBriefRun with a context and the notes path chosen by
// the test.
func startAddRun(t *testing.T, f *fakeThink, v *recordingView, interval time.Duration, context string) (events, input chan Event, done chan error, notes string) {
	t.Helper()
	fastTimers(t)
	notes = filepath.Join(t.TempDir(), "notes.md")
	events, input = make(chan Event), make(chan Event)
	done = make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: interval, Think: f.think, View: v, Input: input, Notes: notes,
			Transcript: TranscriptPath(notes), Context: context}, events)
	}()
	return events, input, done, notes
}

func countInfo(v *recordingView, text string) int {
	n := 0
	for _, it := range v.Items() {
		if it.Kind == ItemInfo && it.Text == text {
			n++
		}
	}
	return n
}

func TestRunAddLocalFile(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`), answer(`{"summary":"s2"}`)}}
	v := &recordingView{}
	events, input, done, notes := startAddRun(t, f, v, time.Hour, "")
	events <- line("others", twelveWords)
	waitUntil(t, "first update", func() bool { return f.count() == 1 })
	file := filepath.Join(t.TempDir(), "pricing.md")
	mustWrite(t, file, []byte("# Pricing\nPro costs 20 EUR.\n"), 0644)
	input <- addEvent("add", file)
	waitUntil(t, "source item", func() bool {
		return hasItem(v, func(it Item) bool {
			return it.Badge == "SOURCE" && it.Text == "pricing.md" && it.Note == "1 KB" &&
				strings.HasPrefix(it.Plain, "📎 ") && strings.HasSuffix(it.Plain, " Added source: pricing.md (1 KB)")
		})
	})
	if b := lastBoardOf(v); len(b.Sources) == 0 || b.Sources[len(b.Sources)-1] != "pricing.md" {
		t.Errorf("board sources = %q", b.Sources)
	}
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	instr, tr := f.call(1)
	ctx := instr[strings.Index(instr, "<context>"):strings.Index(instr, "</context>")]
	if !strings.Contains(ctx, "Pro costs 20 EUR.") || !strings.Contains(instr, "The user added a source since the last update ("+file+")") {
		t.Errorf("instructions:\n%s", instr)
	}
	if strings.Contains(tr, "Pro costs") {
		t.Errorf("doc in the transcript:\n%s", tr)
	}
	if first, _ := f.call(0); strings.Contains(first, "Pro costs") {
		t.Errorf("first update has the doc")
	}
	nb, _ := os.ReadFile(notes)
	if !strings.Contains(string(nb), "## Sources\n\n- [") || !strings.Contains(string(nb), "pricing.md (`"+file+"`), 1 KB") {
		t.Errorf("notes:\n%s", nb)
	}
	close(events)
	<-done
}

func TestRunAddRefusesOwnFiles(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	events, input, done, notes := startAddRun(t, f, v, time.Hour, "")
	mustMkdir(t, SourcesDir(notes))
	mustWrite(t, filepath.Join(SourcesDir(notes), "x.md"), []byte("x"), 0644)
	mustWrite(t, notes, []byte("# Meeting notes\n"), 0644)
	mustWrite(t, TranscriptPath(notes), []byte("# Transcript\n"), 0644)
	rel := filepath.Join(filepath.Dir(notes), ".", "sub", "..", filepath.Base(notes)) // not clean
	for i, target := range []string{notes, rel, TranscriptPath(notes), filepath.Join(SourcesDir(notes), "x.md"), SourcesDir(notes), filepath.Dir(notes)} {
		input <- addEvent("add", target)
		waitUntil(t, "refusal of "+target, func() bool { return countInfo(v, "that is this meeting's own file") == i+1 })
	}
	input <- addEvent("list", "")
	waitUntil(t, "empty list", func() bool { return hasInfo(v, "no sources added yet: /add <path or url> adds one") })
	close(events)
	<-done
}

func TestRunAddBudget(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	events, input, done, _ := startAddRun(t, f, v, time.Hour, strings.Repeat("c", 150<<10))
	file := filepath.Join(t.TempDir(), "big.md")
	mustWrite(t, file, []byte(strings.Repeat("b", 60500)), 0644)
	input <- addEvent("add", file)
	waitUntil(t, "refusal", func() bool {
		return hasInfo(v, "too big: big.md is 60 KB and the context would be 210 KB (limit 200 KB)")
	})
	if hasItem(v, func(it Item) bool { return it.Badge == "SOURCE" }) {
		t.Errorf("a source was added")
	}
	close(events)
	<-done
}

func TestRunAddDuplicateAndList(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	events, input, done, _ := startAddRun(t, f, v, time.Hour, "")
	dir := t.TempDir()
	file := filepath.Join(dir, "a.md")
	mustWrite(t, file, []byte("alpha"), 0644)
	input <- addEvent("add", file)
	waitUntil(t, "added", func() bool { return hasItem(v, func(it Item) bool { return it.Badge == "SOURCE" }) })
	input <- addEvent("add", filepath.Join(dir, ".", "a.md"))
	waitUntil(t, "duplicate", func() bool { return hasInfo(v, "already added: a.md") })
	input <- addEvent("list", "")
	waitUntil(t, "list", func() bool { return hasInfo(v, "1. a.md (1 KB, file)") })
	input <- addEvent("add", filepath.Join(dir, "missing.md"))
	waitUntil(t, "load error", func() bool {
		return hasItem(v, func(it Item) bool {
			return it.Kind == ItemInfo && strings.HasPrefix(it.Text, "cannot add "+filepath.Join(dir, "missing.md")+": ")
		})
	})
	n := 0
	for _, it := range v.Items() {
		if it.Badge == "SOURCE" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d source items", n)
	}
	close(events)
	<-done
}

func TestRunAddBinaryRefused(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	events, input, done, _ := startAddRun(t, f, v, time.Hour, "")
	file := filepath.Join(t.TempDir(), "blob.md")
	mustWrite(t, file, []byte("abc\x00def"), 0644)
	input <- addEvent("add", file)
	waitUntil(t, "refusal", func() bool { return hasInfo(v, "blob.md is not a text file") })
	bad := filepath.Join(t.TempDir(), "latin.md")
	mustWrite(t, bad, []byte("caf\xe9"), 0644)
	input <- addEvent("add", bad)
	waitUntil(t, "refusal 2", func() bool { return hasInfo(v, "latin.md is not a text file") })
	close(events)
	<-done
}

func TestRunAddRestoresNotes(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	events, input, done, _ := startAddRun(t, f, v, time.Hour, "")
	old := filepath.Join(t.TempDir(), "standup.md")
	mustWrite(t, old, []byte(Render(Memory{Briefs: []Brief{{Text: "Sam is the PM"}}}, "h", false, 0)), 0644)
	input <- addEvent("add", old)
	waitUntil(t, "source and restore", func() bool {
		return hasItem(v, func(it Item) bool { return it.Badge == "SOURCE" && it.Text == "standup.md" }) &&
			hasInfo(v, "↺ restored 1 brief from standup.md")
	})
	if b := lastBoardOf(v); len(b.Briefs) != 1 || b.Briefs[0] != "Sam is the PM" {
		t.Errorf("board briefs = %q", b.Briefs)
	}
	// A duplicate add restores nothing more.
	input <- addEvent("add", old)
	waitUntil(t, "duplicate", func() bool { return hasInfo(v, "already added: standup.md") })
	// A notes file with a repeated section is a parse error: still a source, nothing restored.
	bad := filepath.Join(t.TempDir(), "bad.md")
	mustWrite(t, bad, []byte(Render(Memory{Briefs: []Brief{{Text: "a b"}}}, "h", false, 0)+"## Brief\n\n- c d\n"), 0644)
	input <- addEvent("add", bad)
	waitUntil(t, "parse error", func() bool {
		return hasItem(v, func(it Item) bool {
			return it.Kind == ItemInfo && strings.HasPrefix(it.Text, "⚠ bad.md: ") && strings.HasSuffix(it.Text, "; nothing restored")
		})
	})
	if b := lastBoardOf(v); len(b.Briefs) != 1 {
		t.Errorf("board briefs = %q", b.Briefs)
	}
	close(events)
	<-done
}

func TestRunAddAfterEnd(t *testing.T) {
	rel := make(chan struct{})
	f := &fakeThink{replies: []func() (string, error){func() (string, error) { <-rel; return `{"summary":"s"}`, nil }}}
	v := &recordingView{}
	events, input, done, _ := startAddRun(t, f, v, time.Hour, "")
	events <- line("others", twelveWords)
	waitUntil(t, "update in flight", func() bool { return f.count() == 1 })
	close(events)
	input <- addEvent("add", t.TempDir())
	waitUntil(t, "refusal", func() bool { return hasInfo(v, "the meeting is over: sources are closed") })
	close(rel)
	<-done
}

func TestRunAddTooBigFile(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	events, input, done, _ := startAddRun(t, f, v, time.Hour, "")
	dir := t.TempDir()
	file := filepath.Join(dir, "huge.md")
	mustWrite(t, file, []byte(strings.Repeat("h", 250<<10)), 0644)
	input <- addEvent("add", file)
	waitUntil(t, "refusal", func() bool {
		return hasInfo(v, "too big: huge.md is 250 KB and the context would be 250 KB (limit 200 KB)")
	})
	// A folder sums its .md files only.
	mustWrite(t, filepath.Join(dir, "other.txt"), []byte(strings.Repeat("t", 100<<10)), 0644)
	input <- addEvent("add", dir)
	waitUntil(t, "folder refusal", func() bool {
		return hasInfo(v, "too big: "+filepath.Base(dir)+" is 250 KB and the context would be 250 KB (limit 200 KB)")
	})
	close(events)
	<-done
}

// fakePages answers Fetch from a table; a page whose url is in block waits
// for release.
type fakePages struct {
	mu      sync.Mutex
	pages   map[string]webpage.Page
	errs    map[string]error
	block   map[string]chan struct{}
	started int
}

func (p *fakePages) fetch(ctx context.Context, u string) (webpage.Page, error) {
	p.mu.Lock()
	p.started++
	ch := p.block[u]
	p.mu.Unlock()
	if ch != nil {
		<-ch
	}
	if err := p.errs[u]; err != nil {
		return webpage.Page{}, err
	}
	return p.pages[u], nil
}

func startPageRun(t *testing.T, f *fakeThink, v *recordingView, p *fakePages, withSources bool) (events, input chan Event, done chan error, notes string) {
	t.Helper()
	fastTimers(t)
	notes = filepath.Join(t.TempDir(), "notes.md")
	events, input = make(chan Event), make(chan Event)
	done = make(chan error, 1)
	c := Config{Engine: "fake", Interval: time.Hour, Think: f.think, View: v, Input: input, Notes: notes,
		Transcript: TranscriptPath(notes), Fetch: p.fetch}
	if withSources {
		c.SourcesDir = SourcesDir(notes)
	}
	go func() { done <- Run(c, events) }()
	return events, input, done, notes
}

func TestRunAddPage(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`), answer(`{"summary":"s2"}`)}}
	v := &recordingView{}
	p := &fakePages{pages: map[string]webpage.Page{"https://example.com/pricing": {
		Title: "Acme pricing", URL: "https://www.example.com/pricing/", Markdown: "Pro costs 20 EUR."}}}
	events, input, done, notes := startPageRun(t, f, v, p, true)
	events <- line("others", twelveWords)
	waitUntil(t, "first update", func() bool { return f.count() == 1 })
	input <- addEvent("add", "https://example.com/pricing")
	waitUntil(t, "fetching info", func() bool { return hasInfo(v, "📎 fetching https://example.com/pricing…") })
	waitUntil(t, "source item", func() bool {
		return hasItem(v, func(it Item) bool { return it.Badge == "SOURCE" && it.Text == "Acme pricing" })
	})
	waitUntil(t, "second update", func() bool { return f.count() == 2 })
	instr, _ := f.call(1)
	un := instr[strings.Index(instr, "<untrusted_sources>"):strings.Index(instr, "</untrusted_sources>")]
	if !strings.Contains(un, "Pro costs 20 EUR.") {
		t.Errorf("untrusted block:\n%s", un)
	}
	if !strings.Contains(instr, "The user added a source since the last update (https://example.com/pricing)") {
		t.Errorf("change line missing:\n%s", instr)
	}
	copyText, err := os.ReadFile(filepath.Join(SourcesDir(notes), "acme-pricing.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Acme pricing\n\nSource: https://www.example.com/pricing/\n\nPro costs 20 EUR.\n"; string(copyText) != want {
		t.Errorf("copy = %q", copyText)
	}
	close(events)
	<-done
}

func TestRunAddPageLoginWithoutLibrarian(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	u := "https://docs.example.com/d/1"
	p := &fakePages{errs: map[string]error{u: &webpage.LoginError{Reason: "redirected to a sign-in page"}}}
	events, input, done, _ := startPageRun(t, f, v, p, false)
	input <- addEvent("add", u)
	waitUntil(t, "login message", func() bool {
		return hasInfo(v, u+" needs a login (redirected to a sign-in page): goscribe can't read it. Start assist with -librarian and a read-only tool for docs.example.com in librarian.allowed_tools")
	})
	close(events)
	<-done
}

func TestRunAddPageError(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	u := "https://example.com/gone"
	p := &fakePages{errs: map[string]error{u: errors.New("HTTP 404")}}
	events, input, done, _ := startPageRun(t, f, v, p, false)
	input <- addEvent("add", u)
	waitUntil(t, "error", func() bool { return hasInfo(v, "⚠ cannot add "+u+": HTTP 404") })
	close(events)
	<-done
}

func TestRunAddPageSlugCollision(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	a, b := "https://example.com/a", "https://example.com/b"
	p := &fakePages{pages: map[string]webpage.Page{
		a: {Title: "Acme pricing", URL: a, Markdown: "one"},
		b: {Title: "Acme   Pricing!", URL: b, Markdown: "two"},
	}}
	events, input, done, notes := startPageRun(t, f, v, p, true)
	input <- addEvent("add", a)
	waitUntil(t, "first", func() bool { return len(lastBoardOf(v).Sources) == 1 })
	input <- addEvent("add", b)
	waitUntil(t, "second", func() bool { return len(lastBoardOf(v).Sources) == 2 })
	for _, n := range []string{"acme-pricing.md", "acme-pricing-2.md"} {
		data, err := os.ReadFile(filepath.Join(SourcesDir(notes), n))
		if err != nil {
			t.Error(err)
		} else if want := map[string]string{"acme-pricing.md": "one", "acme-pricing-2.md": "two"}[n]; !strings.Contains(string(data), want) {
			t.Errorf("%s = %q", n, data)
		}
	}
	close(events)
	<-done
}

func TestRunAddPageLimitOfThree(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	p := &fakePages{block: map[string]chan struct{}{}}
	rel := make(chan struct{})
	for i := 1; i <= 4; i++ {
		p.block[fmt.Sprintf("https://example.com/%d", i)] = rel
	}
	events, input, done, _ := startPageRun(t, f, v, p, false)
	for i := 1; i <= 4; i++ {
		input <- addEvent("add", fmt.Sprintf("https://example.com/%d", i))
	}
	waitUntil(t, "limit", func() bool { return hasInfo(v, "3 pages are already being fetched; wait for one") })
	p.mu.Lock()
	n := p.started
	p.mu.Unlock()
	if n > 3 {
		t.Errorf("%d fetches started", n)
	}
	close(rel)
	close(events)
	<-done
}

func TestRunAddPageAfterEndDropped(t *testing.T) {
	f := &fakeThink{}
	v := &recordingView{}
	u := "https://example.com/slow"
	rel := make(chan struct{})
	p := &fakePages{block: map[string]chan struct{}{u: rel}, pages: map[string]webpage.Page{u: {Title: "Slow", URL: u, Markdown: "x"}}}
	events, input, done, _ := startPageRun(t, f, v, p, false)
	input <- addEvent("add", u)
	waitUntil(t, "fetching", func() bool { return hasInfo(v, "📎 fetching "+u+"…") })
	close(events)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run waited for the fetch")
	}
	close(rel) // the goroutine finishes into the buffer and is dropped
}
