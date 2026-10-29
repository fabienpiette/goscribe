package assistant

import (
	"fmt"
	"time"
)

// userAsk is one of the user's own librarian requests: a login-walled doc
// to read or a search to run. Exactly one of Doc and Search is set.
type userAsk struct {
	Doc    string // the URL of the doc to read
	Search Lookup
}

// librarianOff says why the librarian is off. Callers of Run that never set
// Config.LibrarianOff get generic advice, naming the tool a page's host needs
// when there is one.
func (s *state) librarianOff(host string) string {
	switch {
	case s.c.LibrarianOff != "":
		return s.c.LibrarianOff
	case host != "":
		return fmt.Sprintf("add a read-only tool for %s to librarian.allowed_tools in the config", host)
	}
	return "add read-only tools to librarian.allowed_tools in the config"
}

// search handles /search: the question goes to the librarian ahead of the
// model's own lookups. It is the user's own request, so it is not in Asked
// and does not count toward MaxLookups.
func (s *state) search(text string) {
	q := cleanText(text, maxQuery)
	if q == "" {
		return
	}
	now := time.Now()
	at := s.elapsed(now)
	s.show(Item{Kind: ItemTranscript, At: at, Speaker: "you", Text: "/search " + q, Plain: "› /search " + q})
	if s.ending {
		s.info("the meeting is over: searches are closed")
		return
	}
	if s.c.Research == nil {
		s.info(fmt.Sprintf("/search needs the librarian: %s", s.librarianOff("")))
		return
	}
	s.mem.Searched = append(s.mem.Searched, q)
	s.queueUser(userAsk{Search: Lookup{Query: q, Why: "asked by the user during the meeting", At: at, Asker: "you"}})
}

// queueUser adds one of the user's requests to the FIFO queue and starts it
// if the librarian is idle. A search that has to wait says so; a doc already
// said "asking the librarian".
func (s *state) queueUser(a userAsk) {
	s.userQueue = append(s.userQueue, a)
	if s.looking && a.Doc == "" {
		s.info("🔎 waiting for the librarian: " + a.Search.Query)
	}
	s.maybeLookup(time.Now())
}
