package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"goscribe/internal/assistant"
	"goscribe/internal/harness"
	"goscribe/internal/provider"
	"goscribe/internal/realtime"
	"goscribe/internal/recorder"
	"goscribe/internal/webpage"
	"goscribe/internal/whisperlocal"
	"goscribe/pkg/config"
)

const assistUsage = `goscribe assist - a live meeting copilot that only you can see

USAGE:
  goscribe assist [flags]     (Ctrl+C to stop and write the final notes; a second Ctrl+C quits at once)

It listens to your mic ("me") and the computer sound ("others") with
ElevenLabs realtime speech-to-text. Every few seconds it asks the -provider
model to update its memory of the meeting (summary, decisions, open questions)
and to suggest questions to ask, answers from your context, missed points and
risks. Nothing is said or posted in the meeting. The notes file is rewritten
after every update.

It captures the audio on its own: run it in a second terminal next to
'goscribe record', or alone.

In a terminal it takes the full screen: the live feed, a dashboard of the
meeting (summary, decisions, open questions, brief, fixes) and a chat input.
Type a question about the meeting and press Enter, or a command: /help,
/mute me|others, /recap [duration], /fix <wrong> <right>, /brief <text>,
/add <path|url>, /search <question>.
Alt+m mutes or unmutes your mic, Alt+s the computer sound, Alt+w asks for a
recap of the last minutes (-recap, default 5m). Tab shows the dashboard on a narrow window;
PgUp/PgDn or the mouse wheel scroll, End goes back to live; hold Shift and
drag to select text to copy. At the end, the final summary and the notes and
transcript paths stay in the terminal.

With -plain, or when the output is not a terminal, it prints the feed as
lines instead; there, m and s mute and w asks for the recap.

/fix corrects a misheard word: /fix cash cache replaces the whole word, in any
case, in the lines to come and in what was already said, the summary and the
lists, the transcript file and the notes (## Fixes). Quote phrases: /fix "dark
mood" "dark mode". Suggested fixes show as FIX?; answer with /fix yes|no [n],
nothing changes until you accept. /fix lists the fixes, /fix rm n stops one for
new lines. Live speech-to-text keyterms are not updated mid-meeting.

Naming an earlier assist notes file with -context restores its fixes and
briefs and keeps the file as untrusted reference data, not trusted context; a
folder restores nothing and leaves out earlier notes, transcripts and page copies
(saved pages are marked as untrusted).

/brief <text> gives the model a standing instruction or fact, trusted and sent
with every update, question, recap and the final notes: /brief focus on risks
to the release. It starts an update at once, once something has been said. /brief lists the briefs, /brief rm
n removes one (at most 20, 300 characters each). They show under BRIEF on the
dashboard and ## Brief in the notes.

/add <path or url> (alias /context) adds a file, folder or web page to the
context while the meeting runs and, once something has been said, starts an
update at once, like /brief; /add alone lists the sources. A public page is
fetched, converted to Markdown and saved in <notes name>-sources/<slug>.md
(e.g. assist-2026-10-29_10-00-sources/); it is untrusted data. Docs behind a
login (Google Docs, Notion, Linear and the like, any 401 or 403, a sign-in
redirect) go to the librarian when it is on, which reads them with its
read-only tools; without it, the feed explains how to set it up. Naming an
earlier notes file restores its fixes and briefs and is kept as untrusted
reference data; a folder restores nothing and leaves out earlier notes,
transcripts and page copies, which are marked as untrusted.
The limit is 200 KB in total, counting -context and everything added.

/search <question> asks the librarian to look something up in your sources
now. It is unlimited, runs before the model's own lookups and a second one
waits its turn; the answer shows as a Librarian line, feeds the updates and
goes to ## Research in the notes. Without the librarian, the feed says why.
A question the context can't answer may start a lookup the same way.

A muted side is not transcribed (its live session gets silence), so nothing
from it reaches the model or the notes. Muting affects only assist; mute
record separately in its own terminal. The transcript is written next to the
notes, line by line (standup.md gets standup-transcript.md).

EXAMPLES:
  goscribe assist -context ~/notes/standup/
  goscribe assist -context agenda.md -provider claude -o standup-assist.md
  goscribe assist -source system -language fr
  goscribe assist -provider codex -model gpt-6-luna
  goscribe assist -plain -source system

Good to know:
  - The -context files (.md) are sent to the -provider model with every update.
    Put the agenda or goals in them: points still not covered are raised when
    the meeting wraps up. A "keyterms: Name, Term" line adds names and jargon to
    the live speech-to-text, like -keyterms (at most 20 characters each).
  - Needs an ElevenLabs API key (config elevenlabs_api_key or $ELEVENLABS_API_KEY).
    Out of quota, it switches to local transcription (faster-whisper, slower
    and less accurate) when that is installed.
    Live transcription is billed per stream: two with -source both.
  - The librarian lets the model look things up in your own sources (past
    decisions, tickets, documents) through a read-only Claude Code session.
    It is on by default when librarian.allowed_tools is set in the config (only
    list read-only tools); a failed startup check then prints a warning and
    assist runs without it. -librarian=false or librarian.enabled: false turns
    it off; an explicit -librarian makes a failed check an error.
    Each lookup takes 30-120 s on your Claude plan, and its answer goes to the
    -provider model.
  - Use headphones. With speakers, the mic also hears the others, and their
    words are labelled "me".

FLAGS:
`

