package assistant

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

type EventKind int

const (
	PartialEvent EventKind = iota // words still being recognized
	LineEvent                     // a committed transcript line
	NoticeEvent                   // a status message to show as is
	MuteEvent                     // a side was muted or unmuted (Muted)
	RecapEvent                    // the user asked for a recap of the last minutes
)

// Event is what the transcription side sends to Run.
type Event struct {
	Kind    EventKind
	Speaker string // "me" or "others"
	Text    string
	Muted   bool // MuteEvent: the side is now muted
}

type Config struct {
	Context  string        // trusted meeting background (LoadContext)
	Notes    string        // notes file path, rewritten after each update
	Header   string        // one line describing the session, for the notes
	Engine   string        // provider name, for messages ("claude")
	Interval time.Duration // shortest time between two updates
	// Think sends one prompt to the model and returns its answer.
	Think       func(instructions, transcript string) (string, error)
	Out         io.Writer       // plain output, used when View is nil
	Interactive bool            // Out is a terminal: status line and dim lines
	View        View            // shows the feed, status and dashboard; nil means NewPlainView on Out
	Force       <-chan struct{} // closed on a second Ctrl+C: stop waiting at once
	MuteHint    string          // mute keys for the status line, e.g. "m/s"; "" when there are none
	RecapKey    string          // the key that asks for a recap, for the status line; "" when there is none
	RecapWindow time.Duration   // how far back a recap goes; 0 means defaultRecapWindow
	// Research sends one prompt to the librarian and returns its answer and
	// the tool calls its allowlist refused; nil turns the librarian off.
	Research              func(prompt string) (ResearchReply, error)
	LibrarianInstructions string   // the user's sources and how to search them
	LibrarianTools        []string // the librarian's allowed tools, listed in its prompt
	MaxLookups            int      // lookups per meeting; 0 or less means defaultMaxLookups
}

// ResearchReply is the librarian's answer and the tool calls that its
// allowlist refused ("Tool(input)" or the tool name).
type ResearchReply struct {
	Answer  string
	Refused []string
}

// maxRefusedShown bounds the refused tool calls listed in the feed.
const maxRefusedShown = 3

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
	if c.View == nil {
		c.View = NewPlainView(c.Out, c.Interactive, c.MuteHint, c.RecapKey)
	}
	s := &state{c: c, start: time.Now(), sources: Sources(c.Context), results: make(chan result, 16),
		lookupResults: make(chan lookupResult, 1), partials: map[string]string{}, muted: map[string]bool{}}
	// Suggestion times are offsets from this start; with the wall clock, they
	// can be matched against a recording started at another moment.
	s.header = c.Header + " · started " + s.start.Format("2006-01-02 15:04:05")
	ticker := time.NewTicker(Tick)
	defer ticker.Stop()
	s.board()
	s.status(time.Now())

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
	upto   int  // lines covered by this update
	recap  bool // the answer is a recap, not an update
	answer string
	err    error
}

// defaultRecapWindow is how far back a recap goes when RecapWindow is unset.
const defaultRecapWindow = 5 * time.Minute

type lookupResult struct {
	lookup Lookup
	reply  ResearchReply
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
	muted       map[string]bool // sides muted with the mute keys
	lastSpeaker string

	thinking   bool
	recapping  bool // the call in flight is a recap
	recapAsked bool // a recap waits for the call in flight
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
	// The live session lags the audio by a second or two: what it delivers for
	// a muted side was said just before the key press. Muted means none of it
	// is used.
	if (ev.Kind == PartialEvent || ev.Kind == LineEvent) && s.muted[ev.Speaker] {
		return
	}
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
		s.show(Item{Kind: ItemTranscript, At: l.At, Speaker: l.Speaker, Text: l.Text,
			Plain: s.dim(fmt.Sprintf("  %s %-6s %s", clock(l.At), l.Speaker, l.Text))})
		if len(s.lines) == 1 {
			_ = s.writeNotes(false) // a failure is shown in the feed
		}
	case NoticeEvent:
		s.notice(Clean(ev.Text))
	case RecapEvent:
		s.askRecap(time.Now())
	case MuteEvent:
		s.muted[ev.Speaker] = ev.Muted
		icon, word := "🔊", "unmuted"
		if ev.Muted {
			icon, word = "🔇", "muted"
			delete(s.partials, ev.Speaker) // its words in progress won't be finished
		}
		s.notice(fmt.Sprintf("%s %s %s at %s", icon, ev.Speaker, word, clock(s.elapsed(time.Now()))))
	}
}

