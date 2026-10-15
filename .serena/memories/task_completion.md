# Task completion checklist (mirrors CI)

1. `gofmt -l .` must print nothing (CI fails otherwise) — fix with `go fmt ./...`.
2. `go vet ./...`
3. `go test -race ./...`
4. `make lint` (golangci-lint, default ruleset).
5. Docs drift easily; when behavior changes, update all that apply:
   - CLI flags/subcommands: flag help strings in `cmd/goscribe/*.go`, README "All flags" table + usage sections.
   - Config keys: README config example.
   - Server env vars: README server table + `.env.example`.
   - API endpoints/payloads: `internal/api/openapi.yaml` (embedded) + README endpoint table.
   - New package or changed imports: `docs/ARCHITECTURE.md` code map, invariants, testing table; README "Project Structure".
6. Dockerfile/server touched: `docker build -t goscribe:ci .`
7. CLI behavior the user relies on day to day: `make install` so `~/.local/bin/goscribe` is current.