// Default time between two updates: harnesses start a process per call.
const (
	assistIntervalAPI     = 20 * time.Second
	assistIntervalHarness = 30 * time.Second
)

func assistName(t time.Time) string {
	return t.Format("assist-2006-01-02_15-04-05") + ".md"
}

// runAssist implements "goscribe assist" and returns the exit code:
// 0 done, 1 failed while running, 2 usage error.
func runAssist(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("assist", flag.ContinueOnError)
	fs.SetOutput(stderr)
	contextPath := fs.String("context", "", "Meeting background: a .md file or a folder of .md files (sent to the model)")
	output := fs.String("o", "", "Notes file (default: assist-YYYY-MM-DD_HH-MM-SS.md in the current folder)")
	providerFlag := fs.String("provider", "", "Model that thinks: openai, gemini, claude, codex or hermes (default: config provider, else openai)")
	modelFlag := fs.String("model", "", "Model for -provider (default: config codex_model / claude_model / hermes_model / gemini_model, else the provider's default)")
	source := fs.String("source", "", "mic, system or both (default: config record_source, else both)")
	mic := fs.String("mic", "", "Input device (default: config record_mic, else the system default; see -list-sources)")
	system := fs.String("system", "", "Computer sound monitor (default: config record_system, else the default output's monitor)")
	language := fs.String("language", "", "Meeting language code, e.g. fr (default: config language, else auto-detect)")
	keyterms := fs.String("keyterms", "", "Comma-separated names/jargon to favor (default: config elevenlabs_keyterms)")
	elevenLabsKey := fs.String("elevenlabs-key", "", "ElevenLabs API key (default: config, then $ELEVENLABS_API_KEY)")
	interval := fs.Duration("interval", 0, "Shortest time between two updates (default: 20s, 30s with claude/codex/hermes)")
	recap := fs.Duration("recap", 0, "How far back a recap goes (w, Alt+w, /recap without a duration; default: config assist_recap, else 5m)")
	librarian := fs.Bool("librarian", false, "Use the librarian (default: on when librarian.allowed_tools is set; -librarian=false turns it off)")
	configFile := fs.String("config", "", "Config file (default: ~/.goscribe/config.yml)")
	listSources := fs.Bool("list-sources", false, "List inputs and computer sound sources, then exit")
	plain := fs.Bool("plain", false, "Print the feed as plain lines instead of the full-screen view (always so when not on a terminal)")
	fs.Usage = func() {
		fmt.Fprint(stderr, assistUsage)
		fs.PrintDefaults()
	}

	rest, err := splitArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	usageErr := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "Error: "+format+"\n", a...)
		return 2
	}
	if len(rest) > 0 {
		return usageErr("unexpected argument %q (use -context for the meeting notes, -o for the output)", rest[0])
	}
	if *listSources {
		return printSources(stdout, stderr)
	}

	cfg, err := loadAssistConfig(*configFile)
	if err != nil {
		return usageErr("%v", err)
	}
	prov := pick(*providerFlag, cfg.Provider, "openai")
	if !config.IsValidProvider(prov) {
		return usageErr("invalid provider %q (valid: %s)", prov, strings.Join(config.ValidProviders, ", "))
	}
	if err := checkThinker(prov, cfg); err != nil {
		return usageErr("%v", err)
	}
	harness.Models = cfg.HarnessModels()
	harness.HermesProvider = cfg.HermesProvider
	harness.OwnProcessGroup = true // Ctrl+C ends the meeting, not the call in flight
	if *modelFlag != "" && harness.IsHarness(prov) {
		harness.Models[prov] = *modelFlag
	}
	lib, libOn, libOff, libWarn, err := resolveLibrarian(isSet(fs, "librarian"), *librarian, cfg.Librarian.WithDefaults(), checkLibrarian)
	if err != nil {
		return usageErr("librarian: %v", err)
	}
	// A failed check is only a warning when -librarian was not passed.
	var librarianNotice string
	if libWarn {
		librarianNotice = "⚠ librarian off: " + libOff
	}
	elKey := pick(*elevenLabsKey, cfg.ElevenLabsAPIKey, os.Getenv("ELEVENLABS_API_KEY"))
	if elKey == "" {
		return usageErr("an ElevenLabs API key is required (-elevenlabs-key, elevenlabs_api_key in the config, or $ELEVENLABS_API_KEY)")
	}
	if isSet(fs, "interval") && *interval <= 0 {
		return usageErr("-interval must be positive (e.g. 20s)")
	}
	if isSet(fs, "recap") && *recap <= 0 {
		return usageErr("-recap must be positive (e.g. 5m)")
	}
	recapWindow := *recap
	if recapWindow == 0 {
		recapWindow = cfg.AssistRecap
	}
	if recapWindow <= 0 {
		recapWindow = 5 * time.Minute
	}
	every := *interval
	if every == 0 {
		every = assistIntervalAPI
		if harness.IsHarness(prov) {
			every = assistIntervalHarness
		}
	}

	background, contextFiles, contextDocs, contextSkipped, err := loadContextArg(*contextPath)
	if err != nil {
		return usageErr("context: %v", err)
	}
	ctxBytes := len(background)
	for _, d := range contextDocs {
		ctxBytes += len(d.Text)
	}
	// An earlier assist notes file named by -context gives back its fixes and
	// briefs; a parse error is a startup notice and the file is still an untrusted
	// notes doc.
	restore, restoreFrom, restoredTerms, restoreErr := restoreFromContext(*contextPath)
	var restoreNotice string
	if restoreErr != nil {
		restoreNotice = fmt.Sprintf("⚠ %s: %v; nothing restored", filepath.Base(*contextPath), restoreErr)
	}

	var skippedNotice string
	if len(contextSkipped) > 0 {
		skippedNotice = assistant.LeftOutMessage(len(contextSkipped))
	}

	notes := *output
	if notes == "" {
		notes = assistName(time.Now())
	}
	if _, err := os.Stat(notes); err == nil {
		return usageErr("%s already exists; pick another name with -o", notes)
	}
	if info, err := os.Stat(filepath.Dir(notes)); err != nil || !info.IsDir() {
		return usageErr("folder does not exist: %s", filepath.Dir(notes))
	}

	defaults := config.ReadRecordDefaults(*configFile)
	inputs, err := recorder.Resolve(pick(*source, defaults.Source, "both"),
		pick(*mic, defaults.Mic, ""), pick(*system, defaults.System, ""))
	if err != nil {
		return usageErr("%v", err)
	}

	opts := realtime.Options{
		APIKey:   elKey,
		Language: pick(*language, cfg.Language, ""),
		Keyterms: cfg.ElevenLabsKeyterms,
		Verbatim: cfg.ElevenLabsVerbatim,
	}
	if *keyterms != "" {
		opts.Keyterms = strings.Split(*keyterms, ",")
	}
	// Names from the context ("keyterms:" lines) help live speech-to-text
	// spell what the model will look up in the context.
	rawContext := assistant.ParseKeyterms(background)
	fromContext := assistant.MergeKeyterms(nil, rawContext)
	tooLong := assistant.LongKeyterms(append(append([]string{}, opts.Keyterms...), rawContext...))
	opts.Keyterms = assistant.MergeKeyterms(assistant.MergeKeyterms(nil, opts.Keyterms), fromContext)
	opts.Keyterms = assistant.MergeKeyterms(opts.Keyterms, restoredTerms)

	sides := assistSides(inputs)
	var names []string
	for _, s := range sides {
		names = append(names, s.speaker)
	}
	engine := prov
	if m := pick(*modelFlag, harness.Models[prov], ""); m != "" {
		engine += " (" + m + ")"
	}
	header := fmt.Sprintf("%s · %s, updates every ≥%s", strings.Join(names, "+"), engine, every)
	if len(contextFiles) > 0 {
		header += fmt.Sprintf(" · context: %d file(s), %d KB", len(contextFiles), (len(background)+1023)>>10)
	}
	if len(fromContext) > 0 {
		header += fmt.Sprintf(" · %d keyterm(s) from the context", len(fromContext))
	}

	if restore != nil {
		header += " · restoring from " + restoreFrom
	}
	if libOn {
		header += fmt.Sprintf(" · librarian: %s, %d tool(s)", lib.Provider, len(lib.AllowedTools))
		if !isSet(fs, "librarian") {
			header += " (on by default; -librarian=false turns it off)"
		}
	}

	transcript := assistant.TranscriptPath(notes)

	out, _ := stdout.(*os.File)
	tuiMode := out != nil && useTUI(*plain, os.Stdin, out)
	ctx, force, interrupt, stopSignals := watchSignalsInterruptible()
	defer stopSignals()

	var toggle <-chan string
	restoreKeys := func() {}
	var screenKeys chan string
	muteHint, recapHint := "", ""
	var notices []string
	if tuiMode {
		// Bubble Tea reads the keys: Alt+m/s/w come back on screenKeys.
		screenKeys = make(chan string, 8)
		toggle = screenKeys
		notices = startupNotices(header, notes, transcript, tuiHint, tooLong, ctxBytes)
		if restoreNotice != "" {
			notices = append(notices, restoreNotice)
		}
		if skippedNotice != "" {
			notices = append(notices, skippedNotice)
		}
		if librarianNotice != "" {
			notices = append(notices, librarianNotice)
		}
	} else {
		// Mute keys as in record, and the recap key: only on a terminal (and on Linux).
		toggle, restoreKeys = startKeys(os.Stdin, assistKeys())
		keys := "Ctrl+C to stop"
		if toggle != nil {
			muteHint, recapHint = assistMuteHint(sides), string(recapKey)
			keys = fmt.Sprintf("%s to mute or unmute, %s for a recap of the last %s, Ctrl+C to stop",
				muteHint, recapHint, assistant.WindowLabel(recapWindow))
		}
		for _, line := range startupNotices(header, notes, transcript, keys, tooLong, ctxBytes) {
			fmt.Fprintln(stdout, line)
		}
		if restoreNotice != "" {
			fmt.Fprintln(stdout, restoreNotice)
		}
		if skippedNotice != "" {
			fmt.Fprintln(stdout, assistant.LeftOutShort(len(contextSkipped)))
		}
		if librarianNotice != "" {
			fmt.Fprintln(stdout, librarianNotice)
		}
	}
	defer restoreKeys()

	local, closeLocal := localFallback(cfg, opts.Language)
	defer closeLocal()
	events := streamSides(ctx, force, sides, opts, local, toggle)
	acfg := assistant.Config{
		Context:     background,
		Docs:        contextDocs,
		Notes:       notes,
		Transcript:  transcript,
		Header:      header,
		Engine:      prov,
		Interval:    every,
		Think:       thinker(prov, cfg, *modelFlag),
		Out:         stdout,
		Interactive: isTerminal(stdout),
		MuteHint:    muteHint,
		RecapKey:    recapHint,
		RecapWindow: recapWindow,
		Force:       force,
		Restore:     restore,
		RestoreFrom: restoreFrom,
		Fetch:       webpage.Fetch,
		SourcesDir:  assistant.SourcesDir(notes),
	}
	if !libOn {
		acfg.LibrarianOff = libOff
		if libWarn {
			acfg.LibrarianOff = "the librarian is off: " + libOff
		}
	} else {
		ro := harness.ResearchOptions{AllowedTools: lib.AllowedTools, Timeout: lib.Timeout, Model: lib.Model, MCPConfig: lib.MCPConfig, UserSettings: lib.UseClaudeSettings}
		acfg.Research = func(prompt string) (assistant.ResearchReply, error) {
			r, err := harness.Research(prompt, ro)
			return assistant.ResearchReply{Answer: r.Answer, Refused: r.Refused}, err
		}
		acfg.LibrarianInstructions = lib.Instructions
		acfg.LibrarianTools = lib.AllowedTools
		acfg.MaxLookups = lib.MaxLookups
	}
	if tuiMode {
		err = tuiSession{
			cfg:       acfg,
			notices:   notices,
			sides:     names,
			toggle:    screenKeys,
			interrupt: interrupt,
			start:     newTeaProgram(out),
			stdio:     captureStdio,
		}.run(events, stdout, stderr)
	} else {
		err = assistant.Run(acfg, events)
	}
	restoreKeys()
	stopSignals()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// resolveLibrarian decides whether the librarian runs. Only an explicit
