# Architecture

This document describes the high-level architecture of goscribe.
If you want to familiarize yourself with the codebase, you are in the right place.

## Bird's Eye View

goscribe transcribes audio files and optionally post-processes the resulting
text through AI models. Two choices are separate:

- **Transcriber** (who turns audio into text): OpenAI Whisper, Gemini,
  ElevenLabs Scribe, or local faster-whisper.
- **Provider** (who runs post-processing actions and `--auto`): OpenAI, Gemini,
  or a local coding-agent CLI (Claude Code or Codex), called a *harness*.

It handles arbitrarily large files by splitting audio and chunking text, and
exposes two distinct operation modes from the same shared engine.

**CLI mode** (`cmd/goscribe`) is a synchronous, single-binary tool: feed it an
audio file, get back a transcript and any post-processed results. Everything
runs in one process, one call stack. The CLI also has two subcommands:
`goscribe record` (capture mic and/or computer sound to mp3, optionally
transcribing afterwards) and `goscribe search` (find meetings by the
`*-tags.yml` files that `-tags` writes).

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
   │  │  │                   │          │
   │  │  └► internal/tags    │     cmd/server (MODE=api)
   │  └───► internal/recorder│          │
   ▼                         │     internal/api ──► asynq ──► Redis
 pkg/config                  │                                   │
                             │     cmd/server (MODE=worker)      │
                             │          │                        │
                             └──► internal/worker ◄─────────────┘
                                        │
                                   provider ──► internal/openai
                                               internal/gemini
                                               internal/elevenlabs
                                               internal/whisperlocal
                                               internal/harness
                                               internal/util
