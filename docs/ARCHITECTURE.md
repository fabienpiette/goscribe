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
runs in one process, one call stack. Song mode (`-song`) extends this by
first extracting vocals with demucs, then running the standard pipeline, then
validating the resulting lyrics with an AI provider.

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
       │           song      │          │
       ▼                     │     cmd/server (MODE=api)
   pkg/config                │          │
   pkg/lyrics                │     internal/api ──► asynq ──► Redis
                             │                                   │
                             │     cmd/server (MODE=worker)      │
                             │          │                        │
                             └──► internal/worker ◄─────────────┘
                                        │
                                   provider ──► internal/openai
                                   song    ──► internal/gemini
                                               internal/util
```

## Code Map

### `cmd/goscribe/`

CLI entry point. Parses flags, loads config via `pkg/config`, and runs the
transcription and post-processing pipeline synchronously. The `run()` function
holds all resolved state as local variables — no globals. When `-song` is set,
`run()` calls `song.ExtractVocals` (or an injected `vocalExtractor` for tests),
reassigns `audioPath` to the extracted vocals, runs the normal transcription
pipeline, then calls `song.ValidateLyrics` and writes the result to
`<basename>-lyrics-validation.json` alongside the original audio file.

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
`PostAction` slice. The `song` form field is read and forwarded into
`ProcessPayload`; the handler itself performs no song-mode logic.

Key files: `handler.go` (`Handler`, `SubmitJob`, `GetJob`, `ListActions`,
`Health`), `router.go` (chi router wiring), `openapi.yaml` (OpenAPI 3.0 spec,
embedded via `//go:embed` in `swagger.go`).

**Architecture Invariant:** `internal/api` never imports `internal/provider`,
`internal/openai`, `internal/gemini`, or `internal/song`. All AI work happens
in the worker.

### `internal/worker/`

Async job processor for server mode. `Processor` implements the asynq handler
interface: it unmarshals a `ProcessPayload`, runs the full transcription and
post-processing pipeline via the `Transcriber` interface, writes the `JobResult`
to Redis, and optionally fires a webhook. When `payload.Song` is true, it calls
the injected `VocalExtractor` (defaults to `song.ExtractVocals`), substitutes
the vocals path for transcription, and calls `Transcriber.ValidateLyrics` after
the transcript is ready. Validation failure is non-fatal: the job still completes
with `status=completed` and a nil `LyricsValidation` field.

The `Transcriber` interface is satisfied by `RealTranscriber` in production and
a mock in tests. `EnableFallback` on `Config` controls provider fallback for all
AI calls (transcription, post-processing, and lyrics validation).

Key files: `tasks.go` (shared types: `ProcessPayload`, `JobResult`, status
constants, `TaskTypeProcess`), `processor.go` (`Processor`, `ProcessTask`,
`fireWebhook`, SSRF protection via `dialContextWithValidation`).

**Architecture Invariant:** `internal/worker` is the only package in server
mode that calls provider logic. `internal/api` must not reach into provider
logic directly.

### `pkg/config/`

Configuration types and all config operations. This is the only `pkg/`
package alongside `pkg/lyrics`, making it importable by external tools.

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

### `pkg/lyrics/`

Shared types for the lyrics validation result. Lives in `pkg/` rather than
`internal/song/` so that `internal/worker` can reference `LyricsValidation`
without creating a transitive import from the worker to the song package.

Exports `LyricsValidation`, `CoherenceIssue`, `StructureAnalysis`,
`SemanticConsistency`, `SuspectedError`.

Key files: `lyrics.go`.

**Architecture Invariant:** `pkg/lyrics` has zero imports from `internal/`.
It depends only on the standard library.

### `internal/song/`

Song-mode support: vocal extraction via demucs and AI-powered lyrics validation.
`ExtractVocals` shells out to the `demucs` binary, writes output to a temporary
directory, and returns the `vocals.wav` path plus a cleanup function.
`ValidateLyrics` sends the transcript to the selected AI provider using the
embedded `validationPrompt` constant; it calls the AI client packages directly
(not through `internal/provider`) to avoid double-injecting the transcript into
the prompt.

