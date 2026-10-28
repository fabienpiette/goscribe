package assistant

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingView keeps what Run shows, for tests.
type recordingView struct {
	mu       sync.Mutex
	items    []Item
	statuses []Status
	boards   []Board
}

func (v *recordingView) Show(it Item) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.items = append(v.items, it)
}

func (v *recordingView) Status(st Status) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.statuses = append(v.statuses, st)
}

func (v *recordingView) Board(b Board) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.boards = append(v.boards, b)
}

// Items returns a copy of the items shown so far.
func (v *recordingView) Items() []Item {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]Item(nil), v.items...)
}

func TestPlainViewShowPrintsPlainAndRedrawsStatus(t *testing.T) {
	var out bytes.Buffer
	v := NewPlainView(&out, true, "", "")
	v.Status(Status{Elapsed: 72 * time.Second, Engine: "claude", State: "listening"})
	v.Show(Item{Plain: "hello"})
	got := out.String()
	if !strings.Contains(got, "\r\033[Khello\n") {
		t.Errorf("line not printed after a clear: %q", got)
	}
	i := strings.LastIndex(got, "\r\033[K● ASSIST 00:01:12")
	if i < 0 || !strings.Contains(got[i:], "· listening") || strings.Contains(got[i:], "\n") {
		t.Errorf("output must end with the status line: %q", got)
	}
}

func TestPlainViewNotInteractiveHasNoStatus(t *testing.T) {
	var out bytes.Buffer
	v := NewPlainView(&out, false, "", "")
	v.Status(Status{Elapsed: time.Second, Engine: "claude", State: "listening"})
	if out.Len() != 0 {
		t.Fatalf("status wrote %q", out.String())
	}
	v.Show(Item{Plain: "x"})
	if out.String() != "x\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestPlainViewBoardIsSilent(t *testing.T) {
	var out bytes.Buffer
	NewPlainView(&out, true, "", "").Board(Board{Summary: "s", Decisions: []string{"d"}})
	if out.Len() != 0 {
		t.Errorf("board wrote %q", out.String())
	}
}

func TestRunSendsStructuredItemsToView(t *testing.T) {
	fastTimers(t)
	v := &recordingView{}
	events := make(chan Event)
	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Engine: "fake", Interval: time.Hour, Think: (&fakeThink{}).think, View: v}, events)
	}()
	events <- line("others", "so the next step is the freeze")
	events <- Event{Kind: NoticeEvent, Text: "a notice"}
	close(events)
	<-done
	items := v.Items()
	if len(items) < 2 || items[0].Kind != ItemTranscript || items[0].Speaker != "others" || items[0].Text != "so the next step is the freeze" {
		t.Fatalf("transcript item: %+v", items)
	}
	if items[1].Kind != ItemNotice || items[1].Plain != "a notice" {
		t.Errorf("notice item: %+v", items[1])
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.boards) == 0 || len(v.statuses) == 0 {
		t.Errorf("Run must send a board and a status at start: %d, %d", len(v.boards), len(v.statuses))
	}
}
