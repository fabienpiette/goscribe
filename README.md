# goscribe

AI-powered audio recording, transcription and post-processing — transcribe with OpenAI Whisper, Gemini, ElevenLabs Scribe or local faster-whisper, then summarize with OpenAI, Gemini, or your Claude Code / Codex / Hermes Agent login.

---

<p align="center">
  <img src="docs/demo.gif" alt="goscribe demo" width="800">
</p>

## Quick Start

```bash
# Install
git clone https://github.com/fabienpiette/goscribe && cd goscribe
make build && make install

# Store your API key once
goscribe -set-key YOUR_OPENAI_API_KEY

# Transcribe
goscribe meeting.mp3

# Transcribe + summarize in one go
goscribe -action openai-meeting-summary meeting.mp3
```

## Features

- **Dual providers** — OpenAI (Whisper + GPT) and Google Gemini, with automatic fallback between them
- **Local agent harnesses** — run post-processing actions through your Claude Code, Codex or Hermes Agent CLI login instead of an API key
- **Search** — `goscribe search tokens person:alex -since 2026-09-01` finds meetings by their tags
- **Live assistant** — `goscribe assist` follows a meeting live (ElevenLabs realtime) and, in your terminal only, suggests questions, answers from your notes, missed points and risks, while keeping a running summary
- **Recording** — `goscribe record` captures the mic and/or computer sound to mp3 (Ctrl+C to stop), lets you mute either side mid-recording, warns about silent sources, and can transcribe right after
- **Searchable tags** — `-tags` writes a `<name>-tags.yml` (type, summary, topics, people, tickets, projects) next to the transcript, reusing existing tags, so `grep` finds every meeting on a subject
- **Pluggable transcription** — transcribe locally and offline (faster-whisper) or with ElevenLabs Scribe (speaker labels), then post-process with OpenAI or Gemini
- **18 built-in actions** — meeting summaries, action items, technical notes, retrospectives, and more
- **Auto-select mode** — let the AI pick the most relevant actions for your transcript
- **Large file handling** — audio splitting and transcript chunking happen transparently
- **HTTP API + async job queue** — Redis-backed server mode for homelab and self-hosted deployments
- **Custom actions** — define your own post-processing prompts in YAML config

## Install

**Prerequisites:** Go 1.24+. Depending on the features you use:

- **ffmpeg** — for `goscribe record` and `goscribe assist`, and to split files over the provider size limits
- **pactl** (PipeWire or PulseAudio) — for `goscribe record` and `goscribe assist`
- **whisper-ctranslate2** — for `-transcriber local` (`uv tool install whisper-ctranslate2`)
- **claude**, **codex** or **hermes** CLI, logged in — for `-provider claude` / `-provider codex` / `-provider hermes`

### CLI — from source

```bash
git clone https://github.com/fabienpiette/goscribe
cd goscribe
make build && make install
```

### Server / Docker

```bash
cp .env.example .env
# Add your API key(s) to .env, then:
make docker-up
```

