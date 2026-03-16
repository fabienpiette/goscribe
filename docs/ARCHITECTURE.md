# Architecture

This document describes the high-level architecture of goscribe.
If you want to familiarize yourself with the codebase, you are in the right place.

## Bird's Eye View

goscribe transcribes audio files and optionally post-processes the resulting
text through AI models. It supports two AI providers (OpenAI and Google Gemini),
handles arbitrarily large files by splitting audio and chunking text, and exposes
two distinct operation modes from the same shared engine.

**CLI mode** (`cmd/goscribe`) is a synchronous, single-binary tool: feed it an
audio file, get back a transcript and any post-processed results. Everything
runs in one process, one call stack.

**Server mode** (`cmd/server`) is an HTTP API backed by a Redis job queue. A
client submits a job via `POST /jobs`, receives a job ID immediately, and polls
`GET /jobs/{id}` for results. Transcription and post-processing run
asynchronously in a worker process. The two halves (API and worker) can run
in one process (`MODE=all`) or as separate containers (`MODE=api` +
`MODE=worker`) sharing a Redis instance and an uploads volume.

```
  CLI mode                         Server mode
  ────────                         ───────────

  cmd/goscribe ──► provider ─┐     HTTP client
       │                     │          │
       ▼                     │     cmd/server (MODE=api)
   pkg/config                │          │
                             │     internal/api ──► asynq ──► Redis
                             │                                   │
                             │     cmd/server (MODE=worker)      │
                             │          │                        │
                             └──► internal/worker ◄─────────────┘
                                        │
                                   provider ──► internal/openai
                                               internal/gemini
                                               internal/util
```

Both modes share `pkg/config`, `internal/provider`, `internal/openai`,
`internal/gemini`, and `internal/util`. Neither mode is aware of the other's
entry point.

## Code Map

### `cmd/goscribe/`

CLI entry point. Parses flags, loads config via `pkg/config`, and runs the
transcription and post-processing pipeline synchronously. The `run()` function
holds all resolved state as local variables — no globals.

Key files: `main.go` (flag parsing, `run()`, `normalizeArgs()`).

### `cmd/server/`

Server entry point. Reads configuration from environment variables, connects to
Redis, and starts whichever combination of HTTP server and asynq worker the
`MODE` variable selects. Graceful shutdown drains both servers sequentially on
`SIGTERM`/`SIGINT`.

Key files: `main.go` (`main()`, `loadConfig()`, `buildHTTPServer()`,
`buildAsynqServer()`, `parseRedisAddr()`, `loadPostActions()`).

### `internal/api/`

HTTP layer for server mode. `Handler` receives an `asynq.Client` and a Redis
client at construction time; it never touches the transcription engine directly.
`SubmitJob` saves an initial result to Redis, then enqueues a `goscribe:process`
task. `GetJob` reads back the result by job ID. `ListActions` returns the loaded
`PostAction` slice.

Key files: `handler.go` (`Handler`, `SubmitJob`, `GetJob`, `ListActions`,
`Health`), `router.go` (chi router wiring).

**Architecture Invariant:** `internal/api` never imports `internal/provider`,
`internal/openai`, or `internal/gemini`. All AI work happens in the worker.

### `internal/worker/`

Async job processor for server mode. `Processor` implements the asynq handler
interface: it unmarshals a `ProcessPayload`, runs the full transcription and
post-processing pipeline via the `Transcriber` interface, writes the `JobResult`
to Redis, and optionally fires a webhook. The `Transcriber` interface is
satisfied by `RealTranscriber` in production and a mock in tests.

Key files: `tasks.go` (shared types: `ProcessPayload`, `JobResult`, status
constants, `TaskTypeProcess`), `processor.go` (`Processor`, `ProcessTask`,
`fireWebhook`, SSRF protection via `dialContextWithValidation`).

**Architecture Invariant:** `internal/worker` is the only package in server
mode that imports `internal/provider`. `internal/api` must not reach into
provider logic directly.

### `pkg/config/`

Configuration types and all config operations. This is the only `pkg/`
package, making it importable by external tools.