// recapWindow is how far back a recap goes.
func (s *state) recapWindow() time.Duration {
	if s.c.RecapWindow > 0 {
		return s.c.RecapWindow
	}
	return defaultRecapWindow
}

// askRecap notes a recap request; it starts as soon as no call is in flight.
func (s *state) askRecap(now time.Time) {
	if s.ending {
		return
	}
	if s.recapAsked || (s.thinking && s.recapping) {
		s.info("📝 a recap is already on its way")
		return
	}
	if len(recentLines(s.lines, s.elapsed(now), s.recapWindow())) == 0 {
		s.info("📝 nothing was said in the last " + WindowLabel(s.recapWindow()))
		return
	}
	s.recapAsked = true
}

func (s *state) startRecap(now time.Time) {
	s.recapAsked = false
	s.info(fmt.Sprintf("📝 recapping the last %s (%s)…", WindowLabel(s.recapWindow()), s.c.Engine))
	instructions, transcript := RecapPrompt(s.mem, s.lines, s.elapsed(now), s.recapWindow())
	s.call(now, s.analysed, true, instructions, transcript)
}

// finishRecap shows a recap answer in the feed.
func (s *state) finishRecap(r result) {
	var recap struct {
		Recap []string `json:"recap"`
	}
	err := r.err
	if err == nil {
		err = parseJSON(r.answer, &recap)
	}
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), s.c.Engine+" failed: ")
		s.notice("⚠ recap failed: " + oneLine(msg, 200))
		return
	}
	at := s.elapsed(time.Now())
	points := cleanList(recap.Recap, maxRecapPoints)
	if len(points) == 0 {
		s.info(fmt.Sprintf("📝 %s nothing notable in the last %s", clock(at), WindowLabel(s.recapWindow())))
		return
	}
	s.notice(fmt.Sprintf("📝 %s Recap of the last %s:", clock(at), WindowLabel(s.recapWindow())))
	for _, p := range points {
		s.show(Item{Kind: ItemAssist, At: at, Badge: "RECAP", Text: Clean(p), Plain: "  • " + Clean(p)})
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
	if !s.thinking && s.recapAsked {
		s.startRecap(now)
		return
	}
	if s.thinking || s.analysed == len(s.lines) || now.Sub(s.lastStart) < s.interval() {
		return
	}
	fresh := s.lines[s.analysed:]
	if wordCount(fresh) < minNewWords && s.elapsed(now)-fresh[0].At < 2*s.c.Interval {
		return
	}
	upto := len(s.lines)
	instructions, transcript := CyclePrompt(s.c.Context, s.mem, s.lines[:s.analysed], s.lines[s.analysed:upto], s.c.Research != nil)
	s.call(now, upto, false, instructions, transcript)
}

func (s *state) call(now time.Time, upto int, recap bool, instructions, transcript string) {
	s.gen++
	gen, think, results := s.gen, s.c.Think, s.results
	s.thinking, s.recapping, s.thinkStart = true, recap, now
	if !recap { // a recap does not delay the next update
		s.lastStart = now
	}
	go func() {
		answer, err := think(instructions, transcript)
		select {
		case results <- result{gen: gen, upto: upto, recap: recap, answer: answer, err: err}:
		default: // only abandoned calls can find the buffer full
		}
	}()
}

func (s *state) finish(r result) {
	if r.gen != s.gen || !s.thinking {
		return
	}
	s.thinking = false
	if r.recap {
		s.finishRecap(r)
		return
	}
	reply, err := r.answer, r.err
	var parsed Reply
	if err == nil {
		parsed, err = Parse(reply)
	}
	if err != nil {
		s.failures++
		// Harness errors already start with "<engine> failed: ".
		msg := strings.TrimPrefix(err.Error(), s.c.Engine+" failed: ")
		s.notice(fmt.Sprintf("⚠ %s failed: %s; next try in %s", s.c.Engine, oneLine(msg, 200), s.interval().Round(time.Second)))
		return
	}
	s.failures = 0
	s.analysed = r.upto
	at := s.elapsed(time.Now())
	suggestions, decisions := s.mem.Apply(parsed, at, s.sources)
	s.board()
	for _, sg := range suggestions {
		l := suggestionLabels[sg.Type]
		it := Item{Kind: ItemAssist, At: at, Badge: suggestionBadges[sg.Type], Text: Clean(sg.Text), High: sg.Priority == High}
		line := fmt.Sprintf("%s %s %s: %s", l.icon, clock(at), l.label, it.Text)
		if it.High {
			line = "‼ " + line
		}
		if note := suggestionNote(sg); note != "" {
			it.Note = Clean(note)
			line += s.dim(" — " + it.Note)
		}
		it.Plain = line
		s.show(it)
	}
	for _, d := range decisions {
		s.show(Item{Kind: ItemAssist, At: at, Badge: "DECISION", Text: Clean(d),
			Plain: fmt.Sprintf("✅ %s Decision: %s", clock(at), Clean(d))})
	}
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.queueLookup(parsed, at)
}

