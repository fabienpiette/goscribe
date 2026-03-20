# Suggested Commands

## Build & Run
```bash
make build              # build CLI binary -> ./goscribe
make build-optimized    # stripped binary (ldflags -s -w)
make build-all          # cross-compile linux/darwin/windows amd64+arm64
make install            # build + sudo install to /usr/local/bin
make run                # go run ./cmd/goscribe
```

## Testing
```bash
make test               # go test -v ./...
make test-short         # go test ./...  (no verbose)
make test-coverage      # generates coverage.out + coverage.html
go test -v -run TestFunctionName ./internal/package/   # single test
```

## Linting & Formatting
```bash
make lint               # golangci-lint (runs via go run, no local install needed)
go fmt ./...            # format all code
```

## Docker / Server Mode
```bash
make docker-build       # docker build -t goscribe:local .
make docker-up          # docker compose up -d (server + Redis)
make docker-down        # docker compose down
make docker-logs        # docker compose logs -f goscribe
```

## CLI Usage (development)
```bash
./goscribe -list-actions
./goscribe -set-key YOUR_OPENAI_API_KEY
./goscribe meeting.mp3
./goscribe -action openai-meeting-summary meeting.mp3
./goscribe -provider gemini meeting.mp3
```
