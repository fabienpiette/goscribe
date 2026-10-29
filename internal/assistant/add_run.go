package assistant

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"goscribe/internal/webpage"
)

// add runs an /add command the user typed.
func (s *state) add(r AddRequest) {
	switch r.Op {
	case "add":
		if t := strings.TrimSpace(r.Target); isWebURL(t) {
			s.addPage(t)
			return
		}
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
		if errors.Is(err, errContextTooBig) {
			s.refuseBig(path)
			return
		}
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
// so it names only the path or URL the user typed, never the title or text.
func docChangedLine(d Doc) string {
	return fmt.Sprintf("The user added a source since the last update (%s): use it in the summary, lists and suggestions.", Clean(d.Origin))
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

// refuseBig says that a file or folder passes the context limit, in the
// words addDoc uses, with the size of the .md files on disk.
func (s *state) refuseBig(path string) {
	size := 0
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if p == path || strings.EqualFold(filepath.Ext(p), ".md") {
			if info, err := d.Info(); err == nil {
				size += int(info.Size())
			}
		}
		return nil
	})
	total := s.contextBytes() + size
	s.info(fmt.Sprintf("too big: %s is %d KB and the context would be %d KB (limit %d KB)",
		Clean(filepath.Base(path)), (size+1023)>>10, (total+1023)>>10, MaxContextBytes>>10))
}

const (
	maxFetches   = 3                // pages fetched at the same time
	fetchTimeout = 30 * time.Second // one fetch
)

// fetchResult is what a fetch goroutine hands back to the run loop.
type fetchResult struct {
	url  string
	page webpage.Page
	err  error
}

func isWebURL(t string) bool {
	l := strings.ToLower(t)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// addPage fetches a public page in the background; finishFetch adds it.
func (s *state) addPage(target string) {
	if s.c.Fetch == nil {
		s.info("pages cannot be added here")
		return
	}
	if s.fetching >= maxFetches {
		s.info(fmt.Sprintf("%d pages are already being fetched; wait for one", maxFetches))
		return
	}
	s.fetching++
	s.info("📎 fetching " + target + "…")
	fetch, out := s.c.Fetch, s.fetched
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		p, err := fetch(ctx, target)
		out <- fetchResult{url: target, page: p, err: err} // at most maxFetches are in flight, the buffer is bigger
	}()
}

// finishFetch adds the page a fetch returned, or says why it could not be.
func (s *state) finishFetch(r fetchResult) {
	s.fetching--
	if r.err != nil {
		var le *webpage.LoginError
		if errors.As(r.err, &le) {
			s.askLibrarianDoc(r.url, le.Reason)
			return
		}
		s.info(fmt.Sprintf("⚠ cannot add %s: %s", r.url, Clean(r.err.Error())))
		return
	}
	final := r.page.URL
	if final == "" {
		final = r.url
	}
	s.addFetched("web", r.page.Title, r.url, final, r.page.Markdown)
}

// addFetched adds a doc read from a page, by a fetch or by the librarian, and
// saves a copy in the sources folder. final is the address shown in the copy.
func (s *state) addFetched(via, rawTitle, origin, final, markdown string) {
	host := hostOf(origin)
	title := cleanText(rawTitle, maxItem)
	if title == "" {
		title = cleanText(host, maxItem)
	}
	md := CleanDoc(markdown)
	d := Doc{Name: s.pageName(rawTitle, host) + ".md", Title: title, Origin: origin, Via: via, Text: md}
	if !s.addDoc(d) {
		return
	}
	if s.c.SourcesDir == "" {
		return
	}
	body := fmt.Sprintf("# %s\n\nSource: %s\n\n%s\n", title, Clean(final), md)
	err := os.MkdirAll(s.c.SourcesDir, 0o755)
	if err == nil {
		err = os.WriteFile(filepath.Join(s.c.SourcesDir, d.Name), []byte(body), 0o644)
	}
	if err != nil && !s.copyErr {
		s.copyErr = true
		s.info(fmt.Sprintf("⚠ cannot save a copy of %s: %s", d.Name, err))
	}
}

// askLibrarianDoc handles a page that needs a login. Without the librarian it
// says how to set it up; with it, the URL joins the doc queue and maybeLookup
// starts it before any model lookup. It is the user's own request: it is not
// in Asked and does not count toward MaxLookups.
func (s *state) askLibrarianDoc(target, reason string) {
	if s.c.Research == nil {
		s.info(fmt.Sprintf("%s needs a login (%s): goscribe can't read it. Start assist with -librarian and a read-only tool for %s in librarian.allowed_tools",
			target, Clean(reason), hostOf(target)))
		return
	}
	if s.ending {
		return
	}
	if s.docURL == target || slices.Contains(s.docQueue, target) {
		s.info("already fetching " + target)
		return
	}
	s.docQueue = append(s.docQueue, target)
	s.info("📚 asking the librarian for " + target + "…")
	s.maybeLookup(time.Now())
}

// finishDoc adds the document the librarian read, or says why it could not.
func (s *state) finishDoc(r lookupResult) {
	s.showRefused(r.reply.Refused)
	d, err := DocReply{}, r.err
	if err == nil {
		d, err = ParseDocReply(r.reply.Answer)
	}
	if err != nil {
		s.notice("⚠ librarian failed: " + oneLine(err.Error(), 200))
		return
	}
	if !d.Found {
		reason := d.Reason
		if reason == "" {
			reason = "no reason given"
		}
		s.info(fmt.Sprintf("📚 the librarian can't read %s: %s; add a read-only tool for %s to librarian.allowed_tools", r.doc, reason, hostOf(r.doc)))
		return
	}
	s.addFetched("librarian", d.Title, r.doc, r.doc, d.Markdown)
}

func hostOf(target string) string {
	if u, err := url.Parse(target); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return target
}

// pageName is a file-name slug (without ".md") from the title, or the host,
// that no doc or context file already uses.
func (s *state) pageName(title, host string) string {
	slug := slugify(title)
	if slug == "" {
		slug = slugify(host)
	}
	if slug == "" {
		slug = "page"
	}
	taken := func(n string) bool {
		n += ".md"
		return slices.Contains(s.sources, n) || slices.ContainsFunc(s.mem.Docs, func(d Doc) bool { return d.Name == n })
	}
	name := slug
	for i := 2; taken(name); i++ {
		name = fmt.Sprintf("%s-%d", slug, i)
	}
	return name
}

// slugify keeps lowercase ASCII letters and digits, turns other runs into
// one "-", trims them and keeps at most 50 characters.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	out := b.String()
	if len(out) > 50 {
		out = strings.Trim(out[:50], "-")
	}
	return out
}
