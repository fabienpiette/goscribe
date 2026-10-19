package assistant

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type EventKind int

const (
	PartialEvent EventKind = iota // words still being recognized
	LineEvent                     // a committed transcript line
	NoticeEvent                   // a status message to show as is
)

// Event is what the transcription side sends to Run.
type Event struct {
	Kind    EventKind
	Speaker string // "me" or "others"
	Text    string
}

type Config struct {
	Context  string        // trusted meeting background (LoadContext)
	Notes    string        // notes file path, rewritten after each update
	Header   string        // one line describing the session, for the notes
	Engine   string        // provider name, for messages ("claude")
	Interval time.Duration // shortest time between two updates
	// Think sends one prompt to the model and returns its answer.
	Think       func(instructions, transcript string) (string, error)
	Out         io.Writer
	Interactive bool            // Out is a terminal: status line and dim lines
	Force       <-chan struct{} // closed on a second Ctrl+C: stop waiting at once
	// Research sends one prompt to the librarian and returns its answer; nil
	// turns the librarian off.
	Research              func(prompt string) (string, error)
	LibrarianInstructions string // the user's sources and how to search them
	MaxLookups            int    // lookups per meeting; 0 or less means defaultMaxLookups
}

// Overridable for tests.
var (
	Tick        = time.Second     // status redraw and scheduling check
	MaxBackoff  = 5 * time.Minute // longest wait between updates after failures
	MinWatchdog = 3 * time.Minute // shortest wait before giving up on a call
	// LookupMaxWait is how long a lookup may wait for the one in progress
	// before it is dropped.
	LookupMaxWait = 2 * time.Minute
)

const (
	defaultMaxLookups = 8  // Config.MaxLookups when unset
	minNewWords       = 12 // words worth an update before 2×Interval has passed
	minFinalWords     = 30 // below this, there is nothing to summarize
)

// Run follows the meeting until events is closed: it shows the transcript and
// the suggestions, updates the memory in cycles (at most one model call at a
// time) and rewrites the notes file. Then it writes the final notes, unless
// Force is closed first. It returns an error only if the notes can't be
// written at the end.
func Run(c Config, events <-chan Event) error {
	s := &state{c: c, start: time.Now(), sources: Sources(c.Context), results: make(chan result, 16),
		lookupResults: make(chan lookupResult, 1), partials: map[string]string{}}
	// Suggestion times are offsets from this start; with the wall clock, they
	// can be matched against a recording started at another moment.
	s.header = c.Header + " · started " + s.start.Format("2006-01-02 15:04:05")
	ticker := time.NewTicker(Tick)
	defer ticker.Stop()

	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			s.handle(ev)
			s.maybeStart(time.Now())
		case r := <-s.results:
			s.finish(r)
		case r := <-s.lookupResults:
			s.finishLookup(r)
		case now := <-ticker.C:
			s.watchdog(now)
			s.maybeStart(now)
			s.maybeLookup(now)
			s.status(now)
		}
	}
	return s.end()
}

type result struct {
	gen    int
	upto   int // lines covered by this update
	answer string
	err    error
}

type lookupResult struct {
	lookup Lookup
	answer string
	err    error
}

type state struct {
	c       Config
	start   time.Time
	header  string   // Config.Header with the wall-clock start
	sources []string // context files an answer may come from

	lines    []Line
	analysed int // lines already covered by a successful update
	mem      Memory

	partials    map[string]string
	lastSpeaker string

	thinking   bool
	thinkStart time.Time
	lastStart  time.Time
	failures   int
	gen        int // abandoned calls carry an older gen and are ignored
	results    chan result

	looking       bool    // a librarian lookup is in flight
	pending       *Lookup // the newest lookup waiting for it
	pendingSince  time.Time
	limitShown    bool // the lookup limit line was printed
	ending        bool // the meeting is over: no new lookups
	lookupResults chan lookupResult

	notesErr bool
}

func (s *state) elapsed(now time.Time) time.Duration { return now.Sub(s.start) }

func (s *state) handle(ev Event) {
	switch ev.Kind {
	case PartialEvent:
		s.partials[ev.Speaker] = Clean(ev.Text)
		s.lastSpeaker = ev.Speaker
	case LineEvent:
		text := Clean(strings.TrimSpace(ev.Text))
		if text == "" {
			return
		}
		l := Line{Speaker: ev.Speaker, At: s.elapsed(time.Now()), Text: text}
		s.lines = append(s.lines, l)
		delete(s.partials, ev.Speaker)
		s.print(s.dim(fmt.Sprintf("  %s %-6s %s", clock(l.At), l.Speaker, l.Text)))
		if len(s.lines) == 1 {
			_ = s.writeNotes(false) // a failure is shown in the feed
		}
	case NoticeEvent:
		s.print(Clean(ev.Text))
	}
}

