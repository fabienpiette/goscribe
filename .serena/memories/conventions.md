# Conventions

## Code
- Errors wrapped `fmt.Errorf("context: %w", err)`. CLI `main()` prints + `os.Exit(1)`; HTTP handlers use `writeError(w, status, msg)` JSON. No panics.
- Engine API types unexported. `openai` and `gemini` mirror each other (TranscribeAudioWithSplitting, ProcessChunked, SelectBestActions, hierarchical merge, retry: 3 attempts, 2^n backoff, 429 wait parsed) — change one → check the other.
- External CLIs (`whisper-ctranslate2`, `claude`, `codex`, ffmpeg) wrapped behind a package-level binary-name var so tests can point at a fake shell script.
- Harness prompts carry untrusted transcript text: never give the agent tools, MCP, hooks, plugins, writable dirs or sessions.
- Commands that print data (`search`, `record`) must not go through `LoadConfig` (it prints); use the quiet `config.Read*` helpers.
- New provider/engine: follow "A Typical Change" in `docs/ARCHITECTURE.md`.
- Server config only from env vars; `GOSCRIBE_CONFIG_FILE` supplies post-actions only, never keys.

## Tests
- Co-located `_test.go`, table-driven `t.Run`, `t.TempDir()`, `HOME` overridden to tmp dir.
- No real APIs/CLIs: `BaseURL` → `httptest.NewServer`; binary vars → fake scripts. Redis via miniredis.
- openai/gemini tests take ~4–8s (real retry backoff sleeps).

## Commits
- Conventional commits (`feat:`, `fix:`, `chore:`, `refactor:`, `ci:`, `style:`, `docs:`), PR number suffix on squash merges.