// -librarian makes a failed check an error; by default the librarian is on
// when allowed_tools is set, and a failed check turns it off with a reason; warn
// is true only then, so the reason is shown as a warning.
func resolveLibrarian(explicit, flagOn bool, l config.Librarian, check func(config.Librarian) (config.Librarian, error)) (lib config.Librarian, on bool, off string, warn bool, err error) {
	switch {
	case explicit && !flagOn:
		return l, false, "the librarian is off (-librarian=false)", false, nil
	case explicit:
		lib, err = check(l)
		return lib, err == nil, "", false, err
	case len(l.AllowedTools) == 0:
		return l, false, "add read-only tools to librarian.allowed_tools in the config to use the librarian (see goscribe assist -h)", false, nil
	case l.Enabled != nil && !*l.Enabled:
		return l, false, "the librarian is off (librarian.enabled: false in the config)", false, nil
	}
	lib, err = check(l)
	if err != nil {
		return l, false, err.Error(), true, nil
	}
	return lib, true, "", false, nil
}

// checkLibrarian runs the read-only checks on the librarian config and returns
// it with mcp_config resolved.
func checkLibrarian(l config.Librarian) (config.Librarian, error) {
	if err := l.Validate(); err != nil {
		return l, err
	}
	if l.MCPConfig != "" {
		p, err := resolveMCPConfig(l.MCPConfig)
		if err != nil {
			return l, err
		}
		l.MCPConfig = p
	}
	if l.UseClaudeSettings {
		sp, err := claudeSettingsPath()
		if err == nil {
			err = checkClaudeSettings(sp)
		}
		if err != nil {
			return l, err
		}
	}
	if _, err := exec.LookPath(harness.ClaudeBinary); err != nil {
		return l, fmt.Errorf("needs the %s CLI on PATH", harness.ClaudeBinary)
	}
	return l, nil
}

