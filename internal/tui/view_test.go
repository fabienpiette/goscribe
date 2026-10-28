package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"goscribe/internal/assistant"
)

// Each View call becomes the matching message for the program.
func TestNewViewSendsMessages(t *testing.T) {
	var got []tea.Msg
	v := NewView(func(m tea.Msg) { got = append(got, m) })
	it := assistant.Item{Kind: assistant.ItemNotice, Text: "hello"}
	st := assistant.Status{State: "listening", Engine: "fake"}
	b := assistant.Board{Summary: "s", Decisions: []string{"Ship on Monday"}}
	v.Show(it)
	v.Status(st)
	v.Board(b)
	want := []tea.Msg{ItemMsg(it), StatusMsg(st), BoardMsg(b)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("messages = %#v, want %#v", got, want)
	}
}
