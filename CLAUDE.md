# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build              # Build binary
make build-optimized    # Build with stripped symbols (-ldflags="-s -w")
make build-all          # Cross-compile: linux/darwin (amd64/arm64), windows/amd64
make test               # go test -v ./...
make test-short         # go test ./... (no verbose)
make test-coverage      # Generate coverage.out and coverage.html
go test -v -run TestFunctionName ./...  # Run a single test
```

External dependency: `ffmpeg` is required at runtime for audio splitting.

## Architecture

Single-package (`main`) CLI tool. Go source files:

- **main.go** — CLI entry point (`main`, `run`, `normalizeArgs`), flag parsing, `runOptions`
- **types.go** — All struct types (OpenAI/Gemini API request/response, `Config`, `PostAction`)
- **config.go** — Config loading, validation, creation, API key storage
- **openai.go** — OpenAI API client, transcription, chunked processing, merge logic
- **gemini.go** — Gemini API client, transcription, chunked processing, merge logic
- **provider.go** — Provider dispatch with automatic fallback
- **util.go** — Utilities (string ops, file ops, audio splitting, model context limits)
- **default_config.go** — Default YAML config template with 18 built-in post-processing actions
- **main_test.go** — Table-driven unit tests

### Provider System

Two transcription/post-processing providers: **OpenAI** (Whisper + GPT) and **Gemini**. Key dispatch pattern:

- `transcribeAudioWithProvider()` → routes to `transcribeAudio()` (OpenAI) or `transcribeAudioGemini()` (Gemini)
- `processWithProviderChunked()` → routes to provider-specific chunked processing
- `selectBestActionsWithProvider()` → AI-based action selection per provider

Providers have automatic fallback: if the primary fails, the secondary is attempted (unless `-no-fallback`).

### Chunking Strategy

Large files are handled at two levels:
1. **Audio splitting** — files >25MB (OpenAI) or >20MB (Gemini) are split via ffmpeg
2. **Transcript chunking** — text is split at sentence boundaries based on model context limits (`getModelContextLimit()`), then merged hierarchically

### Configuration

YAML config at `~/.goscribe/config.yml`. Stores API keys, provider preference, Gemini model, and custom post-processing actions. Auto-created on first run via `-init`.

## Commit Format

Follow Conventional Commits, single line only (no body or footer):

```
<type>: <description>
```

**Types:** `feat`, `fix`, `chore`, `refactor`, `docs`, `style`, `test`, `perf`, `ci`, `build`, `revert`

**Rules:**
- Subject line max 50 characters
- Imperative mood ("Add feature" not "Added feature")
- All lowercase after the type prefix, no trailing punctuation
- Be direct — describe the what/why, not the how
- Never include `Co-Authored-By` or any AI/Claude references

**Examples:**
- `feat: add container filtering by label`
- `fix: handle cgroup v2 memory stats correctly`
- `ci: vendor dependencies to fix module resolution`
- `refactor: extract label sanitization into helper`

## Conventions

- Before a commit and push: always check if the README and `goscribe -h` output are up to date
- Tests use `t.TempDir()` for isolation and table-driven sub-tests
- Only external dependency is `gopkg.in/yaml.v3`; everything else uses stdlib
