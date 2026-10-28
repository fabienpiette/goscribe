// Package tui is the full-screen view of goscribe assist: a top bar, the feed,
// the dashboard and a chat input.
package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"goscribe/internal/assistant"
)

// Messages the caller sends into the program.
type (
	ItemMsg   assistant.Item
	StatusMsg assistant.Status
	BoardMsg  assistant.Board
	EndedMsg  struct{} // Run returned: quit
)

// Options connects the model to the rest of goscribe. Every func is required
// and called from Update, so it must not block.
type Options struct {
	Send      func(assistant.Event) // user events into Config.Input
	Toggle    func(label string)    // "mic", "system", "recap": same labels as recorder.MuteKeys/assistKeys
	Interrupt func()                // Ctrl+C
	Sides     []string              // captured speakers, for the top bar
}

// WideWidth is the narrowest window that shows the dashboard next to the feed.
const WideWidth = 120

const dashWidth = 34

// altKeys are the toggles reachable with Alt, by key.
var altKeys = map[rune]string{'m': "mic", 's': "system", 'w': "recap"}

// keysHelp ends the /help listing.
const keysHelp = "Tab: dashboard · PgUp/PgDn, wheel: scroll · End: back to live · Shift+drag: select text to copy · Ctrl+C: end the meeting"

const helpHint = "/help "

type model struct {
	opts          Options
	width, height int
	feed          feed
	vp            viewport.Model
	in            textinput.Model
	status        assistant.Status
	started       bool // a StatusMsg arrived
	board         assistant.Board
	overlay       bool // the dashboard covers the feed (narrow windows only)
	live          bool // the feed follows new items
	unseen        int  // items added while scrolled up
}

// New returns the assist screen.
func New(o Options) tea.Model {
	in := textinput.New()
	in.Placeholder = "ask a question, or /help"
	in.Prompt = "› "
	in.PromptStyle = barStyle
	in.TextStyle = barStyle
	in.PlaceholderStyle = barDim
	in.Cursor.TextStyle = barStyle
	in.Focus()
	return &model{opts: o, vp: viewport.New(0, 0), in: in, live: true}
}

func (m *model) Init() tea.Cmd { return textinput.Blink }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.wide() {
			m.overlay = false
		}
		m.layout()
	case ItemMsg:
		m.feed.add(assistant.Item(msg))
		if !m.live {
			m.unseen++
		}
		m.refresh()
	case StatusMsg:
		st := assistant.Status(msg)
		if st.State == "" { // the meeting is over: keep its length and who was muted
			st.Elapsed, st.Muted = m.status.Elapsed, m.status.Muted
		}
		m.status, m.started = st, true
		m.feed.partialSpeaker, m.feed.partial = msg.PartialSpeaker, msg.Partial
		m.refresh()
	case BoardMsg:
		m.board = assistant.Board(msg)
	case EndedMsg:
		return m, tea.Quit
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		m.followIfAtBottom()
		return m, cmd
	case tea.KeyMsg:
		return m, m.key(msg)
	default:
		var cmd tea.Cmd
		m.in, cmd = m.in.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) key(k tea.KeyMsg) tea.Cmd {
	if k.Alt && k.Type == tea.KeyRunes { // never typed: Alt+m/s/w toggle, Alt+b/f/d edit words
		if label, ok := altKeys[firstRune(k)]; ok {
			m.toggle(label)
			return nil
		}
		if !m.editKey(k) {
			return nil
		}
	}
	switch k.Type {
	case tea.KeyCtrlC:
		m.opts.Interrupt()
	case tea.KeyTab:
		if !m.wide() {
			m.overlay = !m.overlay
		}
	case tea.KeyPgUp:
		m.vp.PageUp()
		m.followIfAtBottom()
	case tea.KeyPgDown:
		m.vp.PageDown()
		m.followIfAtBottom()
	case tea.KeyEnd:
		m.goLive()
	case tea.KeyEnter:
		m.submit()
	default:
		var cmd tea.Cmd
		m.in, cmd = m.in.Update(k)
		return cmd
	}
	return nil
}

