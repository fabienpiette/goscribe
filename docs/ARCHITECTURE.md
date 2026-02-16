# Architecture

This document describes the high-level architecture of goscribe.
If you want to familiarize yourself with the codebase, you are in the right place.

## Bird's Eye View

goscribe is a single-binary CLI tool that transcribes audio files and optionally
post-processes the resulting text through AI models. It supports two providers
(OpenAI and Google Gemini) and handles arbitrarily large files by splitting audio
and chunking text.

```
┌─────────────────────────────────────────────────────────────────┐
│                    cmd/goscribe (CLI)                           │
│  Parses flags, loads config, dispatches to the right subsystem  │
└────────┬──────────────┬──────────────────┬──────────────────────┘
         │              │                  │
    ┌────▼────┐   ┌─────▼──────┐    ┌─────▼──────┐
    │   pkg/  │   │ Transcribe │    │   Post-    │
    │  config │   │  Pipeline  │    │  Process   │
    └────┬────┘   └─────┬──────┘    └─────┬──────┘
         │              │                  │
         │        ┌─────▼──────────────────▼──────┐
         │        │   internal/provider            │
         │        │     routing + fallback          │
         │        └─────┬──────────────────┬───────┘
         │              │                  │
         │        ┌─────▼──────┐    ┌──────▼──────┐
         │        │  internal/ │    │  internal/  │
         │        │   openai   │    │   gemini    │
         │        └─────┬──────┘    └──────┬──────┘
         │              │                  │
         │        ┌─────▼──────────────────▼──────┐
         │        │       internal/util            │
         │        │  model limits, audio splitting │
         │        │  sentence splitting, MIME types│
         │        └────────────────────────────────┘
         │
    ┌────▼──────────────────┐
    │  ~/.goscribe/         │
    │    config.yml         │
    └───────────────────────┘
```

## Code Map

### `cmd/goscribe/`

CLI entry point. Parses flags, loads config via `pkg/config`, and orchestrates
the transcription and post-processing pipeline. Contains the `run()` function
that holds all resolved state as local variables — no globals.

Key files: `main.go` (flag parsing, `run()`, `normalizeArgs()`).

### `pkg/config/`

Configuration types and operations. This is the only `pkg/` package, meaning
it is importable by external tools.

Exports `Config`, `PostAction`, `LoadConfig()`, `ValidateConfig()`,
`CreateDefault()`, `Reset()`, `StoreAPIKey()`, `StoreGeminiAPIKey()`,
`SetDefaultProvider()`, `FindAction()`.

Key files: `config.go` (types and all config operations), `defaults.go`
(the 18 built-in action templates as a YAML string).

**Architecture Invariant:** `pkg/config` has zero imports from `internal/`.
It depends only on the standard library and `gopkg.in/yaml.v3`.

### `internal/provider/`

Provider routing and fallback. Each function takes a provider name string and
both API keys, delegates to the correct backend, and falls back to the other
provider on failure when enabled.

Key files: `provider.go` (`TranscribeAudio()`, `ProcessChunked()`,
`SelectBestActions()`).

**Architecture Invariant:** this is the only package that imports both
`internal/openai` and `internal/gemini`. No other package may import both.

### `internal/openai/`

OpenAI API client. Handles Whisper transcription (multipart upload), GPT
chat completions, chunked processing with sentence-boundary splitting, and
intelligent merge of chunk results (including hierarchical merge for very
large outputs).

Key files: `openai.go`. API types (`chatCompletionRequest`, etc.) are
unexported — only the high-level functions are exported.

`BaseURL` is a package-level var defaulting to the production endpoint,
overridable in tests to point at `httptest.Server`.

### `internal/gemini/`

Gemini API client. Handles audio transcription (base64 inline data), text
processing, chunked processing, and merge logic. Mirrors the same chunking
and merge strategy as the OpenAI package.

Key files: `gemini.go`. API types are unexported. `BaseURL` is overridable
for tests.

### `internal/util/`

Pure utility functions with no state and no provider-specific logic.

Exports: `MaxFileSizeBytes`, `AvgCharsPerToken`, `GetModelContextLimit()`,
`GetMimeType()`, `TruncateString()`, `SplitIntoSentences()`, `GetFileSize()`,
`SplitAudioFile()`, `Shellescape()`, `Max()`, `ParseRateLimitWaitTime()`.

Key files: `util.go`.

**Architecture Invariant:** `internal/util` never imports from any other
package in this project. It depends only on the standard library.

## Invariants

1. **No global mutable state.** The previous `postActions` global has been
   eliminated. `LoadConfig()` returns a `*Config` without side effects;
   `FindAction()` takes the actions slice as a parameter. The only package-level
   vars are `openai.BaseURL` and `gemini.BaseURL`, which exist solely for test
   overrides.

