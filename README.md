# goscribe

AI-powered audio transcription tool with OpenAI Whisper, Google Gemini, and intelligent post-processing actions.

## Features

- 🎙️ **Audio Transcription** - Convert audio files to text using OpenAI Whisper or Google Gemini
- 🔄 **Multiple AI Providers** - Choose between OpenAI and Gemini for transcription and processing
- 🛡️ **Automatic Fallback** - Falls back to alternate provider if primary fails
- 📦 **Large File Support** - Automatic splitting for audio files >25MB (OpenAI) or >20MB (Gemini)
- 🤖 **AI Post-Processing** - 18 built-in actions for summarizing, extracting action items, and more
- 🧠 **Smart Auto-Selection** - AI automatically selects the best actions based on content
- 📝 **Process Existing Transcripts** - Apply actions to existing transcript files
- 🔄 **Multiple Actions** - Apply multiple post-processing actions in one command
- ⚙️ **Configurable** - Customize actions via YAML configuration
- 🔑 **API Key Management** - Store your API keys in config file

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
# For OpenAI (default provider)
goscribe -set-key YOUR_OPENAI_API_KEY

# For Gemini
goscribe -set-gemini-key YOUR_GEMINI_API_KEY
```

### 2. Set Default Provider (Optional)

```bash
# Use Gemini as default
goscribe -set-provider gemini

# Use OpenAI as default (this is the default)
goscribe -set-provider openai
```

### 3. Transcribe Audio

```bash
# Basic transcription (uses default provider)
goscribe meeting.mp3

# Transcribe with specific provider
goscribe -provider gemini meeting.mp3

# Transcribe with post-processing
goscribe -action openai-meeting-summary meeting.mp3
```

### 4. Process Existing Transcript

```bash
goscribe -transcript meeting-transcript.txt -action openai-action-items
```

### 5. Multiple Post-Processing Actions

```bash
# Apply multiple actions to one transcript
goscribe -action openai-meeting-summary,openai-action-items meeting.mp3

# With spaces (will be trimmed automatically)
goscribe -action "openai-meeting-summary, openai-action-items, openai-key-insights" meeting.mp3
```

### 6. Automatic Action Selection

```bash
# Let AI choose the best actions based on content
goscribe --auto meeting.mp3

# Works with existing transcripts too
goscribe -transcript notes.txt --auto
```

## Usage

```
goscribe [options] <audio_file>
goscribe -transcript <transcript_file> -action <action_id>
```

### Options

| Option | Description |
|--------|-------------|
| `-k` | OpenAI API key (or use config file) |
| `-gemini-key` | Gemini API key (or use config file) |
| `-provider` | AI provider: `openai` or `gemini` (default: from config or openai) |
| `-no-fallback` | Disable automatic fallback to alternate provider |
| `-action` | Post-processing action ID(s), comma-separated for multiple |
| `--auto` | Automatically select best actions based on transcript content |
| `-transcript` | Process existing transcript file |
| `-o` | Output file name |
| `-config` | Custom config file path |
| `-list-actions` | List all available actions |
| `-set-key` | Store OpenAI API key in config |
| `-set-gemini-key` | Store Gemini API key in config |
| `-set-provider` | Set default AI provider in config |
| `-init` | Reset config to defaults |

## AI Providers

### OpenAI (Default)

- **Transcription**: OpenAI Whisper API
- **Processing**: GPT-3.5-turbo, GPT-4, GPT-4-turbo, GPT-4o
- **File Limit**: 25MB per file
- **Get API Key**: https://platform.openai.com/api-keys

### Gemini

- **Transcription**: Gemini native audio understanding
- **Processing**: gemini-2.0-flash, gemini-1.5-pro, gemini-1.5-flash
- **File Limit**: 20MB inline (larger files automatically split)
- **Context**: Up to 1M tokens
- **Get API Key**: https://aistudio.google.com/app/apikey

### Provider Fallback

When both API keys are configured, goscribe automatically falls back to the alternate provider if the primary fails (rate limits, API errors, etc.):

```bash
# Configure both keys
goscribe -set-key YOUR_OPENAI_KEY
goscribe -set-gemini-key YOUR_GEMINI_KEY

# Now if OpenAI fails, Gemini will be tried automatically
goscribe meeting.mp3

# Disable fallback if needed
goscribe -no-fallback meeting.mp3
```

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

### Full Configuration Example

```yaml
# Default AI provider: "openai" or "gemini"
provider: "openai"

