package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"goscribe/internal/tags"
	"goscribe/pkg/config"
)

const searchUsage = `goscribe search - find meetings by their -tags.yml files

USAGE:
  goscribe search [flags] [terms...]

TERMS (all must match, case-insensitive substring):
  dark-mode             in any field (summary, type, topics, people, tickets, projects)
  topic:dark-mode       only in one field: topic, person, ticket, project, type, summary

EXAMPLES:
  goscribe search dark-mode
  goscribe search tokens person:adri type:1:1
  goscribe search ticket:LIV-17445 -since 2026-09-01
  goscribe search -paths staffing | xargs grep -il hubspot

FLAGS:
`

// splitArgs parses flags wherever they appear among the terms: the standard
// flag package stops at the first non-flag argument, so parse in a loop.
func splitArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var terms []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return terms, nil
		}
		terms = append(terms, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// runSearch implements "goscribe search" and returns the process exit code:
// 0 with matches, 1 without (like grep), 2 on usage or read errors.
func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "Folder to search recursively (default: tags_root in config, else the current folder)")
	since := fs.String("since", "", "Only meetings on or after this date (YYYY-MM-DD)")
	until := fs.String("until", "", "Only meetings on or before this date (YYYY-MM-DD)")
	verbose := fs.Bool("v", false, "Show topics, people, tickets, projects and related files")
	pathsOnly := fs.Bool("paths", false, "Print only transcript paths, one per line")
	configFile := fs.String("config", "", "Config file (default: ~/.goscribe/config.yml)")
	fs.Usage = func() {
		fmt.Fprint(stderr, searchUsage)
		fs.PrintDefaults()
	}

	terms, err := splitArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	query, err := tags.ParseQuery(terms, *since, *until)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	dir := *root
	if dir == "" {
		dir = config.ReadTagsRoot(*configFile)
	}
	if dir == "" {
		dir = "."
	}

	matches, skipped, err := tags.Search(dir, query)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	for _, e := range skipped {
		fmt.Fprintf(stderr, "warning: skipped %v\n", e)
	}

	if *pathsOnly {
		for _, m := range matches {
			fmt.Fprintln(stdout, m.Transcript())
		}
	} else {
		printMatches(stdout, dir, matches, *verbose)
	}

	if len(matches) == 0 {
		return 1
	}
	return 0
}

// summaryWidth keeps one-line results readable in a terminal; -v shows the full summary.
const summaryWidth = 80

// shorten cuts s to at most n runes, ending with "…" when cut.
func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func printMatches(w io.Writer, root string, matches []tags.Match, verbose bool) {
	rel := func(path string) string {
		if r, err := filepath.Rel(root, path); err == nil {
			return r
		}
		return path
	}
	or := func(s, fallback string) string {
		if s == "" {
			return fallback
		}
		return s
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, m := range matches {
		summary := shorten(m.Tags.Summary, summaryWidth)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", or(m.Tags.Date, "????-??-??"), or(m.Tags.Type, "-"), summary, rel(m.Transcript()))
		if !verbose {
			continue
		}
		if summary != m.Tags.Summary {
			fmt.Fprintf(tw, "    summary: %s\n", m.Tags.Summary)
		}
		// Detail lines have no tabs, so in -v mode each meeting is aligned on its own.
		for _, f := range []struct {
			name   string
			values []string
		}{
			{"topics", m.Tags.Topics},
			{"people", m.Tags.People},
			{"tickets", m.Tags.Tickets},
			{"projects", m.Tags.Projects},
		} {
			if len(f.values) > 0 {
				fmt.Fprintf(tw, "    %s: %s\n", f.name, strings.Join(f.values, ", "))
			}
		}
		for _, r := range m.Related() {
			fmt.Fprintf(tw, "    file: %s\n", rel(r))
		}
		fmt.Fprintln(tw)
	}
	tw.Flush()
	fmt.Fprintf(w, "%d meeting(s) found\n", len(matches))
}
