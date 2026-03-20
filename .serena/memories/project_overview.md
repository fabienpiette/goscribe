# Project Overview: goscribe

## Purpose
AI-powered audio transcription CLI and server. Transcribes audio files via OpenAI Whisper or Google Gemini, then optionally post-processes the transcript through 18 built-in actions (meeting summaries, action items, retrospectives, etc.) or user-defined custom actions.

## Tech Stack
- **Language**: Go 1.24
- **HTTP router**: go-chi/chi v5 (server mode)
- **Job queue**: hibiken/asynq (Redis-backed async worker)
- **Redis client**: redis/go-redis v9
- **In-process Redis for tests**: alicebob/miniredis v2
- **Config serialization**: gopkg.in/yaml.v3
- **Unique IDs**: google/uuid
- **External runtime deps**: ffmpeg (required only for audio files exceeding provider size limits)

## Two Operation Modes
- **CLI** (`cmd/goscribe`): synchronous, single binary, reads `~/.goscribe/config.yml`
- **Server** (`cmd/server`): HTTP API + async worker over Redis; configurable via env vars; supports monolith (`MODE=all`) or split `api`+`worker` containers