# API Keys
openai_api_key: "your-openai-key-here"
gemini_api_key: "your-gemini-key-here"

# Default Gemini model (used when provider is "gemini")
gemini_model: "gemini-2.0-flash"

post_actions:
  - id: "custom-summary"
    name: "Custom Summary"
    description: "My custom summary action"
    type: "openai"  # or "gemini"
    prompt: |
      Summarize this transcript focusing on:
      - Key decisions
      - Action items
      - Next steps
    model: "gpt-3.5-turbo"  # or "gemini-2.0-flash"
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

### Transcription with Gemini
```bash
goscribe -provider gemini meeting.mp3
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

### Multiple Actions
```bash
# Generate both summary and action items
goscribe -action openai-meeting-summary,openai-action-items meeting.mp3

# Process transcript with multiple actions
goscribe -transcript notes.txt -action openai-meeting-summary,openai-action-items,openai-key-insights
```

### Automatic Action Selection
```bash
# AI selects best actions automatically
goscribe --auto meeting.mp3

# Example output:
# 🤖 Analyzing transcript with openai to select best actions...
# ✓ Selected 2 action(s): openai-meeting-summary, openai-action-items
# Processing 2 action(s)...
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
- API key storage (OpenAI and Gemini)
- Default config generation
- Provider selection
- MIME type detection

Run tests:
```bash
make test              # Verbose output
make test-short        # Quick test run
make test-coverage     # Generate HTML coverage report
```

## Output Files

- `<filename>-transcript.txt` - Raw transcription
- `<filename>-<action-id>.txt` - Post-processed output

## Large File Handling

### Audio Files >25MB (OpenAI) or >20MB (Gemini)

goscribe automatically handles larger files by:

1. **Automatic Detection** - Checks file size before transcription
2. **Smart Splitting** - Splits audio into chunks using ffmpeg
3. **Sequential Processing** - Transcribes each chunk with progress indicators
4. **Seamless Merging** - Combines all transcripts into single output
5. **Auto Cleanup** - Removes temporary chunks after processing

**Example:**
```bash
# File is 30MB - automatically split and processed
goscribe large-meeting.mp3

# Output:
# ⚠ File size (30.5 MB) exceeds OpenAI limit (25 MB)
# Splitting audio file into chunks...
# ✓ Created 4 chunks
#
# [1/4] Transcribing chunk large-meeting_chunk_000.mp3...
# ✓ Chunk 1/4 complete
# ...
# ✓ All chunks transcribed successfully
```

### Long Transcripts Exceeding Context Limits

When transcripts are too long for the model's context window, goscribe automatically handles this:

1. **Model-Specific Limits** - Accurate limits per model:
   - GPT-4: 6K tokens
   - GPT-4-turbo: 100K tokens
   - Gemini 1.5/2.0: 900K tokens
2. **Token Estimation** - Estimates transcript + prompt tokens (~4 chars per token)
3. **Smart Chunking** - Splits on sentence boundaries for coherence
4. **Context Overlap** - Adds overlap between chunks for continuity
5. **Intelligent Merging** - AI merges chunk results, removing duplicates and consolidating
6. **Hierarchical Merging** - Handles very large transcripts by merging in pairs

**Example:**
```bash
# Long transcript from 60MB audio file
goscribe -action openai-meeting-summary long-recording.mp3

# Output:
# [1/2] Applying post-processing with openai: Smart Meeting Summary...
#   ⚠ Transcript is large (~8000 tokens), processing in chunks...
#   → Split into 2 chunk(s) for processing
#   → Processing chunk 1/2...
#   → Processing chunk 2/2...
#   ✓ All chunks processed, merging results intelligently
# ✓ Post-processed output saved to file.txt
```

## Rate Limit Handling

goscribe includes robust rate limit handling:

- **Automatic Retry** - Retries failed requests with exponential backoff
- **Smart Wait Times** - Parses wait time from API error responses
- **Inter-Chunk Delays** - Adds delays between chunk processing to prevent rate limits
- **Provider Fallback** - Automatically switches to alternate provider on rate limit (when both keys configured)

## Requirements

- Go 1.21 or higher
- OpenAI API key and/or Gemini API key
- ffmpeg (for large audio files)
- Supported audio formats: mp3, mp4, mpeg, mpga, m4a, wav, webm, ogg, flac, aac, aiff

## License

Released under the MIT License. See `LICENSE` for details.

## Contributing

Contributions welcome! Please feel free to submit a Pull Request.
