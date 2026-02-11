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
│                          CLI (flag)                             │
│  Parses flags, loads config, dispatches to the right subsystem  │
└────────┬──────────────┬──────────────────┬──────────────────────┘
         │              │                  │
    ┌────▼────┐   ┌─────▼──────┐    ┌─────▼──────┐
    │ Config  │   │ Transcribe │    │   Post-    │
    │ Manager │   │  Pipeline  │    │  Process   │
    └────┬────┘   └─────┬──────┘    └─────┬──────┘
         │              │                  │
         │        ┌─────▼──────┐    ┌─────▼──────┐
         │        │   Audio    │    │  Chunked   │
         │        │  Splitter  │    │  Processor │
         │        │  (ffmpeg)  │    │  + Merger  │
         │        └─────┬──────┘    └─────┬──────┘
         │              │                  │
         │        ┌─────▼──────────────────▼──────┐
         │        │     Provider Dispatch Layer    │
         │        │  ┌─────────┐   ┌───────────┐  │
         │        │  │ OpenAI  │   │  Gemini   │  │
         │        │  │ Client  │   │  Client   │  │
         │        │  └─────────┘   └───────────┘  │
         │        │     with retry + fallback      │
         │        └────────────────────────────────┘
         │
    ┌────▼──────────────────┐
    │  ~/.goscribe/         │
    │    config.yml         │
    └───────────────────────┘
