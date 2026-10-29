package assistant

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Context size limits. Every cycle resends the context, so a big one makes
// each update slower and costlier.
const (
	WarnContextBytes = 50 << 10
	MaxContextBytes  = 200 << 10
)

// LoadContext reads the meeting background: one .md file, or every .md file
// under a folder (recursively, sorted). Each file starts with a
// "### file: <path>" header, relative to the folder. It returns the text and
// the files read.
func LoadContext(path string) (string, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	root := filepath.Dir(path)
	var files []string
	if info.IsDir() {
		root = path
		err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && p != path && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".md") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return "", nil, err
		}
		if len(files) == 0 {
			return "", nil, fmt.Errorf("no .md files in %s", path)
		}
		sort.Strings(files)
	} else {
		files = []string{path}
	}

	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return "", nil, err
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		fmt.Fprintf(&b, fileHeader+"%s\n\n%s\n\n", filepath.ToSlash(rel), strings.TrimSpace(string(data)))
		if b.Len() > MaxContextBytes {
			return "", nil, errContextTooBig
		}
	}
	return strings.TrimSpace(b.String()), files, nil
}

// errContextTooBig is what LoadContext returns when the files pass the limit.
var errContextTooBig = fmt.Errorf("context is over %d KB; pass fewer or smaller files", MaxContextBytes>>10)

const fileHeader = "### file: "

// Sources lists the context files named in a LoadContext text, as written in
// their "### file:" headers.
func Sources(context string) []string {
	var files []string
	for _, l := range strings.Split(context, "\n") {
		if name, ok := strings.CutPrefix(l, fileHeader); ok && strings.TrimSpace(name) != "" {
			files = append(files, strings.TrimSpace(name))
		}
	}
	return files
}

// Keyterm limits of ElevenLabs speech-to-text.
const (
	MaxKeyterms = 100
	// MaxKeytermLen is the realtime API's limit; longer keyterms make it
	// refuse the whole session.
	MaxKeytermLen = 20
)

// Keyterms collects the names and jargon listed on "keyterms:" lines of the
// context (in front matter or in the text), comma-separated, so live
// speech-to-text spells them as the context does. Brackets and quotes of a
// YAML list are accepted.
func Keyterms(context string) []string {
	return MergeKeyterms(nil, ParseKeyterms(context))
}

// ParseKeyterms returns every term on the "keyterms:" lines, before
// MergeKeyterms trims, dedups and drops the ones the API refuses.
func ParseKeyterms(context string) []string {
	var terms []string
	for _, l := range strings.Split(context, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(l), ":")
		if !ok || !strings.EqualFold(strings.Trim(key, " *_-"), "keyterms") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(strings.TrimLeft(value, "*_")), "[]")
		terms = append(terms, strings.Split(value, ",")...)
	}
	return terms
}

// MergeKeyterms appends extra terms to terms, trimmed, without duplicates
// (ignoring case), skipping terms too long for ElevenLabs and stopping at its
// limit.
func MergeKeyterms(terms []string, extra []string) []string {
	for _, t := range extra {
		t = strings.Trim(strings.TrimSpace(t), `"'`)
		if t == "" || len([]rune(t)) > MaxKeytermLen || len(terms) == MaxKeyterms || containsExact(terms, t) {
			continue
		}
		terms = append(terms, t)
	}
	return terms
}

// LongKeyterms lists the terms that MergeKeyterms leaves out for being longer
// than MaxKeytermLen, so the user can be told.
func LongKeyterms(terms []string) []string {
	var long []string
	for _, t := range terms {
		t = strings.Trim(strings.TrimSpace(t), `"'`)
		if len([]rune(t)) > MaxKeytermLen {
			long = append(long, t)
		}
	}
	return long
}
