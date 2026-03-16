<!-- ============================================================
     LAYER 1: IDENTITY — What is this? (2 seconds)
     ============================================================ -->

<p align="center">
  <img src="docs/logo.png" alt="goscribe" width="200">
</p>

<h3 align="center">AI-powered audio transcription with post-processing — CLI tool and HTTP API service for homelab deployment.</h3>

---

<!-- ============================================================
     LAYER 2: PROOF — Does it actually work? (10-60 seconds)
     ============================================================ -->

<p align="center">
  <img src="docs/demo.gif" alt="goscribe demo" width="600">
</p>

## Quick Start

### CLI

```bash
# Install
git clone https://github.com/fabienpiette/goscribe && cd goscribe
make build && sudo make install

# Store your API key once
goscribe -set-key YOUR_OPENAI_API_KEY

# Transcribe
goscribe meeting.mp3

# Transcribe + summarize in one go
goscribe -action openai-meeting-summary meeting.mp3
```

### Docker

```bash
# Start the service
docker compose up -d

# Submit a job via API
curl -X POST http://localhost:8080/jobs \
  -F transcript="This is a test meeting transcript" \
  -F actions="openai-meeting-summary"

# Check job status
curl http://localhost:8080/jobs/<job-id>
```

---

<!-- ============================================================
     LAYER 3: DETAILS — I'm interested, tell me more.
     ============================================================ -->

## Features

- **Dual providers** — OpenAI (Whisper + GPT) and Google Gemini, with automatic fallback
- **18 built-in actions** — meeting summaries, action items, technical notes, retrospectives, and more
- **HTTP API** — submit transcription jobs via REST endpoints
- **Async job queue** — Redis-backed queue for processing long-running tasks
- **Webhook notifications** — get notified when jobs complete
- **Flexible deployment** — run as CLI, single container, or split API/worker

## Background

goscribe started as a CLI tool for transcribing audio files with AI-powered post-processing. The server mode extends this to a homelab-deployable HTTP service that other applications can call.

## Install

**Prerequisites:** Go 1.24+, ffmpeg (only for files exceeding provider size limits)

### CLI from source

```bash
git clone https://github.com/fabienpiette/goscribe
cd goscribe
make build && sudo make install
```

### Docker

```bash
# Quick start (all-in-one)
docker compose up -d

# Or use the Portainer-friendly version
docker compose -f docker-compose.portainer.yml up -d

# Split mode (separate API and worker containers)
docker compose --profile split up -d
```

## Usage

### CLI

```bash
# Use Gemini instead of OpenAI
goscribe -set-gemini-key YOUR_GEMINI_KEY
goscribe -provider gemini meeting.mp3

# Run multiple actions at once
goscribe -action openai-meeting-summary,openai-action-items meeting.mp3

# Let AI pick the best actions
goscribe --auto meeting.mp3

# Process an existing transcript
goscribe -transcript meeting-transcript.txt -action openai-action-items
```

### HTTP API

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/jobs` | Submit a transcription/processing job |
| GET | `/jobs/{id}` | Poll job status and retrieve result |
| GET | `/actions` | List available post-processing actions |
| GET | `/health` | Health check |

```bash
# Submit with audio file
curl -X POST http://localhost:8080/jobs \
  -F "file=@meeting.mp3" \
  -F "actions=openai-meeting-summary"

# Submit with existing transcript
curl -X POST http://localhost:8080/jobs \
  -F "transcript=Meeting notes here" \
  -F "webhook_url=https://your-callback.com/hook"

# Poll for result
curl http://localhost:8080/jobs/<job-id>
```

### Configuration

Environment variables for the server:

| Variable | Default | Description |
|----------|---------|-------------|
| `MODE` | `all` | `all`, `api`, or `worker` |
| `PORT` | `8080` | HTTP server port |
| `REDIS_URL` | `redis://redis:6379` | Redis connection |
| `OPENAI_API_KEY` | — | OpenAI API key |
| `GEMINI_API_KEY` | — | Gemini API key |
| `GOSCRIBE_PROVIDER` | `openai` | Default provider |
| `RESULT_TTL_HOURS` | `24` | Job result retention |
| `MAX_UPLOAD_MB` | `100` | Max upload size |

See `.env.example` for all options.

## Known Issues

- Large audio files (>100MB) may take significant time to process depending on provider limits
- Split-mode deployment requires a shared volume (NFS, S3, etc.) for multi-host setups

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Docker deployment](docs/docker.md)
- Supported audio formats: mp3, mp4, mpeg, mpga, m4a, wav, webm, ogg, flac, aac, aiff

## Contributing

Contributions welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Acknowledgments

Thanks to all [contributors](https://github.com/fabienpiette/goscribe/graphs/contributors).

Inspired by [OpenAI Whisper](https://openai.com/research/whisper) and [Google Gemini](https://gemini.google.com/).

## License

[MIT](LICENSE)