2. **Dependency direction flows inward.** `cmd/` imports `pkg/` and `internal/`.
   `internal/provider` imports `internal/openai`, `internal/gemini`, and
   `pkg/config`. The openai and gemini packages import `internal/util` and
   `pkg/config`. No cycles exist.

3. **Config is always loaded before any action runs.** `run()` loads config
   unconditionally, even for `-list-actions`. No action execution path can skip
   config loading.

4. **Every API call goes through `makeRequest()`.** No direct `http.Client.Do()`
   calls exist outside each package's `makeRequest` function. This guarantees
   retry and rate-limit handling for all API traffic.

5. **Audio splitting requires ffmpeg.** The tool shells out to `ffmpeg` via
   `exec.Command("bash", "-c", ...)`. If ffmpeg is not installed, large file
   transcription will fail. Small files (<25 MB for OpenAI, <20 MB for Gemini)
   bypass splitting entirely.

6. **Chunk merging is recursive but bounded.** `hierarchicalMerge` reduces N
   chunks by half each level, guaranteeing O(log N) merge passes. It cannot
   infinite-loop because each level strictly reduces the count.

7. **Provider fallback only fires on errors, not on empty results.** A successful
   API call that returns an empty response is treated as an error
   (`"no response from API"`), which does trigger fallback.

## Cross-Cutting Concerns

### Error Handling

All errors are wrapped with `fmt.Errorf("context: %w", err)` for chain
visibility. The CLI layer (`main()`) prints the error and calls `os.Exit(1)` —
no panics are used.

### Temporary Files

Audio chunks are written to `os.MkdirTemp("", "goscribe-chunks-*")` and cleaned
up via `defer` + `os.Remove`/`os.RemoveAll`. If the process is killed
mid-transcription, orphaned temp dirs may remain in the OS temp directory.

### Configuration as the Source of Actions

Post-processing actions are not hardcoded in Go. They are defined entirely in
`~/.goscribe/config.yml` as YAML with fields: `id`, `name`, `description`,
`type`, `prompt`, `model`, `temperature`, `max_tokens`. The 18 built-in actions
ship as a default config template in `pkg/config/defaults.go`.

This means:
- Users can add/modify/remove actions without recompiling
- The `type` field on each action indicates which provider's API format to use
- Validation happens at config load time (`ValidateConfig`) — not at action
  execution time

### Retry + Rate Limit Strategy

Both OpenAI and Gemini `makeRequest()` functions use the same retry pattern:
- Up to 3 retries
- Exponential backoff: `2^attempt` seconds for non-429 errors
- For 429s: parse `try again in X.XXXs` from the response body, sleep that
  duration exactly
- Fallback to 10s if parsing fails

## Testing

Tests are co-located with each package:

| Package | File | Coverage |
|---------|------|----------|
| `pkg/config` | `config_test.go` | Config loading, validation, API key storage, defaults |
| `internal/util` | `util_test.go` | Model limits, MIME types, string/file ops, rate limit parsing |
| `internal/openai` | `openai_test.go` | HTTP-mocked API tests, chunked processing, merge logic |
| `internal/gemini` | `gemini_test.go` | HTTP-mocked API tests, chunked processing, merge logic |
| `cmd/goscribe` | `main_test.go` | Integration tests for `run()`, flag parsing, normalizeArgs |

No tests hit real APIs.

Key patterns:
- Table-driven sub-tests with `t.Run`
- `t.TempDir()` for filesystem isolation
- `overrideBaseURL(t, url)` helper to redirect API calls to test servers
- `os.Setenv("HOME", tmpDir)` to isolate config operations (restored via `defer`)

To run:
```bash
make test                                    # all tests, verbose
go test -v -run TestValidateConfig ./...     # single test
make test-coverage                           # HTML coverage report
```

## A Typical Change

**Adding a new AI provider** (e.g., Anthropic):

1. Create `internal/anthropic/anthropic.go` with unexported API types and
   exported `TranscribeAudioWithSplitting()`, `ProcessChunked()`,
   `SelectBestActions()` functions following the same signatures as the OpenAI
   and Gemini packages.
2. Add a new `case "anthropic":` branch in each function in
   `internal/provider/provider.go`.
3. Add `AnthropicAPIKey` field to the `Config` struct in `pkg/config/config.go`
   and update `ValidateConfig()` to accept `"anthropic"` as a valid type.
4. Add CLI flags (`-anthropic-key`, `-set-anthropic-key`) in
   `cmd/goscribe/main.go`.
5. Add tests in `internal/anthropic/anthropic_test.go` using `httptest.Server`.
