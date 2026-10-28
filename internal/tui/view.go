package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"goscribe/internal/assistant"
)

// view turns what Run shows into messages for the program.
type view struct{ send func(tea.Msg) }

// NewView returns the assistant.View of the full-screen program; send is
// usually its Send, which is safe from any goroutine.
func NewView(send func(tea.Msg)) assistant.View { return view{send} }

func (v view) Show(it assistant.Item)     { v.send(ItemMsg(it)) }
func (v view) Status(st assistant.Status) { v.send(StatusMsg(st)) }
func (v view) Board(b assistant.Board)    { v.send(BoardMsg(b)) }
