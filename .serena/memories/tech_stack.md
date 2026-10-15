# Tech stack

- Go 1.24 (go.mod `go 1.24.0`; CI + Dockerfile pin 1.24). Module path `goscribe` (imports are `goscribe/internal/...`).
- Deps: chi v5, hibiken/asynq, go-redis v9, google/uuid, yaml.v3; miniredis for tests.
- go.mod quirk: most direct deps are marked `// indirect` (no `go mod tidy` yet). Builds fine; a tidy produces an unrelated go.mod diff.
- No AI SDKs — raw `net/http` for OpenAI, Gemini, ElevenLabs.
- External tools by feature: ffmpeg (record; splitting files >25 MB OpenAI / >20 MB Gemini), `pactl` on PipeWire/PulseAudio (record), `whisper-ctranslate2` (transcriber local), logged-in `claude`/`codex` CLIs (harness providers). Redis for server mode.
- Docker: multi-stage, alpine runtime with ffmpeg, non-root user, both binaries built, `ENTRYPOINT ["server"]`. Compose: `docker-compose.yml` (profile `split`), `docker-compose.portainer.yml`.
- CI (`.github/workflows/ci.yml`): gofmt check, `go test -race`, golangci-lint (default config), 5-platform CLI build, docker build; on push to main auto-tags next patch version → `release.yml`.
