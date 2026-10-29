package assistant

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// maxBriefs is how many briefs a meeting holds: each one is sent with every
// prompt.
const maxBriefs = 20

// brief runs a /brief command the user typed.
func (s *state) brief(r BriefRequest) {
	switch r.Op {
	case "add":
		s.addBrief(r.Text)
	case "list":
		s.listBriefs()
	case "rm":
		s.removeBrief(r.N)
	}
}

// addBrief adds a brief the user typed. Input is external, so the text is
// cleaned and checked again here. With lines to analyse, the brief asks for an
// update at once; with none, it waits for the first lines like any update.
func (s *state) addBrief(text string) {
	text = strings.TrimSpace(Clean(text))
	if err := ValidateBrief(text); err != nil {
		s.info(err.Error())
		return
	}
	if i := slices.IndexFunc(s.mem.Briefs, func(b Brief) bool { return strings.EqualFold(b.Text, text) }); i >= 0 {
		s.info(fmt.Sprintf("brief %d says that already", i+1))
		return
	}
	if len(s.mem.Briefs) >= maxBriefs {
		s.info(fmt.Sprintf("at most %d briefs: remove one with /brief rm <n>", maxBriefs))
		return
	}
	at := s.elapsed(time.Now())
	s.mem.Briefs = append(s.mem.Briefs, Brief{Text: text, At: at})
	n := len(s.mem.Briefs)
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.board()
	s.show(Item{Kind: ItemAssist, At: at, Badge: "BRIEF", Text: text, Note: fmt.Sprintf("brief %d", n),
		Plain: fmt.Sprintf("📌 %s Brief %d: %s", clock(at), n, text)})
	s.briefAsked = len(s.lines) > 0
}

// listBriefs lists the briefs in force.
func (s *state) listBriefs() {
	for i, b := range s.mem.Briefs {
		s.info(fmt.Sprintf("%d. %s", i+1, b.Text))
	}
	if len(s.mem.Briefs) == 0 {
		s.info("no briefs yet: /brief <text> adds one")
	}
}

// removeBrief takes brief n (1-based) out. It starts no update: the next one
// simply no longer carries it.
func (s *state) removeBrief(n int) {
	if n < 1 || n > len(s.mem.Briefs) {
		s.info(fmt.Sprintf("no brief %d: type /brief to list them", n))
		return
	}
	b := s.mem.Briefs[n-1]
	s.mem.Briefs = slices.Delete(s.mem.Briefs, n-1, n)
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.board()
	s.info(fmt.Sprintf("brief %d removed: %s", n, b.Text))
}

// startBriefUpdate starts the update a new brief asked for, over all the lines
// so far, whether or not new ones came since the last update. Clearing
// briefAsked here, not when the update ends, lets a failed one fall back to
// the normal backoff.
func (s *state) startBriefUpdate(now time.Time) {
	s.briefAsked = false
	upto := len(s.lines)
	instructions, transcript := CyclePrompt(s.c.Context, s.mem, s.lines[:s.analysed], s.lines[s.analysed:upto], s.c.Research != nil, s.c.Input != nil, s.unacceptedGuesses())
	s.call(now, upto, false, instructions, transcript)
}