Exports `Config`, `PostAction`, `LoadConfig()`, `ValidateConfig()`,
`CreateDefault()`, `Reset()`, `StoreAPIKey()`, `StoreGeminiAPIKey()`,
`SetDefaultProvider()`, `DefaultPostActions()`, `FindAction()`.

The 18 built-in post-processing action templates ship as an embedded YAML
string in `defaults.go` and are compiled into the binary — users never need to
manage this file.

Key files: `config.go` (all types and operations), `defaults.go` (embedded
YAML for built-in actions).

**Architecture Invariant:** `pkg/config` has zero imports from `internal/`.
It depends only on the standard library and `gopkg.in/yaml.v3`.

### `internal/provider/`

Provider routing and cross-provider fallback. Each function accepts a provider
name string plus both API keys, delegates to the correct backend, and
transparently retries with the other provider on failure when fallback is
enabled.

Key files: `provider.go` (`TranscribeAudio()`, `ProcessChunked()`,
`SelectBestActions()`).

**Architecture Invariant:** `internal/provider` is the only package that
imports both `internal/openai` and `internal/gemini`. No other package may
import both.

### `internal/openai/`

OpenAI API client. Handles Whisper transcription (multipart upload), GPT chat
completions, chunked processing with sentence-boundary splitting, and
hierarchical merge of chunk results for very large outputs.

All HTTP traffic goes through the package-local `makeRequest()` function.
`BaseURL` is a package-level var that defaults to the production endpoint and
is overridden in tests to point at an `httptest.Server`.

Key files: `openai.go`. API request/response types are unexported.

### `internal/gemini/`

Gemini API client. Handles audio transcription (base64 inline data), text
processing, chunked processing, and merge logic. Mirrors the same chunking and
merge strategy as `internal/openai`.

Key files: `gemini.go`. `BaseURL` is overridable for tests.

### `internal/util/`

Pure utility functions with no state and no provider-specific logic. Shared by
both AI clients.

Exports: `MaxFileSizeBytes`, `AvgCharsPerToken`, `GetModelContextLimit()`,
`GetMimeType()`, `TruncateString()`, `SplitIntoSentences()`, `GetFileSize()`,
`SplitAudioFile()`, `Shellescape()`, `Max()`, `ParseRateLimitWaitTime()`.

Key files: `util.go`.

**Architecture Invariant:** `internal/util` never imports any other package in
this project. It depends only on the standard library.

## Invariants

1. **Dependency direction flows inward.** `cmd/` imports `pkg/` and `internal/`.
   `internal/worker` and `internal/api` import `pkg/config` but not each other.
   `internal/provider` imports `internal/openai`, `internal/gemini`, and
   `pkg/config`. The openai and gemini packages import `internal/util` and
   `pkg/config`. No cycles exist.

2. **No global mutable state.** `LoadConfig()` returns a value without side
   effects; `FindAction()` takes the actions slice as a parameter. The only
   package-level vars are `openai.BaseURL`, `gemini.BaseURL` (test overrides),
   and `worker.webhookClient` (shared HTTP client with a timeout and custom
   dialer).

3. **All AI API calls go through `makeRequest()`.** No direct `http.Client.Do()`
   calls exist outside each AI package's `makeRequest` function. This guarantees
   retry and rate-limit handling for all AI traffic.

4. **Job results are the source of truth for server mode.** The `JobResult`
   written to Redis is the only persistent record of a job's state. Neither
   `internal/api` nor `internal/worker` keep in-memory job state. A worker
   restart recovers cleanly: asynq re-delivers unacknowledged tasks.

5. **Webhook delivery is fire-and-forget with SSRF protection.** `fireWebhook`
   does not retry on failure. The custom `dialContextWithValidation` transport
   resolves DNS once, validates all returned IPs against private ranges at dial
   time (not before), and connects to the validated IP directly — preventing
   both DNS rebinding attacks and IPv4-mapped IPv6 bypasses.

6. **Audio splitting requires ffmpeg.** The tool shells out to `ffmpeg` via
   `exec.Command`. If ffmpeg is not installed, large-file transcription fails.
   Small files (<25 MB for OpenAI, <20 MB for Gemini) bypass splitting entirely.
   The Docker image installs ffmpeg in the runtime layer.

