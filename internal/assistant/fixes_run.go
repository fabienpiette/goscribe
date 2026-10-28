package assistant

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// States of a suggested fix.
const (
	guessPending  = "pending"
	guessAccepted = "accepted"
	guessRejected = "rejected"
)

// proposal is a fix the model suggested; N is its number for /fix yes|no.
type proposal struct {
	N     int
	Guess FixGuess
	At    time.Duration
	State string
}

func (p proposal) fix() Fix { return Fix{Wrong: p.Guess.Wrong, Right: p.Guess.Right} }

// fix runs a /fix command the user typed.
func (s *state) fix(r FixRequest) {
	switch r.Op {
	case "add":
		s.addFix(r.Wrong, r.Right, false)
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
// arrive later are fixed in handle. guessed marks a suggested fix the user
// accepted.
func (s *state) addFix(wrong, right string, guessed bool) {
	at := s.elapsed(time.Now())
	f := Fix{Wrong: wrong, Right: right, Guessed: guessed, At: at}
	s.mem.Fixes = append(s.mem.Fixes, f)
	if s.thinking && !s.recapping { // a recap reply does not touch the memory
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

// answerGuess answers a suggested fix with /fix yes or /fix no; n picks one,
// 0 the latest one waiting. Yes puts it in force like a typed fix.
func (s *state) answerGuess(op string, n int) {
	i := -1
	if n == 0 {
		for j := len(s.proposals) - 1; j >= 0; j-- {
			if s.proposals[j].State == guessPending {
				i = j
				break
			}
		}
	} else if n <= len(s.proposals) {
		i = n - 1
	}
	switch {
	case i < 0 && n > 0:
		s.info(fmt.Sprintf("no suggested fix %d", n))
		return
	case i < 0 && op == "yes":
		s.info("no suggested fix to accept")
		return
	case i < 0:
		s.info("no suggested fix to reject")
		return
	}
	p := &s.proposals[i]
	if p.State != guessPending {
		s.info(fmt.Sprintf("fix %d is already answered", p.N))
		return
	}
	if op == "no" {
		p.State = guessRejected
		s.board()
		s.info(fmt.Sprintf("fix %d rejected", p.N))
		return
	}
	p.State = guessAccepted
	if s.inForce(p.fix()) { // typed meanwhile: applying it twice could double it
		s.board()
		s.info(fmt.Sprintf("fix %d is already in force", p.N))
		return
	}
	s.addFix(p.Guess.Wrong, p.Guess.Right, true)
}

// takeGuess keeps the first fix a reply suggests that is valid, names a word
// said in the transcript, and is neither in force nor suggested before. Its
// text is model output, so it is cleaned first.
func (s *state) takeGuess(guesses []FixGuess, at time.Duration) (proposal, bool) {
	for _, g := range guesses {
		g = FixGuess{Wrong: strings.TrimSpace(Clean(g.Wrong)), Right: strings.TrimSpace(Clean(g.Right)),
			Why: truncate(strings.TrimSpace(Clean(g.Why)), maxItem)}
		f := Fix{Wrong: g.Wrong, Right: g.Right}
		if ValidateFix(g.Wrong, g.Right) != nil || !s.said(g.Wrong) || s.inForce(f) || s.proposed(f) {
			continue
		}
		p := proposal{N: len(s.proposals) + 1, Guess: g, At: at, State: guessPending}
		s.proposals = append(s.proposals, p)
		return p, true
	}
	return proposal{}, false
}

// showGuess shows a suggested fix with the command that accepts it.
func (s *state) showGuess(p proposal) {
	label, cmd := fixLabel(p.fix()), fmt.Sprintf("/fix yes %d", p.N)
	note, plain := cmd, fmt.Sprintf("✏️ %s Fix? %s (%s)", clock(p.At), label, cmd)
	if p.Guess.Why != "" {
		note = p.Guess.Why + " · " + cmd
		plain = fmt.Sprintf("✏️ %s Fix? %s — %s (%s)", clock(p.At), label, p.Guess.Why, cmd)
	}
	s.show(Item{Kind: ItemAssist, At: p.At, Badge: "FIX?", Text: label, Note: note, Plain: plain})
}

// said reports whether wrong is a whole word or phrase of some line.
func (s *state) said(wrong string) bool {
	for _, l := range s.lines {
		if _, n := ReplaceWord(l.Text, wrong, ""); n > 0 {
			return true
		}
	}
	return false
}

func (s *state) inForce(f Fix) bool {
	return slices.ContainsFunc(s.mem.Fixes, func(g Fix) bool { return sameFix(f, g) })
}

// proposed reports whether f was suggested before, whatever the answer: an
// answered guess is never proposed again.
func (s *state) proposed(f Fix) bool {
	return slices.ContainsFunc(s.proposals, func(p proposal) bool { return sameFix(f, p.fix()) })
}

func sameFix(a, b Fix) bool {
	return strings.EqualFold(a.Wrong, b.Wrong) && strings.EqualFold(a.Right, b.Right)
}

// answeredGuesses are the suggested fixes waiting or rejected, for the model
// not to propose them again; accepted ones are listed with the fixes in force.
func (s *state) answeredGuesses() []Fix {
	var out []Fix
	for _, p := range s.proposals {
		if p.State != guessAccepted {
			out = append(out, p.fix())
		}
	}
	return out
}

// fixReply applies fixes to a reply asked before they were made. Only the
// reply is fixed: the memory already has them, and fixing it again would
// double-apply a fix whose right side holds the wrong one.
func fixReply(r *Reply, fixes []Fix) {
	if len(fixes) == 0 {
		return
	}
	r.Summary = ApplyFixes(r.Summary, fixes)
	for _, list := range [][]string{r.Decisions, r.OpenQuestions, r.NotCovered} {
		for i := range list {
			list[i] = ApplyFixes(list[i], fixes)
		}
	}
	for i := range r.Suggestions {
		r.Suggestions[i].Text = ApplyFixes(r.Suggestions[i].Text, fixes)
	}
}