// editKey tells whether k is one of the input's editing keys (Alt+b, Alt+f, Alt+d...).
func (m *model) editKey(k tea.KeyMsg) bool {
	km := m.in.KeyMap
	for _, b := range []key.Binding{km.WordForward, km.WordBackward, km.DeleteWordBackward, km.DeleteWordForward} {
		if key.Matches(k, b) {
			return true
		}
	}
	return false
}

func firstRune(k tea.KeyMsg) rune {
	if len(k.Runes) == 0 {
		return 0
	}
	return k.Runes[0]
}

// submit runs the input line and clears it.
func (m *model) submit() {
	input := m.in.Value()
	m.in.Reset()
	cmd, err := assistant.ParseCommand(input)
	switch {
	case errors.Is(err, assistant.ErrEmpty):
	case err != nil:
		m.info(err.Error())
	case cmd.Name == "help":
		for _, c := range assistant.Commands {
			m.info(fmt.Sprintf("%-18s %s", c.Usage, c.Summary))
		}
		m.info(keysHelp)
	case cmd.Name == "mute":
		m.toggle(map[string]string{"me": "mic", "others": "system"}[cmd.Arg])
	case cmd.Name == "recap":
		m.opts.Send(assistant.Event{Kind: assistant.RecapEvent, Window: cmd.Window})
	case cmd.Name == "fix":
		m.opts.Send(assistant.Event{Kind: assistant.FixEvent, Fix: assistant.FixRequest{
			Op: cmd.Op, Wrong: cmd.Wrong, Right: cmd.Right, N: cmd.N,
		}})
	case cmd.Name == "question":
		m.opts.Send(assistant.Event{Kind: assistant.QuestionEvent, Text: cmd.Text})
	}
	m.goLive()
}

// labelSides are the sides the mute labels toggle.
var labelSides = map[string]string{"mic": "me", "system": "others"}

// toggle passes a mute or recap label on; muting a side that is not captured
// only says so.
func (m *model) toggle(label string) {
	if side, ok := labelSides[label]; ok && !slices.Contains(m.opts.Sides, side) {
		m.info(side + " is not captured")
		m.refresh()
		return
	}
	m.opts.Toggle(label)
}

func (m *model) info(text string) {
	m.feed.add(assistant.Item{Kind: assistant.ItemInfo, Text: text})
}

func (m *model) wide() bool { return m.width >= WideWidth }

func (m *model) bodyHeight() int { return max(m.height-2, 0) }

func (m *model) feedWidth() int {
	if m.wide() {
		return m.width - dashWidth
	}
	return m.width
}

// layout sizes the feed and the input to the window.
func (m *model) layout() {
	m.vp.Width, m.vp.Height = max(m.feedWidth(), 0), m.bodyHeight()
	m.in.Width = max(m.width-lipgloss.Width(m.in.Prompt)-len(helpHint)-2, 1)
	m.refresh()
	if !m.live { // a taller or wider window can bring the bottom into view
		m.followIfAtBottom()
	}
}

// refresh rebuilds the feed content, and follows it when live.
func (m *model) refresh() {
	m.vp.SetContent(m.feed.render(m.vp.Width))
	if m.live {
		m.vp.GotoBottom()
	}
}

func (m *model) goLive() {
	m.live, m.unseen = true, 0
	m.refresh()
}

func (m *model) followIfAtBottom() {
	m.live = m.vp.AtBottom()
	if m.live {
		m.unseen = 0
	}
}

func (m *model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	parts := []string{m.topBar()}
	if h := m.bodyHeight(); h > 0 {
		parts = append(parts, m.body(h))
	}
	if m.height > 1 {
		parts = append(parts, m.inputStrip())
	}
	return strings.Join(parts, "\n")
}

func (m *model) body(h int) string {
	if m.overlay {
		return dashboard(m.board, m.width, h)
	}
	feedView := m.feedView()
	if !m.wide() {
		return feedView
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, feedView, dashboard(m.board, dashWidth, h))
}

