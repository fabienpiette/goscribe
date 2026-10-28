# goscribe — notes for coding agents

Go CLI and server for transcribing audio and post-processing transcripts. Build and test with
`make build` / `go test ./...`; lint with `make lint` (CI runs golangci-lint, every OS build, and the
tests). Project notes for Serena live in `.serena/memories/`.

## This repository is public: no personal or company references

Everything committed here, and everything written about it on GitHub, is public. That covers code,
tests, fixtures, docs, help text, commit messages, PR titles and descriptions, and issue or review
comments. goscribe is often developed against the maintainer's real meetings, so real data leaks in
easily, mostly through examples and test fixtures copied from actual runs.

Never put any of these in the repository or on GitHub:

- names of real people (colleagues, customers, speakers in a recording)
- the maintainer's employer, its customers, vendors or partners, and their products
- ticket or issue IDs from other trackers (Linear, Jira...), and internal team, project or squad names
- real meeting titles, recording file names, transcript excerpts, summaries or tags taken from real
  meetings (topics, dates tied to a real meeting)
- hosts, IP addresses, home-directory paths, emails, channel names, URLs of internal tools

Use neutral placeholders instead, and keep them consistent:

| Kind | Use |
|---|---|
| Company or product | `Acme`, `Globex` |
| Person | `Sam`, `Alex`, `Alexis`, or a famous public figure (`Marie Curie`) |
| Ticket ID | `PROJ-123`, `PROJ-1` |
| Team or project | `billing`, `onboarding` |
| Topic or tag | `dark-mode`, `design-tokens`, `release-process`, `api-design` |
| Recording name | `team-sync-2026_10_15-...`, `meeting.mp3` |
| Paths and hosts | `~/records`, `example.com` |

When a live test against real data proves a point in a PR description, describe the result in
neutral terms ("found the missing meeting", "5 meetings across a year") rather than quoting the
data.

### Before committing or posting on GitHub

1. Read the diff (or the PR text) for anything in the list above.
2. Check it against the local denylist, which holds the real strings and is never committed (it
   lives in `.git/info/` of the main checkout; in a `git worktree`, `.git` is a file, so resolve the
   path with `git rev-parse --git-common-dir`):
   ```bash
   denylist="$(git rev-parse --git-common-dir)/info/denylist"
   test -f "$denylist" || echo "no denylist"                # an empty grep proves nothing without it
   git diff --cached | grep -i -n -f "$denylist"            # staged changes
   git grep -i -n -f "$denylist"                            # whole tree
   ```
   If the denylist doesn't exist, ask the maintainer for the terms instead of guessing.
3. If something real was already pushed, say so and stop: removing it from history means rewriting
   it (`git filter-repo`) and force-pushing, which only the maintainer decides.