// interval is the wait between updates, doubled after each failure.
func (s *state) interval() time.Duration {
	d := s.c.Interval
	for i := 0; i < s.failures && d < MaxBackoff; i++ {
		d *= 2
	}
	return min(d, MaxBackoff)
}

func (s *state) maybeStart(now time.Time) {
	if s.thinking || s.analysed == len(s.lines) || now.Sub(s.lastStart) < s.interval() {
		return
	}
	fresh := s.lines[s.analysed:]
	if wordCount(fresh) < minNewWords && s.elapsed(now)-fresh[0].At < 2*s.c.Interval {
		return
	}
	upto := len(s.lines)
	instructions, transcript := CyclePrompt(s.c.Context, s.mem, s.lines[:s.analysed], s.lines[s.analysed:upto], s.c.Research != nil)
	s.call(now, upto, instructions, transcript)
}

func (s *state) call(now time.Time, upto int, instructions, transcript string) {
	s.gen++
	gen, think, results := s.gen, s.c.Think, s.results
	s.thinking, s.thinkStart, s.lastStart = true, now, now
	go func() {
		answer, err := think(instructions, transcript)
		select {
		case results <- result{gen: gen, upto: upto, answer: answer, err: err}:
		default: // only abandoned calls can find the buffer full
		}
	}()
}

func (s *state) finish(r result) {
	if r.gen != s.gen || !s.thinking {
		return
	}
	s.thinking = false
	reply, err := r.answer, r.err
	var parsed Reply
	if err == nil {
		parsed, err = Parse(reply)
	}
	if err != nil {
		s.failures++
		s.print(fmt.Sprintf("⚠ %s failed: %s; next try in %s", s.c.Engine, oneLine(err.Error(), 160), s.interval().Round(time.Second)))
		return
	}
	s.failures = 0
	s.analysed = r.upto
	at := s.elapsed(time.Now())
	suggestions, decisions := s.mem.Apply(parsed, at, s.sources)
	for _, sg := range suggestions {
		l := suggestionLabels[sg.Type]
		line := fmt.Sprintf("%s %s %s: %s", l.icon, clock(at), l.label, Clean(sg.Text))
		if sg.Priority == High {
			line = "‼ " + line
		}
		if note := suggestionNote(sg); note != "" {
			line += s.dim(" — " + Clean(note))
		}
		s.print(line)
	}
	for _, d := range decisions {
		s.print(fmt.Sprintf("✅ %s Decision: %s", clock(at), Clean(d)))
	}
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.queueLookup(parsed, at)
}

// queueLookup takes the lookup a reply asks, if any: it starts at once when
// the librarian is idle, else it waits, replacing an older waiting one.
func (s *state) queueLookup(r Reply, at time.Duration) {
	if s.c.Research == nil || s.ending || len(r.Lookups) == 0 {
		return
	}
	limit := s.c.MaxLookups
	if limit <= 0 {
		limit = defaultMaxLookups
	}
	if len(s.mem.Asked) >= limit {
		if !s.limitShown {
			s.limitShown = true
			s.print(s.dim(fmt.Sprintf("📚 lookup limit reached (%d)", limit)))
		}
		return
	}
	l, ok := s.mem.TakeLookup(r, at, limit)
	if !ok {
		return
	}
	s.pending, s.pendingSince = &l, time.Now()
	s.maybeLookup(time.Now())
}

// maybeLookup drops a waiting lookup once it is too old, and starts it when
// the librarian is idle.
func (s *state) maybeLookup(now time.Time) {
	if s.pending == nil || s.looking {
		return
	}
	l := *s.pending
	s.pending = nil
	if now.Sub(s.pendingSince) > LookupMaxWait {
		return
	}
	s.looking = true
	s.print(s.dim(fmt.Sprintf("🔎 %s Looking up: %s", clock(s.elapsed(now)), l.Query)))
	prompt := LibrarianPrompt(s.c.LibrarianInstructions, l, s.mem, now)
	research, results := s.c.Research, s.lookupResults
	go func() {
		answer, err := research(prompt)
		select {
		case results <- lookupResult{lookup: l, answer: answer, err: err}:
		default: // only after the end, when nobody reads results
		}
	}()
}

// finishLookup merges a librarian answer into the memory, the feed and the
// notes, then starts the waiting lookup, if any.
func (s *state) finishLookup(r lookupResult) {
	s.looking = false
	defer s.maybeLookup(time.Now())
	f, err := Finding{}, r.err
	if err == nil {
		f, err = ParseFinding(r.answer)
	}
	if err != nil {
		s.print("⚠ librarian failed: " + oneLine(err.Error(), 160))
		return
	}
	f.Query, f.At = r.lookup.Query, s.elapsed(time.Now())
	s.mem.AddFinding(f)
	if f.Found {
		line := fmt.Sprintf("📚 %s Librarian: %s", clock(f.At), f.Answer)
		var titles []string
		for _, src := range f.Sources {
			if src.Title != "" {
				titles = append(titles, src.Title)
			}
		}
		if len(titles) > 0 {
			line += " — " + strings.Join(titles, ", ")
		}
		s.print(line)
	} else {
		s.print(s.dim("📚 nothing found for " + f.Query))
	}
	_ = s.writeNotes(false) // a failure is shown in the feed
}