The server starts on port 8080 with Redis managed by Docker Compose. See [Server Mode](#server-mode) below.

## Usage

### CLI

```bash
# Use Gemini instead of OpenAI
goscribe -set-gemini-key YOUR_GEMINI_KEY
goscribe -provider gemini meeting.mp3

# Transcribe locally (offline), summarize with OpenAI
goscribe -transcriber local -action openai-meeting-summary meeting.mp3

# Transcribe with ElevenLabs Scribe (speaker labels)
ELEVENLABS_API_KEY=... goscribe -transcriber elevenlabs meeting.mp3

# Run actions through your local Claude Code (or Codex) login
goscribe -transcriber local -provider claude -action openai-meeting-summary meeting.mp3

# Tag a meeting for search (new audio, or an existing transcript)
goscribe -provider codex -tags meeting.mp3
goscribe -provider codex -transcript meeting-transcript.txt -tags
grep -rlw dark-mode --include='*-tags.yml' ~/records

# Run multiple actions at once
goscribe -action openai-meeting-summary,openai-action-items meeting.mp3

# Let AI pick the best actions
goscribe --auto meeting.mp3

# Process an existing transcript
goscribe -transcript meeting-transcript.txt -action openai-action-items

# Process multiple transcripts together
goscribe -transcript call1.txt call2.txt -action openai-meeting-summary
```

Output files are written alongside the input:

```
<filename>-transcript.txt     Raw transcription
<filename>-<action-id>.txt    Post-processed output
<filename>-tags.yml           Tags for search (with -tags)
```

**All flags:**

| Flag | Description |
|------|-------------|
| `-k` | OpenAI API key |
| `-gemini-key` | Gemini API key |
| `-provider` | Actions engine: `openai` (default), `gemini`, `claude`, `codex` or `hermes` |
| `-transcriber` | Transcription engine: `local`, `elevenlabs`, `openai` or `gemini` (default: config, else same as `-provider`) |
| `-elevenlabs-key` | ElevenLabs API key (default: config, then `$ELEVENLABS_API_KEY`) |
| `-keyterms` | Comma-separated names/jargon for ElevenLabs to favor (20% surcharge) |
| `-num-speakers` | Max speakers for ElevenLabs diarization (default: auto) |
| `-whisper-model` | Whisper model for `-transcriber local` (default: `small`) |
| `-language` | Audio language code for `local` and `elevenlabs`, e.g. `fr` (default: auto-detect) |
| `-no-fallback` | Disable automatic provider fallback |
| `-action` | Action ID(s), comma-separated |
| `--auto` | AI picks the best actions |
| `-transcript` | Process existing transcript file(s) |
| `-tags` | Write `<name>-tags.yml` next to the transcript (works with `-transcript` alone) |
| `-tags-root` | Folder whose tag files seed the shared vocabulary (default: `tags_root` in config, else the transcript folder's parent) |
| `-o` | Transcript file name (default: `<audio name>-transcript.txt` next to the audio) |
| `-config` | Custom config file path |
| `-list-actions` | Show all available actions |
| `-set-key` | Save OpenAI key to config |
| `-set-gemini-key` | Save Gemini key to config |
| `-set-provider` | Save default provider to config |
| `-set-transcriber` | Save default transcription engine to config |
| `-init` | Reset config to defaults |

### Server Mode

The server exposes a REST API backed by an async job queue. Jobs are processed
by a worker and results are stored in Redis until retrieved.

```bash
# Submit a transcription job
curl -X POST http://localhost:8080/jobs \
  -F "file=@meeting.mp3" \
  -F "actions=openai-meeting-summary"

# Poll for results
curl http://localhost:8080/jobs/<job-id>

# List available actions
curl http://localhost:8080/actions
```

| Endpoint | Description |
|----------|-------------|
| `POST /jobs` | Submit an audio file or transcript for processing |
| `GET /jobs/{id}` | Fetch job status and results |
| `GET /actions` | List configured post-processing actions |
| `GET /health` | Health check |
| `GET /docs` | Swagger UI |
| `GET /openapi.yaml` | OpenAPI 3.0 spec |

`POST /jobs` accepts: `file` (audio), `transcript` (text), `actions`
(comma-separated IDs or `auto`), `provider`, `webhook_url` (called with
results on completion).

**Scaling:** set `MODE=api` and `MODE=worker` in separate containers using
`docker compose --profile split up`. Both containers must share the same Redis
instance and uploads volume.

## Configuration

### CLI

Config lives at `~/.goscribe/config.yml`. Add custom actions:

```yaml
provider: "openai"
openai_api_key: ""
gemini_api_key: ""
gemini_model: "gemini-2.0-flash"

# Optional transcription engine settings
transcriber: "local"            # local, elevenlabs, openai or gemini
whisper_model: "small"          # local only; e.g. large-v3-turbo for accuracy
elevenlabs_api_key: ""
elevenlabs_model: "scribe_v2"
elevenlabs_keyterms: ["Acme"]  # names/jargon to favor (20% surcharge)
elevenlabs_num_speakers: 0      # max speakers, 0 = auto
elevenlabs_verbatim: false      # true keeps filler words
language: ""                    # e.g. "fr"; empty = auto-detect

# Optional tags, search and record defaults
tags_root: ""                   # folder holding your recordings, for -tags and search
record_source: "both"           # mic, system or both
record_mic: ""                  # device name from `goscribe record -list-sources`
record_system: ""               # computer sound monitor; empty = default output
record_quality: "voice"         # voice or music

post_actions:
  - id: "custom-summary"
    name: "Custom Summary"
    description: "My custom summary"
    type: "openai"          # or "gemini"
    prompt: |
      Summarize focusing on key decisions and next steps.
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1500
```

Reset to defaults: `goscribe -init`

### Server

All configuration is via environment variables. Copy `.env.example` to `.env`:

| Variable | Default | Description |
|----------|---------|-------------|
| `MODE` | `all` | `all` \| `api` \| `worker` |
| `PORT` | `8080` | HTTP listen port |
| `REDIS_URL` | `redis://redis:6379` | Redis connection URL |
| `OPENAI_API_KEY` | — | OpenAI API key |
| `GEMINI_API_KEY` | — | Gemini API key |
| `GOSCRIBE_PROVIDER` | `openai` | Default provider |
| `MAX_UPLOAD_MB` | `100` | Max audio upload size |
| `RESULT_TTL_HOURS` | `24` | Job result retention in Redis |
| `GOSCRIBE_CONFIG_FILE` | — | Path to custom `config.yml` for extra actions |
| `GEMINI_MODEL` | `gemini-2.0-flash` | Gemini model |
| `UPLOADS_DIR` | `/tmp/goscribe-uploads` | Where uploaded audio waits for the worker |
| `SHUTDOWN_TIMEOUT_SECONDS` | `30` | Graceful shutdown drain timeout |

## Built-in Actions

18 actions ship by default. Run `goscribe -list-actions` to see them all.

| Category | Actions |
|----------|---------|
| Meetings | `openai-meeting-summary` `openai-action-items` `openai-standup` `openai-one-on-one` `openai-client-meeting` |
| Technical | `openai-tech-meeting` `openai-decision-record` `openai-retrospective` `openai-incident-postmortem` |
| Business | `openai-executive-brief` `openai-project-kickoff` `openai-key-insights` |
| Learning | `openai-training-session` `openai-interview-notes` `openai-brainstorm` `openai-qa-format` |
| HR | `openai-hr-meeting` `openai-company-webinar` |

## Providers

**OpenAI** (default) — Whisper for transcription, GPT for post-processing. 25 MB file limit. [Get an API key](https://platform.openai.com/api-keys).

**Gemini** — native audio understanding for transcription and processing. 20 MB inline limit, up to 1M token context. [Get an API key](https://aistudio.google.com/app/apikey).

When both keys are configured, goscribe automatically falls back to the other provider on failure. Disable with `-no-fallback`.

### Local harnesses

**claude**, **codex** and **hermes** run post-processing actions and `--auto` through the [Claude Code](https://docs.anthropic.com/en/docs/claude-code), [Codex](https://github.com/openai/codex) or [Hermes Agent](https://github.com/NousResearch/hermes-agent) CLI, with that CLI's own login and its default model, or the one set with `claude_model` / `codex_model` / `hermes_model` in the config (the action's `model` field is ignored). They run headless from an empty temp dir, with no tools, no MCP servers, no hooks or plugins and no saved session (Codex also gets a read-only sandbox and no web search). The transcript is untrusted input, so the agents get no way to read files or run commands that a spoken prompt injection could abuse. The whole transcript goes in one prompt, without chunking. They can't transcribe audio, so with no `-transcriber` set they pair with `local`. No fallback to an API provider.

Hermes runs with `--safe-mode`, which drops its user config, plugins, MCP servers, AGENTS.md, SOUL.md and memory, and `-t none`, which enables no toolset (Hermes has no explicit empty toolset, and an empty `-t` would enable them all). Safe mode also drops the default model, so goscribe passes it: `hermes_model` and `hermes_provider` from its config (or `-model` in `assist`), else the `model.default` and `model.provider` of your Hermes config (`$HERMES_HOME/config.yaml`, `~/.hermes` by default). goscribe then reads Hermes's event stream and fails closed: any event other than plain text and the result (a tool call, say) discards the answer with an error. Hermes has no way to skip saving the session, so it keeps it in its history; `--source tool` keeps it out of your session lists.

### Transcribers

`-transcriber` only picks who turns audio into text; `-provider` still runs actions and `--auto`. `local` and `elevenlabs` never fall back to another engine.

**local** — [faster-whisper](https://github.com/SYSTRAN/faster-whisper) via the `whisper-ctranslate2` CLI, fully offline. Install with `uv tool install whisper-ctranslate2` (or `pipx`). The model is downloaded on first use; uses the GPU when available, otherwise the CPU.

**elevenlabs** — [ElevenLabs Scribe](https://elevenlabs.io/docs/capabilities/speech-to-text), tuned for meetings: speaker diarization, filler words and false starts removed (`no_verbatim`), no audio-event tags, and one paragraph per speaker turn with its start time, e.g. `[00:12:34] speaker_1: …`. Optional: `-keyterms` / `elevenlabs_keyterms` to favor names and jargon (ElevenLabs adds a 20% surcharge), `-num-speakers` / `elevenlabs_num_speakers` to cap the speaker count, `elevenlabs_verbatim: true` to keep fillers. Needs an ElevenLabs API key.

## Tags

`-tags` asks the `-provider` model for tags and writes them as YAML with one list item per line:

```yaml
date: "2026-10-13"
type: standup
summary: Weekly sync on the design token migration
topics:
    - dark-mode
    - design-tokens
people:
    - Sam
tickets:
    - PROJ-123
projects:
    - billing
```

goscribe normalizes what the model returns (kebab-case topics and projects, uppercase ticket IDs, a fixed list of meeting types, sorted and de-duplicated lists) rather than trusting its formatting. To keep tags consistent for `grep`, it first reads the topics and projects already used in `*-tags.yml` files under the tags root and its direct subfolders (a `records/<date>/` layout) and asks the model to reuse them. Transcripts under 30 words are not tagged. A tagging failure only prints a warning; the transcript and actions are kept.

### Searching

`goscribe search` finds meetings by their tags files, newest first:

```bash
goscribe search dark-mode                          # in any field
goscribe search tokens person:alex type:1:1        # all terms must match
goscribe search ticket:PROJ-123 -since 2026-09-01
goscribe search -v onboarding                      # + topics, people, tickets, related files
goscribe search -paths onboarding | xargs grep -il invoice
```

With `-v`, related files are the action outputs (`<name>-*.txt`) and notes saved next to the recording as `<name>-*.md`, such as `goscribe assist` notes renamed `<name>-assist.md`.

Terms are case-insensitive substrings, so `alex` finds both "Alex" and "Alexis". `field:value` limits a term to `topic`, `person`, `ticket`, `project`, `type` or `summary`. It searches `-root`, else `tags_root` from the config, else the current folder, recursively. Exit code: 0 with matches, 1 without (like grep), 2 on errors.

### Recording

`goscribe record` records the mic and/or the computer sound (PipeWire/PulseAudio, through ffmpeg) to an mp3 until Ctrl+C:

```bash
goscribe record                                   # mic + computer sound -> recording-<date>_<time>.mp3
goscribe record -source system -o webinar.mp3     # computer sound only
goscribe record -duration 45m -transcribe -provider codex -tags
goscribe record -list-sources                     # pick devices for -mic / -system
```

- `-quality voice` (default) is mono 64 kbps, about 30 MB per hour. `-quality music` is stereo VBR.
- Press `m` to mute or unmute the mic and `s` for the computer sound, while recording in a terminal. Muting only affects the recording: that side is recorded as silence, your real mic and speakers are not touched, and the timeline stays continuous. The status line shows `(muted)`, and each change is logged, e.g. `🔇 system muted at 00:12:41`.
- If a side stays silent for 30 s, goscribe warns (`⚠ no sound from mic for 30s`) and keeps recording.
- The first Ctrl+C stops cleanly; a second one stops at once. An existing file is never overwritten.
- Config defaults: `record_source`, `record_mic`, `record_system`, `record_quality`.
- Use headphones: with speakers, the mic also picks up the computer sound, so the other people are recorded twice.

### Live assistant

`goscribe assist` is a meeting copilot that only you can see. It listens to your mic (`me`) and the computer sound (`others`) with [ElevenLabs realtime speech-to-text](https://elevenlabs.io/docs/api-reference/speech-to-text/v-1-speech-to-text-realtime), one live session per side, so it always knows who said what. Every update, the `-provider` model refreshes its memory of the meeting and suggests what you could say:

```bash
goscribe assist -context ~/notes/standup/                    # background notes: a .md file or a folder
goscribe assist -context agenda.md -provider claude -o standup-assist.md
goscribe assist -source system -language fr                   # only the others, in French
goscribe assist -provider codex -model gpt-6-luna             # pick the model (config: codex_model)
```

```
  00:12:09 others what about the migration rollback?
❓ 00:12:15 Ask: Who owns the rollback if the migration fails? — a named owner is required · agenda.md
⚠️ 00:12:15 Risk: Monday falls inside the database freeze (20th–25th). — agenda.md
✅ 00:13:40 Decision: Ship behind a feature flag
‼ 📌 00:58:02 Missed: Hiring plan for Q3 is still on the agenda. — agenda.md
● ASSIST 00:13:52  others ▸ "so the next step would be…"  · thinking 4s (claude)
```

- Suggestions are `❓ Ask`, `💬 Answer`, `📌 Missed` and `⚠️ Risk`, at most 2 per update, never repeated; `‼` marks one whose moment will soon pass. An answer always names the context file it comes from: one that doesn't is dropped. Nothing is said or posted in the meeting.
- Agenda tracking: the model keeps the agenda items and goals of your context that the meeting has not discussed yet. When the meeting wraps up ("we're out of time", "one last thing"), it raises the important ones as `‼ 📌 Missed`.
- Names: `others` can be several people. When the talk makes clear who speaks (people address each other, or a name fits the attendees in your context), the summary, decisions and action items use the name.
- Keyterms: add a `keyterms: Acme, Globex, Marie Curie` line to a context file (in the text or the front matter) and those names are favored by the live speech-to-text, on top of `-keyterms` and `elevenlabs_keyterms`. The model also spells sound-alikes as the context does. Live speech-to-text takes keyterms of at most 20 characters: longer ones are left out, with a warning at startup.
- The notes file (`assist-<date>_<time>.md` by default) is rewritten after every update: summary so far, agenda items not covered yet, decisions, open questions and every suggestion. Its header gives the wall-clock start, to match suggestion times with a recording started at another moment. Ctrl+C writes the final summary and action items into it; a second Ctrl+C quits at once. These notes are a live draft: a summary made from the full recording (`goscribe` with a summary action) is more reliable.
- Updates run at most every 20 s with `openai`/`gemini`, 30 s with `claude`/`codex`/`hermes` (`-interval` to change). One update at a time; a failed one is retried later with what it missed.
- It captures the audio itself, so run it in a second terminal next to `goscribe record`, or alone. Same device flags and `record_*` config defaults as `record` (`-source`, `-mic`, `-system`, `-list-sources`).
- Needs an ElevenLabs API key (`elevenlabs_api_key` or `$ELEVENLABS_API_KEY`). Live transcription is billed per stream, so two with `-source both`. The context is resent with every update: keep it small (a warning shows above 50 KB, 200 KB is the limit).
- The transcript is treated as untrusted data: the model is told never to follow instructions spoken in the meeting, and `claude`/`codex`/`hermes` run with no tools.
- Use headphones: with speakers, the mic also hears the others and their words are labelled `me`.
- Press `m` to mute or unmute your mic (`me`) and `s` for the computer sound (`others`), in a terminal, as in `record`. A muted side is not transcribed: its live session gets silence and stays open, so nothing said on that side reaches the model, the suggestions or the notes until you unmute. Live transcription lags a second or two behind, so the words said just before you press are dropped too. The status line shows `me (muted)`, and each change is logged, e.g. `🔇 me muted at 00:12:41`. The keys only affect `assist`: when `goscribe record` runs in another terminal, mute it there too.
- Press `w` ("what did I miss?") for a short recap of the last minutes, e.g. after a distraction: `📝 00:42:10 Recap of the last 5 min:` and 2 to 6 points, in the language of the meeting. It goes back 5 minutes by default (`-recap 10m`, or `assist_recap: 10m` in the config). It is one `-provider` call: when an update is running, the recap follows it, without waiting for the update interval.

#### Librarian

With `-librarian`, the model can look things up in your own sources while the meeting runs (a past decision, a ticket, a document, someone's earlier work). Each lookup is a short, read-only Claude Code session that gets a search question, why it helps now and the running meeting summary, never transcript lines. The answer shows in the feed (`🔎 Looking up: …`, then `📚 Librarian: … — titles`), feeds the next updates and is listed under `## Research` in the notes.

```yaml
librarian:
  allowed_tools:                 # required; the only tools the librarian may use
    - Bash(gh search:*)
    - Bash(gh pr view:*)
    - mcp__tracker__search_issues
    - mcp__tracker__get_issue
  instructions: "Acme tickets are PROJ-<n> in the tracker; code and PRs are in GitHub (acme/*)."
  mcp_config: ~/.goscribe/librarian-mcp.json   # optional, see below
  use_claude_settings: false     # true: load your Claude Code connectors and plugins, see below
  timeout: 3m                    # per lookup (default)
  max_lookups: 8                 # per meeting (default)
  model: ""                      # optional Claude model for lookups
```

```bash
goscribe assist -librarian -context agenda.md -provider claude
```

- The header ends with `librarian: claude, N tool(s)`. Without `librarian.allowed_tools`, `-librarian` is a usage error: it is off by default.
- Read-only only. goscribe refuses an `allowed_tools` entry and does not start when it finds one of these:
  - several rules in one entry. Claude Code splits its tool list on commas and spaces, so an entry must have no `,` or space outside its parentheses, and its arguments in a single `(...)` at the end.
  - a tool other than `Bash`, `Read`, `Grep`, `Glob`, `LS` and exact MCP tools (`mcp__<server>__<tool>`). `WebFetch`, `WebSearch`, `Task`, `Agent` and any other built-in are refused.
  - bare or wildcard `Bash` (`Bash`, `Bash(*)`), and `Bash` prefixes that start with a shell, an interpreter, a wrapper or a command that reaches the network or changes files: `sh`, `bash`, `zsh`, `dash`, `fish`, `ksh`, `python`, `python3`, `node`, `perl`, `ruby`, `php`, `awk`, `sed`, `find`, `rm`, `mv`, `cp`, `tee`, `dd`, `sudo`, `doas`, `timeout`, `nohup`, `nice`, `command`, `exec`, `eval`, `env`, `xargs`, `curl`, `wget`, `ssh`, `scp`, `docker`, `kubectl`; and `gh api`, `gh auth` and `gh config`.
  - any entry with a write verb (send, post, create, update, delete, save, write, edit, comment, merge, share, trash, upload, react, schedule).
  - MCP tools whose name has no read verb (`get`, `list`, `search`, `read`, `view`, `fetch`, `query`, `find`, `retrieve`, `lookup`), and server-wide or wildcard MCP entries such as `mcp__tracker` or `mcp__tracker__*`.

  `gh` rules must be `search`, `view`, `list`, `status`, `diff` or `checks`. `git` rules must be `status` or `ls-files`: `git log`, `show`, `diff` and `grep` take options that write files (`--output=<file>`) or run commands (`grep -O<cmd>`), and `git blame --contents <file>` prints any file; a prefix rule cannot restrict options or arguments. For the same reason the file tools `Read`, `Grep`, `Glob` and `LS` are accepted only with a folder scope, such as `Read(~/notes/**)` or `Grep(//srv/docs/**)`. In these rules `//path` is absolute and `~/path` is relative to your home folder: use only these two forms, since `/path` is relative to the project root, which for the librarian is a temp folder deleted after each lookup. A bare name, a scope like `/**`, `~/**`, `**` or `*`, or one containing `..` would let the librarian read any file (ssh keys, say) into its answer, which reaches the notes and the `-provider` model. This check is a heuristic, not a proof: only list tools you know are read-only.
- Two modes. Default, isolated: the librarian loads none of your Claude Code settings, hooks, plugins or permission rules, so Claude Code's own connectors and plugins are not available. It has the shell and file tools you list, plus MCP tools from the servers in the JSON file (Claude Code `--mcp-config` format) that `mcp_config` points to: only the servers in that file are loaded. With `use_claude_settings: true`, your Claude Code settings are loaded (`--setting-sources user`), so your connectors and plugins are available, and the servers in `mcp_config`, if set, are added to them. Your hooks and plugins then run on every lookup, which is slower and may have side effects; a `PreToolUse` hook, yours or a plugin's, can also approve tools and so widen the tool list, like `permissions.allow` rules. goscribe refuses to start in this mode if your Claude settings file (`~/.claude/settings.json`, or `$CLAUDE_CONFIG_DIR/settings.json`) has `permissions.allow` rules, since they would widen the tool list; it cannot check hooks, so only use this mode with hooks and plugins you trust. Apart from that, only the tools in `allowed_tools` are approved. Check which tools answer with a short test meeting.
- `mcp_config` may start with `~`; a relative path is taken from the folder you run goscribe in. goscribe does not start if the file is missing.
- Tool names can differ in headless mode (`claude -p`, which the librarian runs) from what you see in Claude Code: a service available both as a Claude.ai connector and as a plugin shows up as `mcp__claude_ai_<Service>__…` to `claude -p`. List the exact names the librarian sees with:

  ```bash
  # use_claude_settings: true
  echo "List the exact names of your tools that start with mcp__" | claude -p --tools "" --setting-sources user
  # isolated mode, with mcp_config
  echo "List the exact names of your tools that start with mcp__" | claude -p --tools "" --setting-sources "" --mcp-config ~/.goscribe/librarian-mcp.json --strict-mcp-config
  ```
- The librarian's prompt lists its allowed tools, so it runs one plain command at a time instead of pipes or unlisted tools. Calls that the allowlist still refuses show as a dim line in the feed (`📚 refused 2 tool call(s) not in librarian.allowed_tools: …`), so you can add what is missing. When the Claude Code session itself fails (an API error, say), the feed shows its reason.
- Data flow: each lookup sends the search question, its "why" and the running meeting summary to the librarian's Claude session, together with your `instructions` and today's date; transcript lines are never sent. The librarian's answers, with their source titles and links, are sent to the `-provider` model in the next updates. With `openai` or `gemini`, snippets from your sources therefore go to that API. Their text is marked as untrusted data in the prompts, and in the notes links are shown as code, not as clickable links.
- Cost and latency: each lookup is a Claude Code session of about 30 to 120 s, on your Claude plan. One runs at a time, the newest waiting one is kept (and dropped after 2 minutes), similar questions are skipped, and at most `max_lookups` run per meeting; a failed lookup still counts (the feed shows `⚠ librarian failed: <cause>`), while a waiting one that was replaced or dropped never ran and does not.

## Known Issues

- **`goscribe assist` reacts with a delay** — about 2 s for the transcript, then up to the update interval plus the model's answer time (30–70 s with `claude`/`codex`/`hermes`), so it is better at "missed" and "risk" than at answering live.
- **`-transcriber` is CLI only** — server mode still transcribes with the request's provider (OpenAI or Gemini).
- **ffmpeg required for large files** — files over 25 MB (OpenAI) or 20 MB (Gemini) are split using `ffmpeg`. Install it with `brew install ffmpeg` or `apt install ffmpeg`.
- **`--auto` and `actions=auto` call the AI API** for action selection, which incurs additional API costs.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- Supported formats: mp3, mp4, mpeg, mpga, m4a, wav, webm, ogg, flac, aac, aiff

## Project Structure

```
cmd/goscribe/       CLI entry point and orchestration
cmd/server/         HTTP server + async worker entry point
pkg/config/         Config types, loading, validation (importable)
internal/api/       HTTP handlers and chi router (server mode)
internal/worker/    Async job processor via asynq (server mode)
internal/provider/  Provider routing and fallback logic
internal/openai/    OpenAI API client
internal/gemini/    Gemini API client
internal/elevenlabs/ ElevenLabs Scribe speech-to-text client
internal/whisperlocal/ Local faster-whisper transcription (whisper-ctranslate2)
internal/harness/   Claude Code / Codex / Hermes CLI runner for actions
internal/tags/      Tags files (-tags) and goscribe search
internal/recorder/  ffmpeg recording for goscribe record, live PCM capture for assist
internal/realtime/  ElevenLabs realtime speech-to-text client (WebSocket)
internal/assistant/ Live assistant: memory, prompts, update loop, notes
internal/util/      Shared helpers (model limits, audio splitting, etc.)
```

## Development

```bash
make build              # build CLI binary
make build-all          # cross-compile all platforms
make test               # run tests (verbose)
make test-coverage      # generate coverage.html
make docker-build       # build Docker image
make docker-up          # start server + Redis
make docker-logs        # tail container logs
```

## Acknowledgments

Thanks to all [contributors](https://github.com/fabienpiette/goscribe/graphs/contributors).

<p align="center">
<a href="https://buymeacoffee.com/fabienpiette" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" height="60"></a>
</p>

## License

[MIT](LICENSE)