// resolveMCPConfig expands a leading ~ and makes path absolute: the librarian
// runs from a temp dir, where a relative path would point nowhere. The file
// must exist.
func resolveMCPConfig(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("mcp_config: cannot expand ~: %v", err)
		}
		path = filepath.Join(home, path[1:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("mcp_config: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("mcp_config: %v", err)
	}
	return abs, nil
}

// claudeSettingsPath is the user's Claude settings file (a var for tests).
var claudeSettingsPath = func() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("use_claude_settings: cannot locate the Claude settings file: %v", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// checkClaudeSettings refuses user settings with permissions.allow rules: with
// use_claude_settings they would widen the librarian's tool list. A missing
// file is fine; an unreadable or invalid one is an error.
func checkClaudeSettings(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("use_claude_settings: cannot read %s: %v", path, err)
	}
	var st struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("use_claude_settings: %s is not valid JSON: %v", path, err)
	}
	if len(st.Permissions.Allow) > 0 {
		return fmt.Errorf("use_claude_settings: %s has %d permissions.allow rule(s), which would widen the librarian's tool list; remove them or turn use_claude_settings off", path, len(st.Permissions.Allow))
	}
	return nil
}

// loadAssistConfig reads the config file if there is one; assist works
// without it when the keys come from flags or the environment.
func loadAssistConfig(path string) (*config.Config, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".goscribe", "config.yml")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return &config.Config{}, nil
		}
	}
	return config.LoadConfig(path)
}

