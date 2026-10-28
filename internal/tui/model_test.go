package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"goscribe/internal/assistant"
)

// recorder collects the calls the model makes through Options.
type recorder struct {
	sent       []assistant.Event
	toggled    []string
	interrupts int
}

func (r *recorder) options() Options {
	return Options{
		Send:      func(e assistant.Event) { r.sent = append(r.sent, e) },
		Toggle:    func(l string) { r.toggled = append(r.toggled, l) },
		Interrupt: func() { r.interrupts++ },
		Sides:     []string{"me", "others"},
	}
}

func send(m tea.Model, msgs ...tea.Msg) tea.Model {
	for _, msg := range msgs {
		m, _ = m.Update(msg)
	}
	return m
}

func sized(t *testing.T, r *recorder, w, h int) tea.Model {
	t.Helper()
	return send(New(r.options()), tea.WindowSizeMsg{Width: w, Height: h})
}

func typed(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var (
	enter = tea.KeyMsg{Type: tea.KeyEnter}
	tab   = tea.KeyMsg{Type: tea.KeyTab}
)

func line(at time.Duration, speaker, text string) ItemMsg {
	return ItemMsg{Kind: assistant.ItemTranscript, At: at, Speaker: speaker, Text: text}
}

// inputLine is the last line of the view: the input strip.
func inputLine(m tea.Model) string {
	lines := strings.Split(m.View(), "\n")
	return lines[len(lines)-1]
}

func checkWidth(t *testing.T, view string, w int) {
	t.Helper()
	for i, l := range strings.Split(view, "\n") {
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("line %d is %d cells wide, more than %d: %q", i, lw, w, l)
		}
	}
}

