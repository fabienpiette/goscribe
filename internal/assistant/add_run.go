package assistant

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// add runs an /add command the user typed.
func (s *state) add(r AddRequest) {
	switch r.Op {
	case "add":
		s.addLocal(r.Target)
	case "list":
		s.listDocs()
	}
}

// addLocal adds a local file or folder as a trusted doc. A file that is an
// assist notes file also puts its briefs and fixes back in force.
func (s *state) addLocal(target string) {
	target = strings.TrimSpace(target)
	if target == "" {
		s.info("type /add <path or url>")
		return
	}
	path, err := absPath(target)
	if err != nil {
		s.info(fmt.Sprintf("cannot add %s: %s", target, err))
		return
	}
	if s.isOwnFile(path) {
		s.info("that is this meeting's own file")
		return
	}
	text, files, err := LoadContext(path)
	if err != nil {
		s.info(fmt.Sprintf("cannot add %s: %s", path, err))
		return
	}
	base := filepath.Base(path)
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		s.info(base + " is not a text file")
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		s.info(fmt.Sprintf("cannot add %s: %s", path, err))
		return
	}
	root := filepath.Dir(path)
	if info.IsDir() {
		root = path
	}
	var names []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		names = append(names, filepath.ToSlash(rel))
	}
	d := Doc{Name: strings.Join(names, ", "), Title: Clean(base), Origin: path, Via: "file", Text: CleanDoc(text)}
	if !s.addDoc(d) {
		return
	}
	for _, n := range names {
		if !slices.Contains(s.sources, n) {
			s.sources = append(s.sources, n)
		}
	}
	if info.IsDir() {
		return
	}
	if r, ok, err := LoadRestore(path); err != nil {
		s.info(fmt.Sprintf("⚠ %s: %s; nothing restored", base, err))
	} else if ok {
		s.restore(r, base)
	}
}

// absPath expands a leading "~/" and makes the path absolute and clean.
func absPath(p string) (string, error) {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, rest)
	}
	return filepath.Abs(p)
}

// canonical is p with symbolic links resolved as far as it exists, so a link
// cannot hide the meeting's own file.
func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if dir := filepath.Dir(p); dir != p {
		return filepath.Join(canonical(dir), filepath.Base(p))
	}
	return p
}

// within says whether p is dir or inside it.
func within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isOwnFile says whether path is the meeting's own notes, transcript or
// sources folder (or inside it), or a folder holding the notes or the
// transcript: their text is written by the model, so it must never come back
// as trusted background.
func (s *state) isOwnFile(path string) bool {
	if s.c.Notes == "" {
		return false
	}
	notes, err := filepath.Abs(s.c.Notes)
	if err != nil {
		return false
	}
	path = canonical(path)
	notes = canonical(notes)
	own := []string{notes}
	if t, err := filepath.Abs(TranscriptPath(s.c.Notes)); err == nil {
		own = append(own, canonical(t))
	}
	if within(canonical(mustAbs(SourcesDir(s.c.Notes))), path) {
		return true
	}
	for _, f := range own {
		if path == f || within(path, f) {
			return true
		}
	}
	return false
}

func mustAbs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// contextBytes is the size of what every update resends: the context and the
// text of every doc added.
func (s *state) contextBytes() int { return len(s.c.Context) + docsBytes(s.mem.Docs) }

// addDoc stores a doc unless it is a duplicate or the context would pass its
// limit. It shows it, rewrites the notes and the board, and asks an update
// that carries it. It reports whether the doc was added.
func (s *state) addDoc(d Doc) bool {
	if slices.ContainsFunc(s.mem.Docs, func(o Doc) bool { return o.Origin == d.Origin }) {
		s.info("already added: " + d.Title)
		return false
	}
	before := s.contextBytes()
	if total := before + len(d.Text); total > MaxContextBytes {
		s.info(fmt.Sprintf("too big: %s is %d KB and the context would be %d KB (limit %d KB)",
			d.Title, docKB(d), (total+1023)>>10, MaxContextBytes>>10))
		return false
	}
	d.At = s.elapsed(time.Now())
	s.mem.Docs = append(s.mem.Docs, d)
	kb := docKB(d)
	s.show(Item{Kind: ItemAssist, At: d.At, Badge: "SOURCE", Text: d.Title, Note: fmt.Sprintf("%d KB", kb),
		Plain: fmt.Sprintf("📎 %s Added source: %s (%d KB)", clock(d.At), d.Title, kb)})
	if after := s.contextBytes(); before <= WarnContextBytes && after > WarnContextBytes {
		s.notice(fmt.Sprintf("⚠ the context is now %d KB: every update resends it, which is slower and costlier", (after+1023)>>10))
	}
	_ = s.writeNotes(false) // a failure is shown in the feed
	s.board()
	s.addChange(docChangedLine(d))
	s.updateAsked = len(s.lines) > 0
	return true
}

// docChangedLine tells the next update about a source. It is a trusted line,
// so only a path the user typed is named in it.
func docChangedLine(d Doc) string {
	what := "a web page"
	switch {
	case d.Trusted():
		what = Clean(d.Origin)
	case d.Via == "librarian":
		what = "a document from the librarian"
	}
	return fmt.Sprintf("The user added a source since the last update (%s): use it in the summary, lists and suggestions.", what)
}

// listDocs lists the sources added.
func (s *state) listDocs() {
	for i, d := range s.mem.Docs {
		s.info(fmt.Sprintf("%d. %s (%d KB, %s)", i+1, d.Title, docKB(d), d.Via))
	}
	if len(s.mem.Docs) == 0 {
		s.info("no sources added yet: /add <path or url> adds one")
	}
}