// checkThinker fails early when the chosen model can't be reached at all.
func checkThinker(prov string, cfg *config.Config) error {
	switch prov {
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return fmt.Errorf("-provider openai needs an OpenAI key in the config (goscribe -set-key)")
		}
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return fmt.Errorf("-provider gemini needs a Gemini key in the config (goscribe -set-gemini-key)")
		}
	case "claude":
		if _, err := exec.LookPath(harness.ClaudeBinary); err != nil {
			return fmt.Errorf("-provider claude needs the %s CLI on PATH", harness.ClaudeBinary)
		}
	case "codex":
		if _, err := exec.LookPath(harness.CodexBinary); err != nil {
			return fmt.Errorf("-provider codex needs the %s CLI on PATH", harness.CodexBinary)
		}
	case "hermes":
		if _, err := exec.LookPath(harness.HermesBinary); err != nil {
			return fmt.Errorf("-provider hermes needs the %s CLI on PATH", harness.HermesBinary)
		}
	}
	return nil
}

// thinker sends one assistant prompt through the usual provider routing.
// model, when set, replaces the default model of an API provider; the
// claude and codex models are in harness.Models.
func thinker(prov string, cfg *config.Config, model string) func(instructions, transcript string) (string, error) {
	geminiModel := pick(cfg.GeminiModel, "", "gemini-2.0-flash")
	openaiModel := "gpt-4o"
	if model != "" {
		geminiModel, openaiModel = model, model
	}
	return func(instructions, transcript string) (string, error) {
		action := &config.PostAction{
			ID:          "assist",
			Name:        "Assist",
			Prompt:      instructions,
			Model:       openaiModel,
			Temperature: 0.3,
			MaxTokens:   3000,
		}
		return provider.ProcessChunked(transcript, action, prov, cfg.OpenAIAPIKey, cfg.GeminiAPIKey, geminiModel, true)
	}
}