// queueLookup takes the lookup a reply asks, if any: it starts at once when
// the librarian is idle, else it waits, replacing an older waiting one. A
// replaced lookup never ran: it leaves Asked, so it frees its slot under the
// limit and may be asked again.
func (s *state) queueLookup(r Reply, at time.Duration) {
	if s.c.Research == nil || s.ending || len(r.Lookups) == 0 {
		return
	}
	limit := s.c.MaxLookups
	if limit <= 0 {
		limit = defaultMaxLookups
	}
	waiting := s.pending
	if waiting != nil {
		s.mem.forgetAsked(waiting.Query)
	}
	keepWaiting := func() {
		if waiting != nil {
			s.mem.Asked = append(s.mem.Asked, waiting.Query)
		}
	}
	if len(s.mem.Asked) >= limit {
		keepWaiting()
		if !s.limitShown {
			s.limitShown = true
			s.info(fmt.Sprintf("📚 lookup limit reached (%d)", limit))
		}
		return
	}
	l, ok := s.mem.TakeLookup(r, at, limit)
	if !ok {
		keepWaiting()
		return
	}
	s.pending, s.pendingSince = &l, time.Now()
	s.maybeLookup(time.Now())
}

// maybeLookup drops a waiting lookup once it is too old (silently, and out of
// Asked: it never ran), and starts it when the librarian is idle.
func (s *state) maybeLookup(now time.Time) {
	if s.pending == nil || s.looking {
		return
	}
	l := *s.pending
	s.pending = nil
	if now.Sub(s.pendingSince) > LookupMaxWait {
		s.mem.forgetAsked(l.Query)
		return
	}
	s.looking = true
	s.show(Item{Kind: ItemAssist, At: s.elapsed(now), Badge: "LOOKUP", Text: l.Query,
		Plain: s.dim(fmt.Sprintf("🔎 %s Looking up: %s", clock(s.elapsed(now)), l.Query))})
	prompt := LibrarianPrompt(s.c.LibrarianInstructions, s.c.LibrarianTools, l, s.mem, now)
	research, results := s.c.Research, s.lookupResults
	go func() {
		reply, err := research(prompt)
		select {
		case results <- lookupResult{lookup: l, reply: reply, err: err}:
		default: // only after the end, when nobody reads results
		}
	}()
}

// showRefused lists, dimmed, the tool calls the librarian's allowlist
// refused: the sign that librarian.allowed_tools misses a tool it needs.
func (s *state) showRefused(calls []string) {
	if len(calls) == 0 {
		return
	}
	shown := make([]string, 0, maxRefusedShown+1)
	for i, c := range calls {
		if i == maxRefusedShown {
			shown = append(shown, "…")
			break
		}
		shown = append(shown, oneLine(c, 80))
	}
	s.info(fmt.Sprintf("📚 refused %d tool call(s) not in librarian.allowed_tools: %s", len(calls), strings.Join(shown, ", ")))
}