func (s *state) watchdog(now time.Time) {
	limit := max(MinWatchdog, 6*s.c.Interval)
	if !s.thinking || now.Sub(s.thinkStart) < limit {
		return
	}
	s.gen++ // its answer, if it ever comes, is ignored
	s.thinking = false
	s.failures++
	s.print(fmt.Sprintf("⚠ %s did not answer in %s; skipping this update", s.c.Engine, limit.Round(time.Second)))
}

// end waits for the update in progress, then writes the final notes. It
// ignores lookups: one in flight is not waited for, and none starts.
func (s *state) end() error {
	s.ending = true
	s.clearStatus()
	if s.thinking {
		s.print(fmt.Sprintf("⏳ finishing the current update (%s)…", s.c.Engine))
		select {
		case r := <-s.waitCurrent():
			s.finish(r)
		case <-s.c.Force:
			return s.writeNotes(false)
		}
	}
	if wordCount(s.lines) < minFinalWords {
		if len(s.lines) > 0 {
			s.print("Too little was said for a final summary.")
		}
		return s.writeNotes(false)
	}

	s.print(fmt.Sprintf("⏳ writing the final notes (%s)…", s.c.Engine))
	instructions, transcript := FinalPrompt(s.c.Context, s.mem, s.lines)
	s.call(time.Now(), len(s.lines), instructions, transcript)
	select {
	case r := <-s.waitCurrent():
		s.thinking = false
		answer, err := r.answer, r.err
		var parsed Reply
		if err == nil {
			parsed, err = Parse(answer)
		}
		if err != nil {
			s.print(fmt.Sprintf("⚠ final notes failed: %s; the notes keep the live summary", oneLine(err.Error(), 160)))
			return s.writeNotes(false)
		}
		s.mem.ApplyFinal(parsed)
		if err := s.writeNotes(true); err != nil {
			return err
		}
		s.print("✓ Final notes written to " + s.c.Notes)
		return nil
	case <-s.c.Force:
		return s.writeNotes(false)
	}
}

// waitCurrent returns a channel that delivers the result of the current call
// only, skipping late answers of abandoned ones.
func (s *state) waitCurrent() <-chan result {
	out := make(chan result, 1)
	gen := s.gen
	go func() {
		for r := range s.results {
			if r.gen == gen {
				out <- r
				return
			}
		}
	}()
	return out
}

func (s *state) writeNotes(final bool) error {
	if s.c.Notes == "" {
		return nil
	}
	err := WriteNotes(s.c.Notes, Render(s.mem, s.header, final, s.elapsed(time.Now())))
	if err != nil && !s.notesErr {
		s.notesErr = true
		s.print("⚠ cannot write the notes: " + err.Error())
	}
	return err
}

func (s *state) print(line string) {
	s.clearStatus()
	fmt.Fprintln(s.c.Out, line)
	s.status(time.Now())
}

func (s *state) clearStatus() {
	if s.c.Interactive {
		fmt.Fprint(s.c.Out, "\r\033[K")
	}
}

func (s *state) status(now time.Time) {
	if !s.c.Interactive {
		return
	}
	line := "● ASSIST " + clock(s.elapsed(now))
	if p := s.partials[s.lastSpeaker]; p != "" {
		line += fmt.Sprintf("  %s ▸ %q", s.lastSpeaker, lastRunes(p, 60))
	}
	if s.thinking {
		line += fmt.Sprintf("  · thinking %ds (%s)", int(now.Sub(s.thinkStart).Seconds()), s.c.Engine)
	} else {
		line += "  · listening"
	}
	fmt.Fprint(s.c.Out, "\r\033[K"+line+"   (Ctrl+C to stop)")
}

func (s *state) dim(text string) string {
	if !s.c.Interactive {
		return text
	}
	return "\033[2m" + text + "\033[0m"
}

func wordCount(lines []Line) int {
	n := 0
	for _, l := range lines {
		n += len(strings.Fields(l.Text))
	}
	return n
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n+1:])
}

// oneLine fits an error message on one line for the feed. Errors can carry
// an API response or a CLI's stderr, so they are cleaned like transcript text.
func oneLine(s string, n int) string {
	return truncate(strings.Join(strings.Fields(Clean(s)), " "), n)
}