func TestModelLayoutWide(t *testing.T) {
	m := sized(t, &recorder{}, 140, 30)
	m = send(m, BoardMsg{Summary: "Design tokens v2"}, line(5*time.Second, "others", "Let's ship dark-mode first"))
	v := m.View()
	for _, want := range []string{"goscribe assist", "SUMMARY", "Design tokens v2", "Let's ship dark-mode first", "/help"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	checkWidth(t, v, 140)
	if strings.Contains(v, "│") {
		t.Errorf("view has a divider:\n%s", v)
	}
	if n := len(strings.Split(v, "\n")); n != 30 {
		t.Errorf("view has %d lines, want 30", n)
	}
}

func TestModelNarrowHidesDashboardTabShowsIt(t *testing.T) {
	m := send(sized(t, &recorder{}, 100, 30), BoardMsg{Summary: "Design tokens v2"})
	if strings.Contains(m.View(), "SUMMARY") {
		t.Fatal("dashboard shown below WideWidth")
	}
	m = send(m, tab)
	if !strings.Contains(m.View(), "SUMMARY") {
		t.Fatal("Tab did not show the dashboard")
	}
	checkWidth(t, m.View(), 100)
	m = send(m, tab)
	if strings.Contains(m.View(), "SUMMARY") {
		t.Fatal("second Tab did not hide the dashboard")
	}
}

func TestModelResizeClosesOverlayWhenWide(t *testing.T) {
	m := send(sized(t, &recorder{}, 100, 30), BoardMsg{Summary: "Design tokens v2"}, line(0, "me", "hello Sam"), tab)
	m = send(m, tea.WindowSizeMsg{Width: 140, Height: 30})
	v := m.View()
	if c := strings.Count(v, "SUMMARY"); c != 1 {
		t.Fatalf("SUMMARY shown %d times on a wide screen:\n%s", c, v)
	}
	if !strings.Contains(v, "hello Sam") {
		t.Fatalf("feed hidden on a wide screen:\n%s", v)
	}
	checkWidth(t, v, 140)
	m = send(m, tea.WindowSizeMsg{Width: 60, Height: 10})
	v = m.View()
	if strings.Contains(v, "SUMMARY") {
		t.Fatalf("overlay stuck open after going narrow again:\n%s", v)
	}
	checkWidth(t, v, 60)
	// Tiny windows must not panic either.
	send(m, tea.WindowSizeMsg{Width: 1, Height: 1}).View()
	send(m, tea.WindowSizeMsg{Width: 0, Height: 0}).View()
}

func TestModelEnterDispatches(t *testing.T) {
	r := &recorder{}
	m := sized(t, r, 140, 30)

	m = send(m, typed("/mute me"), enter)
	if fmt.Sprint(r.toggled) != "[mic]" {
		t.Errorf("toggled = %v, want [mic]", r.toggled)
	}
	m = send(m, typed("/mute others"), enter)
	if fmt.Sprint(r.toggled) != "[mic system]" {
		t.Errorf("toggled = %v, want [mic system]", r.toggled)
	}
	if strings.Contains(inputLine(m), "/mute") {
		t.Errorf("input not cleared: %q", inputLine(m))
	}

	m = send(m, typed("/recap 10m"), enter)
	if len(r.sent) != 1 || r.sent[0].Kind != assistant.RecapEvent || r.sent[0].Window != 10*time.Minute {
		t.Fatalf("sent = %+v, want one RecapEvent of 10m", r.sent)
	}
	m = send(m, typed("why?"), enter)
	if len(r.sent) != 2 || r.sent[1].Kind != assistant.QuestionEvent || r.sent[1].Text != "why?" {
		t.Fatalf("sent = %+v, want a QuestionEvent why?", r.sent)
	}
	if strings.Contains(inputLine(m), "why?") {
		t.Errorf("input not cleared: %q", inputLine(m))
	}

	m = send(m, enter, typed("   "), enter)
	if len(r.sent) != 2 || len(r.toggled) != 2 {
		t.Errorf("empty Enter made a call: sent %v, toggled %v", r.sent, r.toggled)
	}

	m = send(m, typed("/nope"), enter)
	if !strings.Contains(m.View(), "unknown command /nope") {
		t.Errorf("no error in the feed:\n%s", m.View())
	}
	if strings.Contains(inputLine(m), "/nope") {
		t.Errorf("input not cleared: %q", inputLine(m))
	}

	m = send(m, typed("/help"), enter)
	v := m.View()
	for _, c := range assistant.Commands {
		if !strings.Contains(v, c.Usage) {
			t.Errorf("help lacks %q:\n%s", c.Usage, v)
		}
	}
	if !strings.Contains(v, "Tab: dashboard") || !strings.Contains(v, "end the meeting") {
		t.Errorf("help lacks the keys line:\n%s", v)
	}
}

func TestModelAltKeys(t *testing.T) {
	r := &recorder{}
	m := sized(t, r, 140, 30)
	for _, k := range "msw" {
		m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}, Alt: true})
	}
	if fmt.Sprint(r.toggled) != "[mic system recap]" {
		t.Errorf("toggled = %v, want [mic system recap]", r.toggled)
	}
	// Other Alt keys do not type either.
	m = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})
	m = send(m, enter)
	if len(r.sent) != 0 {
		t.Fatalf("Alt keys went into the input: sent %+v", r.sent)
	}
	send(m, typed("m"), enter)
	if len(r.sent) != 1 || r.sent[0].Text != "m" {
		t.Errorf("plain m not typed: sent %+v", r.sent)
	}
}

