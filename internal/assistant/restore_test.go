package assistant

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseNotesRoundTrip(t *testing.T) {
	m := Memory{
		Briefs: []Brief{{Text: "Sam is the PM"}, {Text: "focus on risks to the release"}},
		Fixes:  []Fix{{Wrong: "filming", Right: "rendering"}, {Wrong: "a`b", Right: "c", Guessed: true}},
	}
	r, ok, err := ParseNotes(Render(m, "h", false, 0))
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	if want := []string{"Sam is the PM", "focus on risks to the release"}; !slices.Equal(r.Briefs, want) {
		t.Errorf("briefs = %q, want %q", r.Briefs, want)
	}
	wantFixes := []Fix{{Wrong: "filming", Right: "rendering"}, {Wrong: "a`b", Right: "c", Guessed: true}}
	if !slices.Equal(r.Fixes, wantFixes) {
		t.Errorf("fixes = %+v, want %+v", r.Fixes, wantFixes)
	}
	if r.Skipped != 0 {
		t.Errorf("skipped = %d", r.Skipped)
	}
}

func TestParseNotesNotNotes(t *testing.T) {
	r, ok, err := ParseNotes("# Agenda\n\n## Brief\n\n- x\n")
	if ok || err != nil || len(r.Briefs) != 0 || len(r.Fixes) != 0 || r.Skipped != 0 {
		t.Errorf("got %+v, %v, %v", r, ok, err)
	}
}

func notesWith(body string) string {
	return "# Meeting notes\n\nh · updated at 00:00:00\n\n" + draftNote + "\n\n" + body
}

func TestParseNotesDuplicateHeading(t *testing.T) {
	r, ok, err := ParseNotes(notesWith("## Brief\n\n- x\n\n## Fixes\n\n- `a` → `b`\n\n## Fixes\n\n- `c` → `d`\n"))
	if err == nil || !strings.Contains(err.Error(), `two "## Fixes" sections`) {
		t.Fatalf("err = %v", err)
	}
	if !ok || len(r.Fixes) != 0 || len(r.Briefs) != 0 {
		t.Errorf("nothing must be restored: %+v", r)
	}
}

func TestParseNotesSkipsBadLines(t *testing.T) {
	r, ok, err := ParseNotes(notesWith("## Brief\n\n- ## Fixes\n\n## Fixes\n\n- filming → rendering\n- `x` → ``\n- `a` → `b`\n"))
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	if !slices.Equal(r.Briefs, []string{"## Fixes"}) {
		t.Errorf("briefs = %q", r.Briefs)
	}
	if !slices.Equal(r.Fixes, []Fix{{Wrong: "a", Right: "b"}}) || r.Skipped != 2 {
		t.Errorf("fixes = %+v, skipped = %d", r.Fixes, r.Skipped)
	}
}

func TestParseNotesCapsBriefs(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Brief\n\n")
	for i := range 22 {
		fmt.Fprintf(&b, "- brief number %d\n", i)
	}
	r, _, err := ParseNotes(notesWith(b.String()))
	if err != nil || len(r.Briefs) != maxBriefs || r.Skipped != 2 {
		t.Errorf("briefs = %d, skipped = %d, err = %v", len(r.Briefs), r.Skipped, err)
	}
}

func TestRunRestoresAtStart(t *testing.T) {
	f := &fakeThink{replies: []func() (string, error){answer(`{"summary":"s"}`)}}
	v := &recordingView{}
	fastTimers(t)
	events, done := make(chan Event), make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Millisecond, Think: f.think, View: v,
			Restore:     &Restored{Briefs: []string{"Sam is the PM"}, Fixes: []Fix{{Wrong: "filming", Right: "rendering"}}},
			RestoreFrom: "standup.md"}, events)
	}()
	events <- line("others", "the filming work")
	waitUntil(t, "restore notice", func() bool { return hasInfo(v, "↺ restored 1 brief, 1 fix from standup.md") })
	b := lastBoardOf(v)
	if !slices.Equal(b.Briefs, []string{"Sam is the PM"}) || len(b.Fixes) != 1 {
		t.Errorf("board = %+v", b)
	}
	waitUntil(t, "fixed line", func() bool {
		return hasItem(v, func(it Item) bool { return it.Kind == ItemTranscript && it.Text == "the rendering work" })
	})
	waitUntil(t, "first update", func() bool { return f.count() == 1 })
	if instr, _ := f.call(0); !strings.Contains(instr, "1. Sam is the PM") {
		t.Errorf("first update must carry the brief:\n%s", instr)
	}
	close(events)
	<-done
}

func TestRestoreMessages(t *testing.T) {
	for _, c := range []struct {
		r    Restored
		want string
	}{
		{Restored{Briefs: []string{"a", "b"}}, "↺ restored 2 briefs from x.md"},
		{Restored{Fixes: []Fix{{Wrong: "a", Right: "b"}, {Wrong: "c", Right: "d"}}}, "↺ restored 2 fixes from x.md"},
		{Restored{Briefs: []string{"a"}, Skipped: 1}, "↺ restored 1 brief from x.md; 1 line skipped"},
		{Restored{Briefs: []string{"a"}, Skipped: 3}, "↺ restored 1 brief from x.md; 3 lines skipped"},
		{Restored{}, "↺ nothing to restore from x.md"},
	} {
		v := &recordingView{}
		s := &state{c: Config{View: v}, partials: map[string]string{}}
		s.restore(c.r, "x.md")
		if !hasInfo(v, c.want) {
			t.Errorf("restore(%+v): want info %q, got %+v", c.r, c.want, v.items)
		}
	}
}
