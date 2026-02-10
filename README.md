# goscribe

A command-line tool that transcribes audio files and optionally post-processes the result using AI. It supports both OpenAI (Whisper + GPT) and Google Gemini, and can fall back from one to the other if a request fails.

## Installation

From source:

```bash
git clone https://github.com/fabienpiette/goscribe
cd transcript
make build
sudo make install
```

Or build manually:

```bash
go build -o goscribe
mv goscribe /usr/local/bin/
```

## Quick start

Store your API key so you don't have to pass it every time:

```bash
goscribe -set-key YOUR_OPENAI_API_KEY
goscribe -set-gemini-key YOUR_GEMINI_API_KEY
```

Optionally pick a default provider (OpenAI is the default):

```bash
goscribe -set-provider gemini
```

Transcribe an audio file:

```bash
goscribe meeting.mp3
goscribe -provider gemini meeting.mp3
```

Transcribe and run a post-processing action in one go:

```bash
goscribe -action openai-meeting-summary meeting.mp3
```

Run actions on an existing transcript:

```bash
goscribe -transcript meeting-transcript.txt -action openai-action-items
```

Combine multiple actions:

```bash
goscribe -action openai-meeting-summary,openai-action-items meeting.mp3
```

Let the AI pick the most relevant actions automatically:

```bash
goscribe --auto meeting.mp3
```

## Options

| Flag | Description |
|------|-------------|
| `-k` | OpenAI API key (or use config file) |
| `-gemini-key` | Gemini API key (or use config file) |
| `-provider` | `openai` or `gemini` (default: from config, or openai) |
| `-no-fallback` | Disable automatic fallback to the other provider |
| `-action` | Post-processing action ID(s), comma-separated |
| `--auto` | Let AI pick the best actions for the transcript |
| `-transcript` | Process an existing transcript file instead of recording |
| `-o` | Output file name |
| `-config` | Path to a custom config file |
| `-list-actions` | Print all available actions |
| `-set-key` | Save OpenAI API key to config |
| `-set-gemini-key` | Save Gemini API key to config |
| `-set-provider` | Save default provider to config |
| `-init` | Reset config to defaults |

## Providers

### OpenAI (default)

Uses the Whisper API for transcription and GPT models (gpt-3.5-turbo, gpt-4, gpt-4-turbo, gpt-4o) for post-processing. File size limit is 25 MB per file. Get an API key at https://platform.openai.com/api-keys.

### Gemini

Uses Gemini's native audio understanding for transcription and Gemini models (gemini-2.0-flash, gemini-1.5-pro, gemini-1.5-flash) for post-processing. File size limit is 20 MB inline; larger files are split automatically. Context window goes up to 1M tokens. Get an API key at https://aistudio.google.com/app/apikey.

### Fallback

When both API keys are configured, goscribe automatically tries the other provider if the first one fails (rate limits, network errors, etc.). This happens transparently. To disable it:

```bash
goscribe -no-fallback meeting.mp3
```

## Built-in actions

There are 18 built-in post-processing actions. Run `goscribe -list-actions` to see them all.

**Meetings:** `openai-meeting-summary`, `openai-action-items`, `openai-standup`, `openai-one-on-one`, `openai-client-meeting`

**Technical:** `openai-tech-meeting`, `openai-decision-record`, `openai-retrospective`, `openai-incident-postmortem`

**Business:** `openai-executive-brief`, `openai-project-kickoff`, `openai-key-insights`

**Learning and analysis:** `openai-training-session`, `openai-interview-notes`, `openai-brainstorm`, `openai-qa-format`

**HR:** `openai-hr-meeting`, `openai-company-webinar`

## Configuration

Config lives at `~/.goscribe/config.yml`. Here's what a full config looks like:

```yaml
provider: "openai"

openai_api_key: "your-openai-key-here"
gemini_api_key: "your-gemini-key-here"

gemini_model: "gemini-2.0-flash"

post_actions:
  - id: "custom-summary"
    name: "Custom Summary"
    description: "My custom summary action"
    type: "openai"       # or "gemini"
    prompt: |
      Summarize this transcript focusing on:
      - Key decisions
      - Action items
      - Next steps
    model: "gpt-3.5-turbo" # or "gemini-2.0-flash"
    temperature: 0.3
    max_tokens: 1500
```

Reset to defaults with `goscribe -init`.

## Large file handling

### Audio splitting

Both providers have a file size limit (25 MB for OpenAI, 20 MB for Gemini). When a file exceeds the limit, goscribe splits it into chunks with ffmpeg, transcribes each chunk, and merges the results. Temporary files are cleaned up automatically.

### Long transcript chunking

When a transcript is too long for the model's context window, goscribe splits it on sentence boundaries with some overlap, processes each chunk, and then uses the AI to merge the partial results into a coherent output. It knows each model's limits (GPT-4 at 6K tokens, GPT-4-turbo at 100K, Gemini 1.5/2.0 at 900K) and picks the right chunk size.

## Rate limit handling

Failed requests are retried with exponential backoff. When the API response includes a retry-after header, goscribe respects it. There are also small delays between chunk processing to stay within rate limits. If retries are exhausted and a second provider is configured, it falls back automatically.

## Output files

- `<filename>-transcript.txt` -- raw transcription
- `<filename>-<action-id>.txt` -- post-processed output

## Requirements

- Go 1.21+
- An OpenAI and/or Gemini API key
- ffmpeg (only needed for files that exceed the size limit)
- Supported formats: mp3, mp4, mpeg, mpga, m4a, wav, webm, ogg, flac, aac, aiff

## Development

```bash
make build              # standard build
make build-optimized    # smaller binary
make build-all          # all platforms
make test               # run tests (verbose)
make test-short         # run tests (quiet)
make test-coverage      # generate coverage.html
```

## License

MIT. See `LICENSE`.