type assistSide struct {
	speaker, device string
}

// assistSides names each captured device after who speaks through it.
func assistSides(in recorder.Inputs) []assistSide {
	var sides []assistSide
	if in.Mic != "" {
		sides = append(sides, assistSide{"me", in.Mic})
	}
	if in.System != "" {
		sides = append(sides, assistSide{"others", in.System})
	}
	return sides
}

// localFallback transcribes with faster-whisper when ElevenLabs is out of
// quota: through one model server shared by both sides (the model loads once,
// on the first segment), else one whisper-ctranslate2 run per segment. No
// fallback when faster-whisper isn't installed. close stops the server.
func localFallback(cfg *config.Config, language string) (local realtime.LocalOptions, close func()) {
	if _, err := exec.LookPath(whisperlocal.Binary); err != nil {
		return realtime.LocalOptions{}, func() {}
	}
	model := pick(cfg.AssistLocalModel, "", "base")
	if whisperlocal.ServerAvailable() {
		server := whisperlocal.NewServer(model, language)
		return realtime.LocalOptions{Transcribe: server.Transcribe, Warm: server.Warm}, server.Close
	}
	return realtime.LocalOptions{Transcribe: func(wav string) (string, error) {
		return whisperlocal.TranscribeLive(wav, model, language)
	}}, func() {}
}

