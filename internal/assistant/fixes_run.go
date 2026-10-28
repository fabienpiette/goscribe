package assistant

import (
	"fmt"
	"slices"
	"time"
)

// fix runs a /fix command the user typed.
func (s *state) fix(r FixRequest) {
	switch r.Op {
	case "add":
		s.addFix(r.Wrong, r.Right)
	case "list":
		s.listFixes()
	case "rm":
		s.removeFix(r.N)
	case "yes", "no":
		s.answerGuess(r.Op, r.N)
	}
}

// addFix puts a fix in force: it rewrites the lines said so far, the words in
// progress and the memory, then the transcript file and the notes. Lines that
// arrive later are fixed in handle.
func (s *state) addFix(wrong, right string) {
	at := s.elapsed(time.Now())
	f := Fix{Wrong: wrong, Right: right, At: at}
	s.mem.Fixes = append(s.mem.Fixes, f)
	if s.thinking {
		s.lateFixes = append(s.lateFixes, f)
	}
	changed := 0
	for i, l := range s.lines {
		// A case-only fix of a word already written right changes nothing.
		if text, _ := ReplaceWord(l.Text, wrong, right); text != l.Text {
			s.lines[i].Text = text
			changed++
		}
	}
	for side, p := range s.partials {
		s.partials[side], _ = ReplaceWord(p, wrong, right)
	}
	s.mem.ApplyFix(f)
	if changed > 0 && s.c.Transcript != "" {
		err := WriteTranscript(s.c.Transcript, s.header, s.lines)
		if err == nil {
			s.transcriptOn = true
		}
		s.transcriptFailed(err)
	}
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.board()
	note := fmt.Sprintf("%d lines", changed)
	if changed == 1 {
		note = "1 line"
	}
	label := fixLabel(f)
	s.show(Item{Kind: ItemAssist, At: at, Badge: "FIX", Text: label, Note: note,
		Plain: fmt.Sprintf("✏️ %s Fix: %s (%s)", clock(at), label, note)})
}

func (s *state) listFixes() {
	if len(s.mem.Fixes) == 0 {
		s.info("no fixes yet")
		return
	}
	for i, f := range s.mem.Fixes {
		s.info(fmt.Sprintf("%d. %s", i+1, fixLabel(f)))
	}
}

// removeFix takes fix n (1-based) out of force for the lines to come; what it
// already rewrote stays.
func (s *state) removeFix(n int) {
	if n < 1 || n > len(s.mem.Fixes) {
		s.info(fmt.Sprintf("no fix %d: type /fix to list them", n))
		return
	}
	f := s.mem.Fixes[n-1]
	s.mem.Fixes = slices.Delete(s.mem.Fixes, n-1, n)
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.board()
	s.info(fmt.Sprintf("fix %d removed: %s (past lines keep it)", n, fixLabel(f)))
}

// answerGuess answers a suggested fix with /fix yes or /fix no; n picks one
// when several wait. Nothing proposes fixes yet, so none is ever pending.
func (s *state) answerGuess(op string, n int) {
	switch {
	case n > 0:
		s.info(fmt.Sprintf("no suggested fix %d", n))
	case op == "yes":
		s.info("no suggested fix to accept")
	default:
		s.info("no suggested fix to reject")
	}
}
