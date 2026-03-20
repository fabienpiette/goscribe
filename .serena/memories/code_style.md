# Code Style & Conventions

## General
- Standard Go conventions (gofmt-formatted); no custom linter config file found
- Errors wrapped with `fmt.Errorf("context: %w", err)` for chain visibility
- No panics outside chi's `Recoverer` middleware
- No global mutable state except `openai.BaseURL`, `gemini.BaseURL` (test overrides) and `worker.webhookClient`

## Naming
- Exported functions use PascalCase: `TranscribeAudio`, `LoadConfig`, `FindAction`
- Unexported helpers use camelCase: `configDir`, `updateConfigField`, `makeRequest`
- Constants for job status as typed strings in `internal/worker/tasks.go`
- Package-level `BaseURL` var pattern in AI client packages (for test overrides)

## Error Handling
- All errors returned, never swallowed silently
- CLI `main()` prints error + `os.Exit(1)`
- HTTP handlers use `writeError(w, status, msg)` returning JSON error objects

## Testing Patterns
- Tests co-located with each package (`*_test.go`)
- No real API calls — AI clients expose `BaseURL` overridden to `httptest.NewServer`
- Table-driven subtests with `t.Run`
- `t.TempDir()` for filesystem isolation
- `os.Setenv("HOME", tmpDir)` to isolate config file operations

## Package Structure Rules (Invariants)
- `pkg/config`: only stdlib + yaml — never imports `internal/`
- `internal/util`: no internal imports at all
- `internal/provider`: only package importing both `internal/openai` and `internal/gemini`
- `internal/api`: never imports provider/openai/gemini
- `internal/worker`: only server-mode package that calls provider logic
