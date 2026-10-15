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
		fmt.Fprintf(&b, "### file: %s\n\n%s\n\n", filepath.ToSlash(rel), strings.TrimSpace(string(data)))
		if b.Len() > MaxContextBytes {
			return "", nil, fmt.Errorf("context is over %d KB; pass fewer or smaller files", MaxContextBytes>>10)
		}
	}
	return strings.TrimSpace(b.String()), files, nil
}
