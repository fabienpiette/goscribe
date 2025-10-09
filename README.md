# goscribe

AI-powered audio transcription tool with OpenAI Whisper and intelligent post-processing actions.

## Features

- 🎙️ **Audio Transcription** - Convert audio files to text using OpenAI Whisper
- 🤖 **AI Post-Processing** - 18 built-in actions for summarizing, extracting action items, and more
- 📝 **Process Existing Transcripts** - Apply actions to existing transcript files
- ⚙️ **Configurable** - Customize actions via YAML configuration
- 🔑 **API Key Management** - Store your OpenAI API key in config file

## Installation

### From Source

```bash
git clone https://github.com/fabienpiette/goscribe
cd transcript
make build
sudo make install
```

### Manual Build

```bash
go build -o goscribe
mv goscribe /usr/local/bin/
```

## Quick Start

### 1. Store Your API Key (Recommended)

```bash
goscribe -set-key YOUR_OPENAI_API_KEY
```

### 2. Transcribe Audio

```bash
# Basic transcription
goscribe meeting.mp3

# Transcribe with post-processing
goscribe -action openai-meeting-summary meeting.mp3
```

### 3. Process Existing Transcript

```bash
goscribe -transcript meeting-transcript.txt -action openai-action-items
```

## Usage

```
goscribe [options] <audio_file>
goscribe -transcript <transcript_file> -action <action_id>
```

### Options

- `-k` - OpenAI API key (or use config file)
- `-action` - Post-processing action ID
- `-transcript` - Process existing transcript file
- `-o` - Output file name
- `-config` - Custom config file path
- `-list-actions` - List all available actions
- `-set-key` - Store API key in config
- `-init` - Reset config to defaults

## Built-in Actions

### Meeting & Communication
- `openai-meeting-summary` - Comprehensive meeting summary
- `openai-action-items` - Extract action items and tasks
- `openai-standup` - Daily standup summary
- `openai-one-on-one` - 1:1 meeting notes
- `openai-client-meeting` - Client meeting notes

### Technical & Development
- `openai-tech-meeting` - Technical meeting summary
- `openai-decision-record` - Architecture Decision Record (ADR)
- `openai-retrospective` - Sprint retrospective
- `openai-incident-postmortem` - Incident analysis

### Business & Strategy
- `openai-executive-brief` - Executive summary
- `openai-project-kickoff` - Project kickoff summary
- `openai-key-insights` - Strategic insights

### Learning & Analysis
- `openai-training-session` - Training session notes
- `openai-interview-notes` - Interview summary
- `openai-brainstorm` - Brainstorming session
- `openai-qa-format` - Q&A generator

### HR & Internal
- `openai-hr-meeting` - HR meeting summary
- `openai-company-webinar` - Company webinar summary

## Configuration

Config file location: `~/.goscribe/config.yml`

### Example Custom Action

```yaml
openai_api_key: "your-api-key-here"

post_actions:
  - id: "custom-summary"
    name: "Custom Summary"
    description: "My custom summary action"
    type: "openai"
    prompt: |
      Summarize this transcript focusing on:
      - Key decisions
      - Action items
      - Next steps
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1500
```

### Reset Config

```bash
goscribe -init
```

## Examples

### Basic Transcription
```bash
goscribe meeting.mp3
# Output: meeting-transcript.txt
```

### Transcription with Action
```bash
goscribe -action openai-meeting-summary interview.mp3
# Output: interview-transcript.txt, interview-openai-meeting-summary.txt
```

### Process Existing Transcript
```bash
goscribe -transcript notes.txt -action openai-action-items
# Output: notes-openai-action-items.txt
```

### Custom Output File
```bash
goscribe -o my-transcript.txt meeting.mp3
```

### List All Actions
```bash
goscribe -list-actions
```

## Development

### Build

```bash
make build              # Standard build
make build-optimized    # Optimized build with size reduction
make build-all          # Build for all platforms
```

### Testing

```bash
make test               # Run tests with verbose output
make test-short         # Run tests without verbose output
make test-coverage      # Generate coverage report (coverage.html)
```

### Project Structure

```
.
├── main.go              # Main application logic
├── main_test.go         # Unit tests
├── default_config.go    # Default configuration template
├── Makefile            # Build and test commands
├── go.mod              # Go module definition
└── README.md           # This file
```

## Testing

The project includes comprehensive unit tests covering:

- Configuration validation
- Action management
- Config file operations
- API key storage
- Default config generation

Run tests:
```bash
make test              # Verbose output
make test-short        # Quick test run
make test-coverage     # Generate HTML coverage report
```

Current test coverage: ~21.5%

## Output Files

- `<filename>-transcript.txt` - Raw transcription
- `<filename>-<action-id>.txt` - Post-processed output

## Requirements

- Go 1.21 or higher
- OpenAI API key
- Supported audio formats: mp3, mp4, mpeg, mpga, m4a, wav, webm

## License

[Add your license here]

## Contributing

Contributions welcome! Please feel free to submit a Pull Request.
