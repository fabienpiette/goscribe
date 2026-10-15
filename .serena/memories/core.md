# goscribe — core

Audio transcription + AI post-processing. Two independent choices: **transcriber** (audio→text: openai, gemini, elevenlabs, local faster-whisper) and **provider** (actions + `--auto`: openai, gemini, or harness claude/codex = local agent CLI). CLI subcommands `record` and `search`. Server mode (HTTP + Redis queue) uses only openai/gemini transcription.
Authoritative design doc: `docs/ARCHITECTURE.md` (code map, numbered invariants, "adding a provider" recipe). Read it before structural changes; keep it in sync when package boundaries change.

## Source map
- `cmd/goscribe/` — CLI. `main.go`: flags, `run()` (all state local), subcommand dispatch; `record.go` (`watchSignals`: 1st Ctrl+C clean stop, 2nd force); `search.go` (flags allowed after terms).
- `cmd/server/` — env-driven HTTP API + asynq worker; `MODE=all|api|worker`.
- `internal/api/` — chi handlers; imports `internal/worker` for task types only. `openapi.yaml` is `//go:embed`ed → edit with handler changes.
- `internal/worker/` — asynq `Processor`, `Transcriber` interface, SSRF-safe webhook.
- `internal/provider/` — routing + fallback: `Transcribe()` (by transcriber), `TranscribeAudio/ProcessChunked/SelectBestActions` (by provider; claude/codex → harness). Only package importing both openai and gemini. No tests.
- Engines: `internal/openai`, `internal/gemini` (mirrored; HTTP via `makeRequest()` with retry), `internal/elevenlabs` (single request, no retry), `internal/whisperlocal` (exec `whisper-ctranslate2`), `internal/harness` (exec `claude`/`codex`, no tools, empty temp dir — transcript is untrusted input).
- `internal/tags/` — `-tags` YAML files + `search`; standalone (no project imports).
- `internal/recorder/` — ffmpeg + `pactl` recording; standalone. Per-OS `proc_*.go`: own process group, Linux `Pdeathsig` (needs `LockOSThread`).
- `internal/util/` — stdlib-only helpers; ffmpeg audio splitting.
- `pkg/config/` — only `pkg/`; zero `internal/` imports. 18 built-in actions as embedded YAML in `defaults.go`. `ReadTagsRoot`/`ReadRecordDefaults` read quietly (keep stdout clean).

## Invariants (convention, not tooling)
- `internal/api` never imports provider or engines; AI work only in worker.
- `local`, `elevenlabs` and harnesses never fall back to another engine.
- Package-level mutable vars are only test overrides (`BaseURL`, `Binary`, `ClaudeBinary`/`CodexBinary`) + `worker.webhookClient`.
- OpenAI key `""` or `"XXXX"` (legacy placeholder, still passed by `record -transcribe`) means "not set".
- `-action` IDs are validated before transcription (no paid calls on typos); `--auto` picks are checked per action later.

## Related memories
- Language/deps/external tools: `mem:tech_stack`
- Build/run/install/docker commands: `mem:suggested_commands`
- Code style, error handling, test fakes: `mem:conventions`
- Pre-commit checks mirroring CI + doc sync list: `mem:task_completion`