// sideForKey maps a mute key's label (recorder.MuteKeys) to the assist speaker.
func sideForKey(label string) string {
	switch label {
	case "mic":
		return "me"
	case "system":
		return "others"
	}
	return ""
}

// assistMuteHint lists the mute keys of the captured sides, e.g. "m/s".
func assistMuteHint(sides []assistSide) string {
	var keys []string
	for _, side := range sides {
		for k, label := range recorder.MuteKeys {
			if sideForKey(label) == side.speaker {
				keys = append(keys, string(k))
			}
		}
	}
	return strings.Join(keys, "/")
}

// muteGate copies captured chunks to the live transcription, replacing them
// with silence of the same length while muted: the session stays open, so
// unmuting needs no reconnect. Like recorder.Stream, it never blocks: when
// out is full (the session is redialing, or stopped), the oldest chunk goes.
// out is closed when in is.
func muteGate(in <-chan []byte, out chan []byte, muted *atomic.Bool) {
	defer close(out)
	for chunk := range in {
		if muted.Load() {
			chunk = make([]byte, len(chunk))
		}
		for sent := false; !sent; {
			select {
			case out <- chunk:
				sent = true
			default:
				select {
				case <-out:
				default:
				}
			}
		}
	}
}

// streamSides captures each side and transcribes it live, one realtime
// session per side. When ctx is cancelled, captures stop and each session
// flushes its last words; the returned channel closes once every side is
// done. force (second Ctrl+C) cuts the sessions without waiting.
// Each label received on toggle mutes or unmutes that side; nil means no mute keys.
// When ElevenLabs is out of quota, a side switches to local transcription
// (local.Transcribe nil: no fallback).
func streamSides(ctx context.Context, force <-chan struct{}, sides []assistSide, opts realtime.Options, local realtime.LocalOptions, toggle <-chan string) <-chan assistant.Event {
	events := make(chan assistant.Event, 256)
	rtCtx, cancelRT := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	muted := map[string]*atomic.Bool{}
	for _, side := range sides {
		captured := make(chan []byte, 150)
		audio := make(chan []byte, 150) // 30 s of audio while a session redials
		flag := &atomic.Bool{}          // the gate gets its own flag: the map is only read after this loop
		muted[side.speaker] = flag
		wg.Add(3)
		go func() {
			defer wg.Done()
			muteGate(captured, audio, flag)
		}()
		go func() {
			defer wg.Done()
			if err := recorder.Stream(ctx, side.device, captured); err != nil {
				events <- assistant.Event{Kind: assistant.NoticeEvent, Text: fmt.Sprintf("✖ %s: %v", side.speaker, firstLine(err.Error()))}
			}
		}()
		go func() {
			defer wg.Done()
			emit := func(e realtime.Event) { events <- assistantEvent(side.speaker, e) }
			err := realtime.Stream(rtCtx, opts, audio, emit)
			var fatal *realtime.FatalError
			if errors.As(err, &fatal) && fatal.Type == "quota_exceeded" && local.Transcribe != nil {
				events <- assistant.Event{Kind: assistant.NoticeEvent, Text: fmt.Sprintf(
					"⚠ %s: ElevenLabs is out of quota: switching to local transcription (faster-whisper: a few seconds behind, less accurate, no partial words)", side.speaker)}
				err = realtime.StreamLocal(rtCtx, local, audio, emit)
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				events <- assistant.Event{Kind: assistant.NoticeEvent, Text: fmt.Sprintf("✖ %s: live transcription stopped: %v", side.speaker, err)}
			}
		}()
	}
	if toggle != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case label := <-toggle:
					if label == "recap" {
						events <- assistant.Event{Kind: assistant.RecapEvent}
						continue
					}
					flag := muted[sideForKey(label)]
					if flag == nil {
						continue // a key for a side that isn't captured
					}
					now := !flag.Load()
					flag.Store(now)
					events <- assistant.Event{Kind: assistant.MuteEvent, Speaker: sideForKey(label), Muted: now}
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		select {
		case <-force:
			cancelRT()
		case <-rtCtx.Done():
		}
	}()
	go func() {
		wg.Wait()
		cancelRT()
		close(events)
	}()
	return events
}

