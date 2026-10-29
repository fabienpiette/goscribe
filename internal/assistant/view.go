package assistant

import "time"

// ItemKind tells a View how to show an Item.
type ItemKind int

const (
	ItemTranscript ItemKind = iota // a committed line: Speaker, Text
	ItemAssist                     // a badge item: Badge, Text, Note, High
	ItemNotice                     // shown as is
	ItemInfo                       // dimmed notice
)

// Item is one entry of the feed.
type Item struct {
	Kind    ItemKind
	At      time.Duration
	Speaker string // ItemTranscript, and "you" for a typed question
	Badge   string // "ASK","ANSWER","MISSED","RISK","DECISION","RECAP","LIBRARIAN","LOOKUP","FIX"
	Text    string // cleaned, without icon or time
	Note    string // dim trailing note (source, why), may be ""
	High    bool   // ‼: the moment will soon pass
	Plain   string // the exact line today's output prints (with ANSI dim when interactive)
}

// Status is what the status line shows.
type Status struct {
	Elapsed                 time.Duration
	Engine                  string
	State                   string        // "listening", "thinking", "recapping"; "" at the end of the meeting
	Busy                    time.Duration // how long State has lasted, 0 when listening
	Asking                  bool          // a typed question is being answered
	Looking                 bool          // a librarian lookup is running
	Muted                   map[string]bool
	PartialSpeaker, Partial string
}

// Board is the dashboard: the memory the notes file is made from.
type Board struct {
	Summary                              string
	NotCovered, Decisions, OpenQuestions []string
	Sources                              []string
	// Fixes are the corrections in force, "cash → cache"; a suggested one not
	// answered yet ends with " ?".
	Fixes []string
	// Briefs are the user's brief texts in order; the TUI numbers them.
	Briefs []string
}

// View is everything Run shows. Calls come from the Run goroutine only.
type View interface {
	Show(Item)
	Status(Status)
	Board(Board)
}