Key files: `song.go` (`ExtractVocals`, `ValidateLyrics`, `validationPrompt`).

### `internal/provider/`

Provider routing and cross-provider fallback. Each function accepts a provider
name string plus both API keys, delegates to the correct backend, and
transparently retries with the other provider on failure when fallback is
enabled.

Key files: `provider.go` (`TranscribeAudio()`, `ProcessChunked()`,
`SelectBestActions()`).

**Architecture Invariant:** `internal/provider` is the standard path for
provider-routing with fallback. Both `internal/provider` and `internal/song`
import the AI client packages directly; `internal/song` does so because
`ProcessChunked` would double-inject the transcript (it always appends the
transcript to the prompt, but `ValidateLyrics` embeds the lyrics in the prompt
itself). No other package may import both AI client packages.

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
   `pkg/config`. `internal/song` imports `internal/openai`, `internal/gemini`,
   and `pkg/lyrics`. The openai and gemini packages import `internal/util` and
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

7. **Song mode requires demucs.** `ExtractVocals` hard-fails if `demucs` is not
   on PATH. The standard Docker image (`Dockerfile`) does not include demucs;
   use `Dockerfile.song` for song-capable deployments.

8. **Song mode validation is non-fatal.** If `ValidateLyrics` returns an error,
   the job (server mode) or CLI run still completes successfully. The transcript
   is always written; `LyricsValidation` is nil/absent when validation fails.

9. **Chunk merging is recursive but bounded.** `hierarchicalMerge` halves the
   chunk count at each level, guaranteeing O(log N) passes. It cannot
   infinite-loop because each level strictly reduces the count.

10. **Provider fallback only fires on errors, not on empty results.** A
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
`defer os.Remove` in the worker after transcription. Song mode creates a separate
temp directory (`goscribe-demucs-*`) for demucs output; the caller's cleanup
function removes it via `defer cleanup()`. If the process is killed mid-job,
orphaned files may remain and must be cleaned up manually.

### Retry and Rate-Limit Strategy

Both `internal/openai` and `internal/gemini` use the same retry pattern: up to
3 attempts, exponential backoff (`2^attempt` seconds) for non-429 errors, and
for 429 responses, the exact wait time is parsed from the response body
(`"try again in X.XXXs"`) with a 10-second fallback.

### Testing

Tests are co-located with each package. No tests hit real APIs — both AI clients
expose an overridable `BaseURL` that tests redirect to `httptest.NewServer`.
Song-mode tests use a fake `demucs` shell script written to a temp dir, injected
via `PATH` override or the `vocalExtractor` function field on `runOptions`/`worker.Config`.

| Package | File | What's tested |
|---|---|---|
| `pkg/config` | `config_test.go` | Loading, validation, key storage, defaults |
| `pkg/lyrics` | `lyrics_test.go` | JSON round-trip, float score unmarshalling |
| `internal/util` | `util_test.go` | Model limits, MIME types, audio/string ops |
| `internal/openai` | `openai_test.go` | HTTP-mocked transcription, chunking, merge |
| `internal/gemini` | `gemini_test.go` | HTTP-mocked transcription, chunking, merge |
| `internal/song` | `song_test.go` | Fake-demucs extraction, HTTP-mocked validation |
| `cmd/goscribe` | `main_test.go` | `run()` integration, flag parsing, song pipeline |
| `internal/api` | `handler_test.go` | HTTP handler behaviour with mock enqueuer/Redis |
| `internal/worker` | `processor_test.go`, `tasks_test.go` | Job processing, song pipeline with mock transcriber |

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
6. **Song mode**: add the `case "anthropic":` branch in `internal/song/song.go`
   `ValidateLyrics` switch so lyrics validation works with the new provider.
7. Add tests in `internal/anthropic/anthropic_test.go` using `httptest.Server`.