func TestModelScrollKeepsPositionAndCountsNew(t *testing.T) {
	m := sized(t, &recorder{}, 140, 30)
	for i := range 100 {
		m = send(m, line(time.Duration(i)*time.Second, "others", fmt.Sprintf("line number %d", i)))
	}
	if !strings.Contains(m.View(), "line number 99") {
		t.Fatal("feed does not follow live")
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyPgUp})
	m = send(m, line(0, "me", "late one"), line(0, "me", "late two"), line(0, "me", "late three"))
	v := m.View()
	if !strings.Contains(v, "↓ 3 new") {
		t.Fatalf("no new marker:\n%s", v)
	}
	if strings.Contains(v, "late three") {
		t.Fatalf("scrolled feed jumped to live:\n%s", v)
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyEnd})
	v = m.View()
	if strings.Contains(v, "new") || !strings.Contains(v, "late three") {
		t.Fatalf("End did not return to live:\n%s", v)
	}

	m = send(m, tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	m = send(m, line(0, "me", "after wheel"))
	if v := m.View(); !strings.Contains(v, "↓ 1 new") {
		t.Fatalf("wheel up did not leave live:\n%s", v)
	}
	for range 10 {
		m = send(m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	if v := m.View(); strings.Contains(v, "new") || !strings.Contains(v, "after wheel") {
		t.Fatalf("wheel to the bottom did not return to live:\n%s", v)
	}
}

func TestModelWrapsWideText(t *testing.T) {
	text := ""
	for utf8.RuneCountInString(text) < 300 {
		text += "会議の議事録😀🚀 dark-mode "
	}
	m := sized(t, &recorder{}, 80, 40)
	m = send(m, line(0, "others", text), ItemMsg{Kind: assistant.ItemAssist, Badge: "ANSWER", Text: text})
	v := m.View()
	checkWidth(t, v, 80)
	for i, l := range strings.Split(v, "\n") {
		if !utf8.ValidString(l) {
			t.Errorf("line %d is not valid UTF-8: %q", i, l)
		}
	}
	if !strings.Contains(v, "🚀") {
		t.Errorf("wide text lost:\n%s", v)
	}
}

func TestModelBadgesAndTopBar(t *testing.T) {
	m := sized(t, &recorder{}, 140, 30)
	m = send(m, ItemMsg{Kind: assistant.ItemAssist, Badge: "RISK", High: true, Text: "Acme deadline slips"})
	v := m.View()
	for _, want := range []string{"RISK", "‼", "Acme deadline slips"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}

	m = send(m, StatusMsg{Elapsed: 83 * time.Second, State: "thinking", Busy: 4 * time.Second, Engine: "claude",
		Muted: map[string]bool{"me": true}})
	top := strings.Split(m.View(), "\n")[0]
	for _, want := range []string{"goscribe assist", "01:23", "claude thinking 4s", "me (muted)", "others"} {
		if !strings.Contains(top, want) {
			t.Errorf("top bar lacks %q: %q", want, top)
		}
	}

	m = send(m, line(0, "others", "we agreed on Globex"), StatusMsg{State: "listening", PartialSpeaker: "others", Partial: "so the next step"})
	if last := lastFeedLine(m); !strings.Contains(last, "others ▸ so the next step") {
		t.Errorf("last feed line = %q, want the partial line", last)
	}
	if top := strings.Split(m.View(), "\n")[0]; strings.Contains(top, "so the next step") {
		t.Errorf("partial words in the top bar: %q", top)
	}
	m = send(m, line(0, "others", "so the next step is PROJ-123"))
	if v := m.View(); strings.Contains(v, "▸") {
		t.Errorf("partial line kept after the committed line:\n%s", v)
	}
	if last := lastFeedLine(m); !strings.Contains(last, "PROJ-123") {
		t.Errorf("last feed line = %q, want the committed line", last)
	}
}

// lastFeedLine is the last non-blank line between the top bar and the input strip.
func lastFeedLine(m tea.Model) string {
	lines := strings.Split(m.View(), "\n")
	for i := len(lines) - 2; i > 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

func TestModelCtrlCInterruptsEachTime(t *testing.T) {
	r := &recorder{}
	m := sized(t, r, 140, 30)
	for range 2 {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatal("Ctrl+C quit by itself; the caller decides through EndedMsg")
			}
		}
	}
	if r.interrupts != 2 {
		t.Errorf("interrupts = %d, want 2", r.interrupts)
	}
}

func TestModelEndedQuits(t *testing.T) {
	_, cmd := sized(t, &recorder{}, 140, 30).Update(EndedMsg{})
	if cmd == nil {
		t.Fatal("EndedMsg returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("EndedMsg did not quit")
	}
}

func TestModelStripsEscapeSequences(t *testing.T) {
	evil := "ok\x1b]52;c;ZXZpbA==\a then\x1b[2J and\rback done\x9b"
	m := sized(t, &recorder{}, 140, 30)
	m = send(m, BoardMsg{Summary: evil, Decisions: []string{evil}},
		ItemMsg{Kind: assistant.ItemAssist, Badge: "RISK", Text: evil, Note: evil},
		line(0, "others", evil),
		StatusMsg{State: "listening", PartialSpeaker: "me", Partial: evil})
	v := m.View()
	for _, bad := range []string{"\x1b", "\a", "\r", "\x9b"} {
		if strings.Contains(v, bad) {
			t.Errorf("view contains %q:\n%q", bad, v)
		}
	}
	if !strings.Contains(v, "ok then and back done") {
		t.Errorf("text lost while cleaning:\n%s", v)
	}
}

func TestModelStatusDoesNotRewrapOldEntries(t *testing.T) {
	m := sized(t, &recorder{}, 140, 30)
	for i := range 1000 {
		m = send(m, line(time.Duration(i)*time.Second, "others", fmt.Sprintf("line number %d", i)))
	}
	f := &m.(*model).feed
	before := f.rendered
	m = send(m, StatusMsg{State: "listening", PartialSpeaker: "me", Partial: "so"}, StatusMsg{State: "listening"})
	if f.rendered != before {
		t.Errorf("status updates wrapped %d old entries again", f.rendered-before)
	}
	m = send(m, line(0, "me", "one more"))
	if f.rendered != before+1 {
		t.Errorf("a new item wrapped %d entries, want 1", f.rendered-before)
	}
	send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if f.rendered != before+1+1001 {
		t.Errorf("a resize wrapped %d entries, want all 1001", f.rendered-before-1)
	}
}

func TestModelAltBackspaceDeletesWord(t *testing.T) {
	r := &recorder{}
	m := sized(t, r, 140, 30)
	m = send(m, typed("why not Acme"), tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}, enter)
	if len(r.sent) != 1 || r.sent[0].Text != "why not" {
		t.Errorf("sent = %+v, want the question \"why not\"", r.sent)
	}
	send(m, typed("ask Sam"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true}, typed("x"), enter)
	if len(r.sent) != 2 || r.sent[1].Text != "ask xSam" {
		t.Errorf("sent = %+v, want Alt+b to move a word back", r.sent)
	}
	if len(r.toggled) != 0 {
		t.Errorf("toggled = %v, want none", r.toggled)
	}
}

func TestModelResizeToBottomReturnsToLive(t *testing.T) {
	m := sized(t, &recorder{}, 140, 30)
	for i := range 100 {
		m = send(m, line(0, "others", fmt.Sprintf("line number %d", i)))
	}
	m = send(m, tea.KeyMsg{Type: tea.KeyPgUp}, line(0, "me", "late one"))
	if !strings.Contains(m.View(), "↓ 1 new") {
		t.Fatal("no new marker")
	}
	m = send(m, tea.WindowSizeMsg{Width: 140, Height: 200})
	if v := m.View(); strings.Contains(v, "new") {
		t.Fatalf("marker stuck though the whole feed fits:\n%s", v)
	}
	m = send(m, tea.WindowSizeMsg{Width: 140, Height: 30}, line(0, "me", "later"))
	if v := m.View(); strings.Contains(v, "new") || !strings.Contains(v, "later") {
		t.Fatalf("feed not live after the resize:\n%s", v)
	}
}

func TestModelEndStatusKeepsClockAndMutes(t *testing.T) {
	m := sized(t, &recorder{}, 140, 30)
	m = send(m, StatusMsg{Elapsed: 83 * time.Second, State: "listening", Muted: map[string]bool{"me": true}}, StatusMsg{})
	top := strings.Split(m.View(), "\n")[0]
	for _, want := range []string{"01:23", "meeting over", "me (muted)", "others live"} {
		if !strings.Contains(top, want) {
			t.Errorf("top bar lacks %q: %q", want, top)
		}
	}
}