7. **Chunk merging is recursive but bounded.** `hierarchicalMerge` halves the
   chunk count at each level, guaranteeing O(log N) passes. It cannot
   infinite-loop because each level strictly reduces the count.

8. **Provider fallback only fires on errors, not on empty results.** A
   successful API call that returns an empty string is treated as an error
   (`"no response from API"`), which does trigger fallback.

## Cross-Cutting Concerns

### Error Handling

All errors are wrapped with `fmt.Errorf("context: %w", err)` for chain
visibility. CLI's `main()` prints the error and calls `os.Exit(1)`. The server's
HTTP handlers write JSON error objects via `writeError(w, status, msg)`. No
panics are used outside the chi `Recoverer` middleware.

### Configuration

CLI mode reads `~/.goscribe/config.yml` for post-processing actions and API
keys. Server mode is environment-variable-driven; it optionally reads a config
file path from `GOSCRIBE_CONFIG_FILE` for post-processing actions only —
API keys always come from environment variables in server deployments.

### Temporary Files

Audio chunks from splitting are written to `os.MkdirTemp("", "goscribe-chunks-*")`
and cleaned up via `defer os.RemoveAll`. Uploaded audio files in server mode are
written to `UPLOADS_DIR` (default `/tmp/goscribe-uploads`) and removed by
`defer os.Remove` in the worker after transcription. If the process is killed
mid-job, orphaned files may remain and must be cleaned up manually.

### Retry and Rate-Limit Strategy

Both `internal/openai` and `internal/gemini` use the same retry pattern: up to
3 attempts, exponential backoff (`2^attempt` seconds) for non-429 errors, and
for 429 responses, the exact wait time is parsed from the response body
(`"try again in X.XXXs"`) with a 10-second fallback.

### Testing

Tests are co-located with each package. No tests hit real APIs — both AI clients
expose an overridable `BaseURL` that tests redirect to `httptest.NewServer`.

| Package | File | What's tested |
|---|---|---|
| `pkg/config` | `config_test.go` | Loading, validation, key storage, defaults |
| `internal/util` | `util_test.go` | Model limits, MIME types, audio/string ops |
| `internal/openai` | `openai_test.go` | HTTP-mocked transcription, chunking, merge |
| `internal/gemini` | `gemini_test.go` | HTTP-mocked transcription, chunking, merge |
| `cmd/goscribe` | `main_test.go` | `run()` integration, flag parsing, normalizeArgs |
| `internal/api` | `handler_test.go` | HTTP handler behaviour with mock enqueuer/Redis |
| `internal/worker` | `processor_test.go`, `tasks_test.go` | Job processing with mock transcriber |

Key patterns: table-driven sub-tests with `t.Run`, `t.TempDir()` for filesystem
isolation, `os.Setenv("HOME", tmpDir)` to isolate config operations.

```bash
make test               # all tests, verbose
make test-coverage      # HTML coverage report
go test -v -run TestValidateConfig ./...  # single test
```

## A Typical Change

**Adding a new AI provider** (e.g., Anthropic):

1. Create `internal/anthropic/anthropic.go` with unexported API types and
   exported `TranscribeAudioWithSplitting()`, `ProcessChunked()`,
   `SelectBestActions()` functions following the same signatures as the OpenAI
   and Gemini packages. Export a `BaseURL` var for test overrides.
2. Add `case "anthropic":` branches in each function in
   `internal/provider/provider.go`. This is the only file that imports the new
   package.
3. Add `AnthropicAPIKey` to `Config` in `pkg/config/config.go`; update
   `ValidateConfig()` to accept `"anthropic"` as a valid provider type.
4. **CLI**: add `-anthropic-key` / `-set-anthropic-key` flags in
   `cmd/goscribe/main.go`.
5. **Server**: add `ANTHROPIC_API_KEY` to `serverConfig` in
   `cmd/server/main.go`, read it in `loadConfig()`, and thread it through
   `worker.Config` and `Processor`.
6. Add tests in `internal/anthropic/anthropic_test.go` using `httptest.Server`.
