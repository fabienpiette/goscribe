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

While it runs, press m to mute or unmute your mic and s for the computer
sound: a muted side is not transcribed (its live session gets silence), so
nothing from it reaches the model or the notes. Each press affects only
assist; mute record separately in its own terminal. Press w ("what did I
miss?") for a short recap of the last minutes (-recap, default 5m).

EXAMPLES:
  goscribe assist -context ~/notes/standup/
  goscribe assist -context agenda.md -provider claude -o standup-assist.md
  goscribe assist -source system -language fr
  goscribe assist -provider codex -model gpt-6-luna

Good to know:
  - The -context files (.md) are sent to the -provider model with every update.
    Put the agenda or goals in them: points still not covered are raised when
    the meeting wraps up. A "keyterms: Name, Term" line adds names and jargon to
    the live speech-to-text, like -keyterms.
  - Needs an ElevenLabs API key (config elevenlabs_api_key or $ELEVENLABS_API_KEY).
    Live transcription is billed per stream: two with -source both.
  - -librarian lets the model look things up in your own sources (past
    decisions, tickets, documents) through a read-only Claude Code session.
    It needs librarian.allowed_tools in the config; only list read-only tools.
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
	recap := fs.Duration("recap", 0, "How far back the recap key (w) goes (default: config assist_recap, else 5m)")
	librarian := fs.Bool("librarian", false, "Let the model look things up in your sources (librarian in the config; Claude Code)")
	configFile := fs.String("config", "", "Config file (default: ~/.goscribe/config.yml)")
	listSources := fs.Bool("list-sources", false, "List inputs and computer sound sources, then exit")
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
	if *modelFlag != "" && harness.IsHarness(prov) {
		harness.Models[prov] = *modelFlag
	}
	lib := cfg.Librarian.WithDefaults()
	if *librarian {
		if err := lib.Validate(); err != nil {
			return usageErr("librarian: %v", err)
		}
		if lib.MCPConfig != "" {
			p, err := resolveMCPConfig(lib.MCPConfig)
			if err != nil {
				return usageErr("librarian: %v", err)
			}
			lib.MCPConfig = p
		}
		if lib.UseClaudeSettings {
			sp, err := claudeSettingsPath()
			if err == nil {
				err = checkClaudeSettings(sp)
			}
			if err != nil {
				return usageErr("librarian: %v", err)
			}
		}
		if _, err := exec.LookPath(harness.ClaudeBinary); err != nil {
			return usageErr("librarian: needs the %s CLI on PATH", harness.ClaudeBinary)
		}
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

	var background string
	var contextFiles []string
	if *contextPath != "" {
		background, contextFiles, err = assistant.LoadContext(*contextPath)
		if err != nil {
			return usageErr("context: %v", err)
		}
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

	if *librarian {
		header += fmt.Sprintf(" · librarian: %s, %d tool(s)", lib.Provider, len(lib.AllowedTools))
	}

	ctx, force, stopSignals := watchSignals()
	defer stopSignals()

	// Mute keys as in record, and the recap key: only on a terminal (and on Linux).
	toggle, restoreKeys := startKeys(os.Stdin, assistKeys())
	defer restoreKeys()
	muteHint, recapHint := "", ""
	if toggle != nil {
		muteHint, recapHint = assistMuteHint(sides), string(recapKey)
		fmt.Fprintf(stdout, "Assist %s → %s (%s to mute or unmute, %s for a recap of the last %s, Ctrl+C to stop)\n",
			header, notes, muteHint, recapHint, assistant.WindowLabel(recapWindow))
	} else {
		fmt.Fprintf(stdout, "Assist %s → %s (Ctrl+C to stop)\n", header, notes)
	}
	if len(tooLong) > 0 {
		fmt.Fprintf(stdout, "⚠ %d keyterm(s) over %d characters left out (live speech-to-text refuses them): %s\n",
			len(tooLong), assistant.MaxKeytermLen, strings.Join(tooLong, ", "))
	}
	if len(background) > assistant.WarnContextBytes {
		fmt.Fprintf(stdout, "⚠ the context is %d KB: every update resends it, which is slower and costlier\n", len(background)>>10)
	}

	events := streamSides(ctx, force, sides, opts, toggle)
	acfg := assistant.Config{
		Context:     background,
		Notes:       notes,
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
	}
	if *librarian {
		ro := harness.ResearchOptions{AllowedTools: lib.AllowedTools, Timeout: lib.Timeout, Model: lib.Model, MCPConfig: lib.MCPConfig, UserSettings: lib.UseClaudeSettings}
		acfg.Research = func(prompt string) (assistant.ResearchReply, error) {
			r, err := harness.Research(prompt, ro)
			return assistant.ResearchReply{Answer: r.Answer, Refused: r.Refused}, err
		}
		acfg.LibrarianInstructions = lib.Instructions
		acfg.LibrarianTools = lib.AllowedTools
		acfg.MaxLookups = lib.MaxLookups
	}
	err = assistant.Run(acfg, events)
	restoreKeys()
	stopSignals()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
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
func streamSides(ctx context.Context, force <-chan struct{}, sides []assistSide, opts realtime.Options, toggle <-chan string) <-chan assistant.Event {
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
			err := realtime.Stream(rtCtx, opts, audio, func(e realtime.Event) {
				events <- assistantEvent(side.speaker, e)
			})
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