// feedView is the viewport, with the new-items marker over its last line.
func (m *model) feedView() string {
	box := lipgloss.NewStyle().Width(m.vp.Width).Height(m.vp.Height).MaxHeight(m.vp.Height).MaxWidth(m.vp.Width)
	v := box.Render(m.vp.View())
	if m.live || m.unseen == 0 {
		return v
	}
	lines := strings.Split(v, "\n")
	lines[len(lines)-1] = newMarker(m.unseen, m.vp.Width)
	return strings.Join(lines, "\n")
}

func (m *model) topBar() string {
	left := titleStyle.Render(" goscribe assist") + barDim.Render("  "+clock(m.status.Elapsed))
	right := barStyle.Render(m.stateText())
	for _, side := range m.opts.Sides {
		if m.status.Muted[side] {
			right += barDim.Render(" · ") + mutedStyle.Render(side+" (muted)")
		} else {
			right += barDim.Render(" · ") + barStyle.Render(side+" live")
		}
	}
	right += barStyle.Render(" ")
	return bar(left, right, m.width)
}

func (m *model) stateText() string {
	st := m.status
	var s string
	switch {
	case !m.started:
		s = "starting"
	case st.State == "":
		s = "meeting over"
	case st.State == "listening":
		s = "listening"
	default:
		s = fmt.Sprintf("%s %s %ds", st.Engine, st.State, int(st.Busy.Seconds()))
	}
	if st.Asking {
		s += " · answering"
	}
	if st.Looking {
		s += " · looking up"
	}
	return s
}

func (m *model) inputStrip() string {
	return bar(m.in.View(), barDim.Render(helpHint), m.width)
}

// bar lays left and right on one tinted line of width cells; right is dropped
// when both do not fit.
func bar(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		right, gap = "", width-lipgloss.Width(left)
	}
	line := left + barStyle.Render(strings.Repeat(" ", max(gap, 0))) + right
	return barStyle.MaxWidth(width).Render(line)
}

// dashboard is the memory of the meeting, empty sections hidden. When it
// does not fit in height, the longest sections lose their last lines to a
// "… N more lines" line, down to one line each: every title stays.
func dashboard(b assistant.Board, width, height int) string {
	inner := max(width-2, 1)
	text := dashTextStyle.Width(inner)
	type section struct {
		title  string
		body   []string // wrapped lines
		hidden int      // lines cut to fit
	}
	var sections []*section
	add := func(title string, items ...string) {
		if len(items) == 0 || (len(items) == 1 && items[0] == "") {
			return
		}
		sec := &section{title: title}
		for _, it := range items {
			sec.body = append(sec.body, strings.Split(text.Render(it), "\n")...)
		}
		sections = append(sections, sec)
	}
	add("SUMMARY", clean(b.Summary))
	add("AGENDA LEFT", bullets(b.NotCovered)...)
	add("DECISIONS", bullets(b.Decisions)...)
	add("OPEN QUESTIONS", bullets(b.OpenQuestions)...)
	add("SOURCES", bullets(b.Sources)...)
	add("FIXES", bullets(b.Fixes)...)

	total := func() int {
		n := max(len(sections)-1, 0) // blank lines between sections
		for _, sec := range sections {
			n += 1 + len(sec.body)
			if sec.hidden > 0 {
				n++
			}
		}
		return n
	}
	for total() > height {
		var longest *section // the first one on ties: the summary goes first
		for _, sec := range sections {
			if len(sec.body) > 1 && (longest == nil || len(sec.body) > len(longest.body)) {
				longest = sec
			}
		}
		if longest == nil {
			break // too short a window: MaxHeight cuts the rest
		}
		longest.body = longest.body[:len(longest.body)-1]
		longest.hidden++
	}

	blocks := make([]string, len(sections))
	for i, sec := range sections {
		lines := append([]string{dashTitleStyle.Width(inner).Render(sec.title)}, sec.body...)
		if sec.hidden > 0 {
			more := fmt.Sprintf("… %d more lines", sec.hidden)
			if sec.hidden == 1 {
				more = "… 1 more line"
			}
			lines = append(lines, dashDimStyle.Width(inner).Render(more))
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return dashStyle.Width(width).MaxWidth(width).Height(height).MaxHeight(height).
		Render(strings.Join(blocks, "\n"+text.Render("")+"\n"))
}

func bullets(items []string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = "• " + clean(it)
	}
	return out
}
