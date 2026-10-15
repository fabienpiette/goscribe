# Suggested commands

- `make build` — CLI binary `./goscribe` (gitignored). Server: `go build ./cmd/server`.
- `make install` — builds and moves the CLI to `$(HOME)/.local/bin` (no sudo). User's `process_videos` script calls this installed binary → reinstall after CLI changes.
- `go run ./cmd/goscribe <file>`; `-list-actions`, `-transcript f.txt -action <id>`, `-transcriber local`, `-provider claude`, `--auto`, `-tags`.
- `goscribe record [-list-sources|-duration 45m -transcribe ...]`, `goscribe search <terms> [-root dir] [-since YYYY-MM-DD]` (exit 0 match, 1 none, 2 error).
- Server locally: `cp .env.example .env`, `make docker-up`, `make docker-logs`, `make docker-down`; split: `docker compose --profile split up`.
- `make test` / `make test-short` / `make test-coverage`; single: `go test -v -run TestName ./internal/worker/`.
- `make lint` — `go run golangci-lint@latest` (slow first run).
- CLI writes `~/.goscribe/config.yml` (`-set-*`, `-init` mutate it; it holds the user's real API keys) — tests must override `HOME`.
