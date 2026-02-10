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

All code lives in a single `main` package across three files:

| File                 | Lines | Responsibility |
|----------------------|-------|----------------|
| `main.go`            | ~2100 | Everything: CLI, config management, API clients, chunking, splitting |
| `default_config.go`  | ~430  | Default YAML config template with 18 built-in post-actions |
| `main_test.go`       | ~1130 | Unit tests (config validation, helpers, provider logic) |

There are no internal packages. Functions are organized by concern within `main.go`:

```
main.go layout (by line range):
  23-100    Type definitions (API request/response structs, Config, PostAction)
  102-122   multiStringFlag (custom flag type for -transcript)
  126-157   Constants and getModelContextLimit()
  159-577   main() + CLI flow
  579-696   Config: findAction, loadConfigActions, validateConfig, createDefaultConfig
  697-919   Config persistence: resetConfig, storeAPIKey, storeGeminiAPIKey, setDefaultProvider
  921-934   parseRateLimitWaitTime
  937-1104  API clients: makeOpenAIRequest, makeGeminiRequest (with retry)
  1106-1122 getMimeType
  1124-1373 Gemini pipeline: transcribe, split, process, chunk, merge
  1375-1509 Provider dispatchers with fallback
  1511-1568 Auto-select actions (Gemini)
  1570-1770 OpenAI pipeline: process, chunk, merge, hierarchical merge
  1772-1868 Auto-select actions (OpenAI), truncateString, splitIntoSentences
  1870-2099 OpenAI transcription: transcribeAudio, getFileSize, splitAudioFile, shellescape
```

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

`postActions` is a package-level `[]PostAction` that `loadConfigActions()` writes
to and `findAction()` reads from. This is the only piece of global mutable state.
Tests must set it explicitly before assertions.

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

Tests live in `main_test.go` and cover config validation, helper functions, and
provider-specific logic. All tests are unit-level — no integration tests hit
real APIs.

Key patterns:
- Table-driven sub-tests with `t.Run`
- `t.TempDir()` for filesystem isolation
- `os.Setenv("HOME", tmpDir)` to isolate config operations (restored via `defer`)
- The global `postActions` slice is set directly in tests that need it

To run:
```bash
make test                                    # all tests, verbose
go test -v -run TestValidateConfig ./...     # single test
make test-coverage                           # HTML coverage report
```
