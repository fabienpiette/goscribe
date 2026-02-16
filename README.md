# goscribe

AI-powered audio transcription with post-processing — supports OpenAI Whisper/GPT and Google Gemini with automatic fallback.

---

<p align="center">
  <img src="docs/demo.gif" alt="goscribe demo" width="800">
</p>

## Quick Start

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

## Features

- **Dual providers** — OpenAI (Whisper + GPT) and Google Gemini, with automatic fallback between them
- **18 built-in actions** — meeting summaries, action items, technical notes, retrospectives, and more
- **Auto-select mode** — let the AI pick the most relevant actions for your transcript
- **Large file handling** — audio splitting and transcript chunking happen transparently
- **Multi-transcript** — process multiple transcript files in a single run
- **Custom actions** — define your own post-processing prompts in YAML config

## Install

**Prerequisites:** Go 1.21+, ffmpeg (only for files exceeding provider size limits)

### From source

```bash
git clone https://github.com/fabienpiette/goscribe
cd goscribe
make build && sudo make install
```

### Build manually

```bash
go build -o goscribe ./cmd/goscribe
sudo mv goscribe /usr/local/bin/
```

## Usage

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

# Process multiple transcripts together
goscribe -transcript call1.txt call2.txt -action openai-meeting-summary
```

## Options

| Flag | Description |
|------|-------------|
| `-k` | OpenAI API key |
| `-gemini-key` | Gemini API key |
| `-provider` | `openai` or `gemini` |
| `-no-fallback` | Disable automatic provider fallback |
| `-action` | Action ID(s), comma-separated |
| `--auto` | AI picks the best actions |
| `-transcript` | Process existing transcript file(s) |
| `-o` | Output file name |
| `-config` | Custom config file path |
| `-list-actions` | Show all available actions |
| `-set-key` | Save OpenAI key to config |
| `-set-gemini-key` | Save Gemini key to config |
| `-set-provider` | Save default provider to config |
| `-init` | Reset config to defaults |

## Providers

**OpenAI** (default) — Whisper for transcription, GPT for post-processing. 25 MB file limit. [Get an API key](https://platform.openai.com/api-keys).

**Gemini** — native audio understanding for transcription and processing. 20 MB inline limit, up to 1M token context. [Get an API key](https://aistudio.google.com/app/apikey).

When both keys are configured, goscribe automatically falls back to the other provider on failure. Disable with `-no-fallback`.

## Built-in Actions

18 actions ship by default. Run `goscribe -list-actions` to see them all.

| Category | Actions |
|----------|---------|
| Meetings | `openai-meeting-summary` `openai-action-items` `openai-standup` `openai-one-on-one` `openai-client-meeting` |
| Technical | `openai-tech-meeting` `openai-decision-record` `openai-retrospective` `openai-incident-postmortem` |
| Business | `openai-executive-brief` `openai-project-kickoff` `openai-key-insights` |
| Learning | `openai-training-session` `openai-interview-notes` `openai-brainstorm` `openai-qa-format` |
| HR | `openai-hr-meeting` `openai-company-webinar` |

## Configuration

Config lives at `~/.goscribe/config.yml`. Define custom actions:

```yaml
provider: "openai"
openai_api_key: ""
gemini_api_key: ""
gemini_model: "gemini-2.0-flash"

post_actions:
  - id: "custom-summary"
    name: "Custom Summary"
    description: "My custom summary"
    type: "openai"          # or "gemini"
    prompt: |
      Summarize focusing on key decisions and next steps.
    model: "gpt-3.5-turbo"  # or "gemini-2.0-flash"
    temperature: 0.3
    max_tokens: 1500
```

Reset to defaults: `goscribe -init`.

## Output Files

```
<filename>-transcript.txt       Raw transcription
<filename>-<action-id>.txt      Post-processed output
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- Supported formats: mp3, mp4, mpeg, mpga, m4a, wav, webm, ogg, flac, aac, aiff

## Project Structure

```
cmd/goscribe/       CLI entry point and orchestration
pkg/config/         Config types, loading, validation (importable)
internal/provider/  Provider routing and fallback logic
internal/openai/    OpenAI API client
internal/gemini/    Gemini API client
internal/util/      Shared helpers (model limits, audio splitting, etc.)
```

## Development

```bash
make build              # standard build
make build-optimized    # smaller binary (-ldflags="-s -w")
make build-all          # cross-compile all platforms
make test               # run tests (verbose)
make test-coverage      # generate coverage.html
make run                # go run ./cmd/goscribe
```

## Acknowledgments

Thanks to all [contributors](https://github.com/fabienpiette/goscribe/graphs/contributors).

<p align="center">
<a href="https://buymeacoffee.com/fabienpiette" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" height="60"></a>
</p>

## License

[MIT](LICENSE)