// finishLookup merges a librarian answer into the memory, the feed and the
// notes, then starts the waiting lookup, if any.
func (s *state) finishLookup(r lookupResult) {
	s.looking = false
	defer s.maybeLookup(time.Now())
	s.showRefused(r.reply.Refused)
	f, err := Finding{}, r.err
	if err == nil {
		f, err = ParseFinding(r.reply.Answer)
	}
	if err != nil {
		s.notice("⚠ librarian failed: " + oneLine(err.Error(), 200))
		return
	}
	f.Query, f.At = r.lookup.Query, s.elapsed(time.Now())
	s.mem.AddFinding(f)
	s.board()
	if f.Found {
		it := Item{Kind: ItemAssist, At: f.At, Badge: "LIBRARIAN", Text: f.Answer}
		line := fmt.Sprintf("📚 %s Librarian: %s", clock(f.At), f.Answer)
		var titles []string
		for _, src := range f.Sources {
			if src.Title != "" {
				titles = append(titles, src.Title)
			}
		}
		if len(titles) > 0 {
			it.Note = strings.Join(titles, ", ")
			line += " — " + it.Note
		}
		it.Plain = line
		s.show(it)
	} else {
		s.info("📚 nothing found for " + f.Query)
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
	if s.recapping { // a lost recap does not slow the updates down
		s.notice(fmt.Sprintf("⚠ recap failed: %s did not answer in %s", s.c.Engine, limit.Round(time.Second)))
		return
	}
	s.failures++
	s.notice(fmt.Sprintf("⚠ %s did not answer in %s; skipping this update", s.c.Engine, limit.Round(time.Second)))
}

// end waits for the update in progress, then writes the final notes. It
// ignores lookups: one in flight is not waited for, and none starts.
func (s *state) end() error {
	s.ending = true
	s.c.View.Status(Status{}) // clears the status line
	if s.thinking {
		s.notice(fmt.Sprintf("⏳ finishing the current update (%s)…", s.c.Engine))
		select {
		case r := <-s.waitCurrent():
			s.finish(r)
		case <-s.c.Force:
			return s.writeNotes(false)
		}
	}
	if wordCount(s.lines) < minFinalWords {
		if len(s.lines) > 0 {
			s.notice("Too little was said for a final summary.")
		}
		return s.writeNotes(false)
	}

	s.notice(fmt.Sprintf("⏳ writing the final notes (%s)…", s.c.Engine))
	instructions, transcript := FinalPrompt(s.c.Context, s.mem, s.lines)
	s.call(time.Now(), len(s.lines), false, instructions, transcript)
	select {
	case r := <-s.waitCurrent():
		s.thinking = false
		answer, err := r.answer, r.err
		var parsed Reply
		if err == nil {
			parsed, err = Parse(answer)
		}
		if err != nil {
			s.notice(fmt.Sprintf("⚠ final notes failed: %s; the notes keep the live summary", oneLine(err.Error(), 160)))
			return s.writeNotes(false)
		}
		s.mem.ApplyFinal(parsed)
		s.board()
		if err := s.writeNotes(true); err != nil {
			return err
		}
		s.notice("✓ Final notes written to " + s.c.Notes)
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
		s.notice("⚠ cannot write the notes: " + err.Error())
	}
	return err
}

// suggestionBadges are the feed badges of the suggestion types.
var suggestionBadges = map[string]string{TypeQuestion: "ASK", TypeAnswer: "ANSWER", TypeMissed: "MISSED", TypeRisk: "RISK"}

// show adds an item, then refreshes the status: what a line changes (a side
// muted, a partial replaced, a call over) must not linger under it.
func (s *state) show(it Item) {
	s.c.View.Show(it)
	s.status(time.Now())
}

// notice shows text as is, info dimmed.
func (s *state) notice(text string) { s.show(Item{Kind: ItemNotice, Text: text, Plain: text}) }
func (s *state) info(text string)   { s.show(Item{Kind: ItemInfo, Text: text, Plain: s.dim(text)}) }

func (s *state) status(now time.Time) {
	st := Status{Elapsed: s.elapsed(now), Engine: s.c.Engine, State: "listening", Looking: s.looking,
		Muted: map[string]bool{}, PartialSpeaker: s.lastSpeaker, Partial: s.partials[s.lastSpeaker]}
	for side, m := range s.muted {
		st.Muted[side] = m
	}
	if s.thinking {
		st.State, st.Busy = "thinking", now.Sub(s.thinkStart)
		if s.recapping {
			st.State = "recapping"
		}
	}
	s.c.View.Status(st)
}

// board sends the memory to the dashboard.
func (s *state) board() {
	m := s.mem
	s.c.View.Board(Board{Summary: m.Summary, NotCovered: slices.Clone(m.NotCovered), Decisions: slices.Clone(m.Decisions),
		OpenQuestions: slices.Clone(m.OpenQuestions), Sources: slices.Clone(s.sources)})
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