```

Both modes share `pkg/config`, `internal/provider` and the engines behind it.
`internal/tags` and `internal/recorder` are CLI-only. Neither mode is aware of
the other's entry point. Server mode only uses the OpenAI and Gemini
transcribers: `-transcriber` is CLI only.

## Code Map

### `cmd/goscribe/`

CLI entry point. Parses flags, loads config via `pkg/config`, and runs the
transcription and post-processing pipeline synchronously. The `run()` function
holds all resolved state as local variables — no globals.

Key files: `main.go` (flag parsing, `run()`, `normalizeArgs()`, subcommand
dispatch), `record.go` (`goscribe record`; `watchSignals()` turns the first
Ctrl+C/SIGTERM/SIGHUP into a clean stop and a second one into a forced stop),
`search.go` (`goscribe search`; accepts flags after the search terms),
`keys.go` / `keys_linux.go` / `keys_other.go` (mute keys: the terminal reads
single key presses without echo but keeps Ctrl+C; the reader polls so it stops
and the terminal is restored before `-transcribe`).

Unknown action IDs given with `-action` are rejected before transcription, so a
typo costs no API calls.

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

`internal/api` imports `internal/worker` only for the shared task types in
`tasks.go` (`ProcessPayload`, `JobResult`, `TaskTypeProcess`).

**Architecture Invariant:** `internal/api` never imports `internal/provider` or
any engine package. All AI work happens in the worker.

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
`IsValidProvider()`, `IsValidTranscriber()`, `CreateDefault()`, `Reset()`,
`StoreAPIKey()`, `StoreGeminiAPIKey()`, `SetDefaultProvider()`,
`SetDefaultTranscriber()`, `DefaultPostActions()`, `FindAction()`,
`ReadTagsRoot()`, `RecordDefaults`, `ReadRecordDefaults()`.

`ReadTagsRoot()` and `ReadRecordDefaults()` read single keys quietly (no
validation, no printing), so `search` and `record` keep a clean stdout.

The 18 built-in post-processing action templates ship as an embedded YAML
string in `defaults.go` and are compiled into the binary — users never need to
manage this file.

Key files: `config.go` (all types and operations), `defaults.go` (embedded
YAML for built-in actions).

**Architecture Invariant:** `pkg/config` has zero imports from `internal/`.
It depends only on the standard library and `gopkg.in/yaml.v3`.

### `internal/provider/`

Engine routing and cross-provider fallback. `Transcribe()` takes
`TranscribeOptions` and dispatches on the transcriber: `local` →
`whisperlocal`, `elevenlabs` → `elevenlabs`, empty/`openai`/`gemini` →
`TranscribeAudio()`. `ProcessChunked()` and `SelectBestActions()` hand
`claude`/`codex` to `internal/harness`, and otherwise delegate to OpenAI or
Gemini, transparently retrying with the other one on failure when fallback is
enabled. `local`, `elevenlabs` and the harnesses never fall back: an explicit
engine choice fails loudly.

Key files: `provider.go` (`Transcribe()`, `TranscribeAudio()`,
`ProcessChunked()`, `SelectBestActions()`).

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

### `internal/elevenlabs/`

ElevenLabs Scribe speech-to-text client, tuned for meetings: speaker
diarization, no filler words, no audio-event tags, one paragraph per speaker
turn with its start time. Single HTTP request with a 30-minute timeout and no
retry. `BaseURL` is overridable for tests.

Key files: `elevenlabs.go` (`Options`, `Transcribe()`).

### `internal/whisperlocal/`

Offline transcription with faster-whisper through the `whisper-ctranslate2`
CLI (`exec.Command`). No audio leaves the machine. `Binary` is overridable for
tests.

Key files: `whisperlocal.go` (`Transcribe()`).

### `internal/harness/`

Runs post-processing and `--auto` through a local coding-agent CLI (`claude`
or `codex`) with that CLI's own login and default model; the action's `model`
field is ignored. The transcript is untrusted input, so the agent runs
headless from an empty temp dir with no tools, MCP servers, hooks, plugins or
saved session (Codex also gets a read-only sandbox and no web search). The
whole transcript goes in one prompt, without chunking. `ClaudeBinary` and
`CodexBinary` are overridable for tests.

Key files: `harness.go` (`IsHarness()`, `Run()`, `Process()`,
`SelectBestActions()`).

### `internal/tags/`

Builds `<name>-tags.yml` next to a transcript (`-tags`) and searches those
files (`goscribe search`). The model proposes tags as JSON; this package
normalizes them (kebab-case topics and projects, uppercase ticket IDs, a fixed
list of meeting types, sorted and de-duplicated) and writes the YAML itself,
one list item per line, so `grep -rlw` is reliable. `CollectVocabulary()`
reads topics and projects already used under the tags root so the model
reuses them.

Key files: `tags.go` (`Prompt()`, `Parse()`, `Normalize()`,
`CollectVocabulary()`, `Write()`), `search.go` (`ParseQuery()`, `Search()`).

**Architecture Invariant:** `internal/tags` imports no other project package.

### `internal/recorder/`

Records the mic and/or computer sound to mp3 with ffmpeg on
PipeWire/PulseAudio; devices are listed and resolved with `pactl`. ffmpeg's
silence events drive the "no sound from mic for 30s" warning. ffmpeg runs in
its own process group so a terminal Ctrl+C reaches goscribe only and never
interrupts ffmpeg mid-write. Each side's chain ends in a `volume@mic` /
`volume@system` filter placed after `silencedetect`; labels received on
`UI.Toggle` mute or unmute a side by sending ffmpeg a `c volume@<side> -1
volume 0|1` command on the stdin it also uses for `q`, so a muted side records
silence without looking like a dead source. On Linux, `Pdeathsig` makes the kernel stop
ffmpeg if goscribe dies, so the mic is never left recording. `Record()` keeps
its goroutine locked to one OS thread because `Pdeathsig` is tied to the
thread that started the child.

Key files: `record.go` (`Record()`), `devices.go` (`ListSources()`,
`Resolve()`), `ffmpeg.go` (`Args()`), `silence.go` (`ReadEvents()`),
`proc_linux.go` / `proc_unix.go` / `proc_other.go` (per-OS process isolation).

**Architecture Invariant:** `internal/recorder` imports no other project
package.

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
   `internal/api` imports `internal/worker` (task types only) and `pkg/config`;
   `internal/worker` imports `internal/provider` and `pkg/config`, never
   `internal/api`. `internal/provider` imports `internal/openai`,
   `internal/gemini`, `internal/elevenlabs`, `internal/whisperlocal`,
   `internal/harness`, and `pkg/config`. The openai, gemini and harness
   packages import `internal/util` and `pkg/config`; elevenlabs imports only
   `internal/util`. No cycles exist.

2. **No global mutable state.** `LoadConfig()` returns a value without side
   effects; `FindAction()` takes the actions slice as a parameter. The only
   package-level vars are test overrides (`openai.BaseURL`, `gemini.BaseURL`,
   `elevenlabs.BaseURL`, `whisperlocal.Binary`, `harness.ClaudeBinary`,
   `harness.CodexBinary`, `recorder.Binary`, `recorder.StopTimeout`,
   `recorder.Tick`, `recorder.Runner`), `recorder.MuteKeys` (the fixed
   key-to-side map for mute keys, never modified) and `worker.webhookClient`
   (shared HTTP client with a timeout and custom dialer).

3. **All OpenAI and Gemini calls go through `makeRequest()`.** No direct
   `http.Client.Do()` calls exist in those packages outside their
   `makeRequest` function. This guarantees retry and rate-limit handling for
   that traffic. ElevenLabs makes a single request without retry; the
   harnesses and whisperlocal run local CLIs, not HTTP.

4. **Job results are the source of truth for server mode.** The `JobResult`
   written to Redis is the only persistent record of a job's state. Neither
   `internal/api` nor `internal/worker` keep in-memory job state. A worker
   restart recovers cleanly: asynq re-delivers unacknowledged tasks.

5. **Webhook delivery is fire-and-forget with SSRF protection.** `fireWebhook`
   does not retry on failure. The custom `dialContextWithValidation` transport
   resolves DNS once, validates all returned IPs against private ranges at dial
   time (not before), and connects to the validated IP directly — preventing
   both DNS rebinding attacks and IPv4-mapped IPv6 bypasses.

6. **Audio splitting and recording require ffmpeg.** The tool shells out to
   `ffmpeg` via `exec.Command`. If ffmpeg is not installed, large-file
   transcription and `goscribe record` fail. Small files (<25 MB for OpenAI,
   <20 MB for Gemini) bypass splitting entirely. Recording also needs `pactl`.
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

CLI mode reads `~/.goscribe/config.yml` for post-processing actions, API
keys, default provider and transcriber, transcriber settings, `tags_root` and
`record_*` defaults. Flags override the config. Server mode is
environment-variable-driven; it optionally reads a config file path from
`GOSCRIBE_CONFIG_FILE` for post-processing actions only — API keys always come
from environment variables in server deployments.

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

Tests are co-located with each package. No tests hit real APIs or run real
CLIs: the HTTP clients expose an overridable `BaseURL` that tests redirect to
`httptest.NewServer`, and the CLI wrappers expose an overridable binary path
that tests point at a fake script.

| Package | File | What's tested |
|---|---|---|
| `pkg/config` | `config_test.go` | Loading, validation, key storage, defaults |
| `internal/util` | `util_test.go` | Model limits, MIME types, audio/string ops |
| `internal/openai` | `openai_test.go` | HTTP-mocked transcription, chunking, merge |
| `internal/gemini` | `gemini_test.go` | HTTP-mocked transcription, chunking, merge |
| `internal/elevenlabs` | `elevenlabs_test.go` | HTTP-mocked Scribe requests and transcript formatting |
| `internal/whisperlocal` | `whisperlocal_test.go` | CLI invocation and output parsing |
| `internal/harness` | `harness_test.go` | CLI invocation and isolation flags |
| `internal/tags` | `tags_test.go`, `search_test.go` | Normalization, vocabulary, YAML output, query parsing, search |
| `internal/recorder` | `devices_test.go`, `ffmpeg_test.go`, `silence_test.go`, `record_test.go`, `record_linux_test.go` | Device resolution, ffmpeg args, silence events, mute commands, stop/kill behaviour |
| `cmd/goscribe` | `main_test.go`, `search_test.go`, `record_test.go`, `record_signals_test.go`, `keys_linux_test.go` | `run()` integration, flag parsing, normalizeArgs, subcommands, signal handling, mute keys on a pseudo-terminal |
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