```

## Source Map

All code lives in a single `main` package, organized by concern:

| File                 | Lines | Responsibility |
|----------------------|-------|----------------|
| `main.go`            | ~450  | CLI entry point (`main`, `run`, `normalizeArgs`), flag parsing, `runOptions` |
| `types.go`           | ~85   | All struct types (OpenAI/Gemini API request/response, `Config`, `PostAction`) |
| `config.go`          | ~345  | Config loading, validation, creation, API key storage |
| `openai.go`          | ~530  | OpenAI API client, transcription, chunked processing, merge logic |
| `gemini.go`          | ~415  | Gemini API client, transcription, chunked processing, merge logic |
| `provider.go`        | ~140  | Provider dispatch with automatic fallback |
| `util.go`            | ~190  | Utilities (string ops, file ops, audio splitting, model context limits), constants, shared state |
| `default_config.go`  | ~430  | Default YAML config template with 18 built-in post-actions |
| `main_test.go`       | ~3340 | Table-driven unit tests with httptest-based API mocking |

There are no internal packages. Each file groups related functions:

## Key Design Decisions

### Single Package

The entire codebase is one `main` package. This is intentional for a CLI tool of
this size — it keeps the build simple, avoids import cycles, and makes it easy to
follow the control flow. If the project grows to support additional providers or
a library mode, extracting packages would be the natural next step.

### Provider Dispatch + Fallback

Rather than using an interface, provider selection is handled through dispatcher
functions that switch on a provider string:

```
transcribeAudioWithProvider()   → transcribeAudio() or transcribeWithGemini()
processWithProviderChunked()    → processWithOpenAIChunked() or processWithGeminiChunked()
selectBestActionsWithProvider() → selectBestActions() or selectBestActionsWithGemini()
```

Each dispatcher follows the same pattern:
1. Try primary provider
2. If it fails and fallback is enabled, try the other provider
3. If both fail, return both errors

This was chosen over an interface because the two providers have fundamentally
different APIs (multipart upload vs. base64 inline data, different auth headers,
different response shapes). A shared interface would add abstraction without
reducing complexity.

### Two-Level Large File Handling

Large inputs are handled at two independent levels, each with its own size
thresholds:

**Audio splitting** (before transcription):
- OpenAI: files >25 MB are split into 10-minute chunks via `ffmpeg -f segment`
- Gemini: files >20 MB are split into 5-minute chunks

**Transcript chunking** (before post-processing):
- Text is split at sentence boundaries (`.` `!` `?` followed by whitespace)
- Chunk size is derived from `getModelContextLimit()` minus prompt overhead
- 3-sentence overlap between chunks preserves context
- Results are merged with a dedicated AI merge prompt

If the merged result itself exceeds the model context, `hierarchicalMerge()`
pairs up results and merges them in a binary-tree pattern until a single output
remains.

### Retry + Rate Limit Strategy

Both `makeOpenAIRequest()` and `makeGeminiRequest()` use the same retry pattern:
- Up to 3 retries
- Exponential backoff: `2^attempt` seconds for non-429 errors
- For 429s: parse `try again in X.XXXs` from the response body, sleep that
  duration exactly
- Fallback to 10s if parsing fails

### Configuration as the Source of Actions

Post-processing actions are not hardcoded in Go. They are defined entirely in
`~/.goscribe/config.yml` as YAML with fields: `id`, `name`, `description`,
`type`, `prompt`, `model`, `temperature`, `max_tokens`. The 18 built-in actions
ship as a default config template in `default_config.go`.

This means:
- Users can add/modify/remove actions without recompiling
- The `type` field on each action indicates which provider's API format to use
- Validation happens at config load time (`validateConfig`) — not at action
  execution time

### Global Mutable State

`postActions` is a package-level `[]PostAction` (declared in `util.go`) that
`loadConfigActions()` writes to and `findAction()` reads from. `openAIBaseURL`
and `geminiBaseURL` are package-level vars (also in `util.go`) that default to
production endpoints but can be overridden in tests to point at `httptest.Server`
instances. Tests must set these explicitly before assertions.

## Invariants

1. **Config is always loaded before any action runs.** `main()` loads config
   unconditionally, even for `-list-actions`. No action execution path can skip
   config loading.

2. **Every API call goes through `make*Request()`.** No direct `http.Client.Do()`
   calls exist outside `makeOpenAIRequest` and `makeGeminiRequest`. This
   guarantees retry and rate-limit handling for all API traffic.

3. **Audio splitting requires ffmpeg.** The tool shells out to `ffmpeg` via
   `exec.Command("bash", "-c", ...)`. If ffmpeg is not installed, large file
   transcription will fail. Small files (<25 MB for OpenAI, <20 MB for Gemini)
   bypass splitting entirely.

4. **Chunk merging is recursive but bounded.** `hierarchicalMerge` reduces N
   chunks by half each level, guaranteeing O(log N) merge passes. It cannot
   infinite-loop because each level strictly reduces the count.

5. **Provider fallback only fires on errors, not on empty results.** A successful
   API call that returns an empty response is treated as an error
   (`"no response from API"`), which does trigger fallback.

## Cross-Cutting Concerns

### Error Handling

All errors are wrapped with `fmt.Errorf("context: %w", err)` for chain
visibility. The CLI layer (`main()`) prints the error and calls `os.Exit(1)` —
no panics are used.

### Temporary Files

Audio chunks are written to `os.MkdirTemp("", "goscribe-chunks-*")` and cleaned
up via `defer` + `os.RemoveAll`. If the process is killed mid-transcription,
orphaned temp dirs may remain in the OS temp directory.

## Testing

Tests live in `main_test.go` (~3340 lines, ~73% coverage) and cover:
- Pure function tests (string utils, flag parsing, model context limits)
- HTTP-mocked API tests via `httptest.Server` (OpenAI and Gemini request/response)
- Provider dispatch tests (success paths, fallback in both directions, both-fail)
- Chunked processing and merge logic
- Integration tests for `run()` (list actions, transcript mode, error paths)

No tests hit real APIs.

Key patterns:
- Table-driven sub-tests with `t.Run`
- `t.TempDir()` for filesystem isolation
- `overrideBaseURLs(t, openAI, gemini)` helper to redirect API calls to test servers
- `os.Setenv("HOME", tmpDir)` to isolate config operations (restored via `defer`)
- The global `postActions` slice is set directly in tests that need it

To run:
```bash
make test                                    # all tests, verbose
go test -v -run TestValidateConfig ./...     # single test
make test-coverage                           # HTML coverage report
```