func assistantEvent(speaker string, e realtime.Event) assistant.Event {
	switch e.Kind {
	case realtime.Partial:
		return assistant.Event{Kind: assistant.PartialEvent, Speaker: speaker, Text: e.Text}
	case realtime.Committed:
		return assistant.Event{Kind: assistant.LineEvent, Speaker: speaker, Text: e.Text}
	case realtime.Reconnecting:
		return assistant.Event{Kind: assistant.NoticeEvent, Text: fmt.Sprintf("↻ %s: %s", speaker, e.Text)}
	default:
		return assistant.Event{Kind: assistant.NoticeEvent, Text: fmt.Sprintf("⚠ %s: %s", speaker, e.Text)}
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// loadContextArg reads what -context names. An earlier assist notes file is
// not trusted background: its text comes back as a doc (Via "notes"), a
// source the model may cite but that stays in the untrusted block, and the
// background is empty. A folder, or a file that is not assist notes, is the
// trusted background as before, except that the earlier notes, transcripts and page copies
// inside it are left out (skipped). A saved page copy named directly is an
// untrusted doc (Via "web"). An empty path loads nothing.
func loadContextArg(path string) (background string, files []string, docs []assistant.Doc, skipped []string, err error) {
	if path == "" {
		return "", nil, nil, nil, nil
	}
	text, files, skipped, err := assistant.LoadContext(path)
	if err != nil {
		return "", nil, nil, skipped, err
	}
	if info, serr := os.Stat(path); serr == nil && !info.IsDir() {
		base := filepath.Base(path)
		if data, rerr := os.ReadFile(path); rerr == nil && assistant.IsPageCopy(string(data)) {
			return "", nil, []assistant.Doc{{Name: base, Title: base, Origin: path, Via: "web", Text: text}}, nil, nil
		}
		if data, rerr := os.ReadFile(path); rerr == nil && assistant.IsTranscript(string(data)) {
			return "", nil, []assistant.Doc{{Name: base, Title: base, Origin: path, Via: "transcript", Text: text}}, nil, nil
		}
		if _, ok, _ := assistant.LoadRestore(path); ok {
			return "", nil, []assistant.Doc{{Name: base, Title: base, Origin: path, Via: "notes", Text: text}}, nil, nil
		}
	}
	return text, files, nil, skipped, nil
}

// restoreFromContext reads the fixes and briefs of an earlier assist notes
// file named by -context. A folder, a file that is not assist notes and an
// empty path restore nothing. It also returns the file name for the feed and
// the right-hand words of the fixes, which join the live keyterms. An error is
// a notes file that cannot be trusted, such as one with two "## Fixes"
// sections.
func restoreFromContext(path string) (*assistant.Restored, string, []string, error) {
	if path == "" {
		return nil, "", nil, nil
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return nil, "", nil, nil
	}
	r, ok, err := assistant.LoadRestore(path)
	if err != nil || !ok {
		return nil, "", nil, err
	}
	var terms []string
	for _, f := range r.Fixes {
		terms = append(terms, f.Right)
	}
	return &r, filepath.Base(path), terms, nil
}
