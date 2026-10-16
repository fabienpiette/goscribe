package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goscribe/internal/elevenlabs"
	"goscribe/internal/harness"
	"goscribe/internal/provider"
	"goscribe/internal/tags"
	"goscribe/pkg/config"
)

type multiStringFlag []string

func (m *multiStringFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiStringFlag) Set(value string) error {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			*m = append(*m, trimmed)
		}
	}

	return nil
}

// normalizeArgs preprocesses CLI arguments to allow multiple values after -transcript
// without repeating the flag. Returns normalized args and an error if -transcript has no value.
func normalizeArgs(rawArgs []string) ([]string, error) {
	normalized := make([]string, 0, len(rawArgs))

	for i := 0; i < len(rawArgs); i++ {
		arg := rawArgs[i]

		if arg == "-transcript" {
			normalized = append(normalized, arg)
			i++
			if i >= len(rawArgs) {
				return nil, fmt.Errorf("-transcript flag requires at least one value")
			}
			values := []string{rawArgs[i]}
			for i+1 < len(rawArgs) && !strings.HasPrefix(rawArgs[i+1], "-") {
				i++
				values = append(values, rawArgs[i])
			}
			normalized = append(normalized, strings.Join(values, ","))
			continue
		}

		if strings.HasPrefix(arg, "-transcript=") {
			value := strings.TrimPrefix(arg, "-transcript=")
			values := []string{value}
			for i+1 < len(rawArgs) && !strings.HasPrefix(rawArgs[i+1], "-") {
				i++
				values = append(values, rawArgs[i])
			}
			normalized = append(normalized, "-transcript="+strings.Join(values, ","))
			continue
		}

		normalized = append(normalized, arg)
	}

	return normalized, nil
}

type runOptions struct {
	apiKey          string
	geminiKey       string
	provider        string
	transcriber     string
	elevenLabsKey   string
	keyterms        string
	numSpeakers     int
	whisperModel    string
	language        string
	enableFallback  bool
	output          string
	listActions     bool
	tags            bool
	tagsRoot        string
	postAction      string
	autoSelect      bool
	configFile      string
	transcriptFiles []string
	args            []string
}

func run(opts runOptions) error {
	configPath := opts.configFile
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, ".goscribe", "config.yml")

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("Config file not found. Creating default config...")
			if err := config.CreateDefault(); err != nil {
				return fmt.Errorf("creating default config: %w", err)
			}
		}
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config file: %w", err)
	}

	postActions := cfg.PostActions

	apiKey := opts.apiKey
	if (apiKey == "" || apiKey == "XXXX") && cfg.OpenAIAPIKey != "" {
		apiKey = cfg.OpenAIAPIKey
		fmt.Println("Using API key from config file")
	}

	geminiKey := opts.geminiKey
	if geminiKey == "" && cfg.GeminiAPIKey != "" {
		geminiKey = cfg.GeminiAPIKey
	}

	activeProvider := "openai"
	if opts.provider != "" {
		activeProvider = opts.provider
	} else if cfg.Provider != "" {
		activeProvider = cfg.Provider
	}

	if !config.IsValidProvider(activeProvider) {
		return fmt.Errorf("invalid provider '%s' (valid: %s)", activeProvider, strings.Join(config.ValidProviders, ", "))
	}

	geminiModel := cfg.GeminiModel
	if geminiModel == "" {
		geminiModel = "gemini-2.0-flash"
	}

	enableFallback := opts.enableFallback

	transcriber := opts.transcriber
	if transcriber == "" {
		transcriber = cfg.Transcriber
	}
	if transcriber == "" {
		// Harnesses can't transcribe audio, so they pair with the offline engine.
		if harness.IsHarness(activeProvider) {
			transcriber = "local"
		} else {
			transcriber = activeProvider
		}
	}

	elevenLabsKey := pick(opts.elevenLabsKey, cfg.ElevenLabsAPIKey, os.Getenv("ELEVENLABS_API_KEY"))

	keyterms := cfg.ElevenLabsKeyterms
	if opts.keyterms != "" {
		keyterms = strings.Split(opts.keyterms, ",")
	}

	numSpeakers := cfg.ElevenLabsNumSpeakers
	if opts.numSpeakers > 0 {
		numSpeakers = opts.numSpeakers
	}

	whisperModel := opts.whisperModel
	if whisperModel == "" {
		whisperModel = cfg.WhisperModel
	}

	language := opts.language
	if language == "" {
		language = cfg.Language
	}

	if opts.listActions {
		fmt.Println("Available post-processing actions:")
		fmt.Println()
		for _, action := range postActions {
			fmt.Printf("ID: %s\n", action.ID)
			fmt.Printf("Name: %s\n", action.Name)
			fmt.Printf("Description: %s\n", action.Description)
			fmt.Printf("Model: %s\n", action.Model)
			fmt.Println(strings.Repeat("-", 70))
		}
		return nil
	}

	// Reject unknown action IDs before transcribing, so a typo doesn't cost API calls.
	if !opts.autoSelect && opts.postAction != "" {
		var unknown []string
		for _, id := range strings.Split(opts.postAction, ",") {
			id = strings.TrimSpace(id)
			if id != "" && config.FindAction(postActions, id) == nil {
				unknown = append(unknown, id)
			}
		}
		if len(unknown) > 0 {
			return fmt.Errorf("unknown action(s) '%s'. Use -list-actions to see available options", strings.Join(unknown, "', '"))
		}
	}

	var transcription string
	var audioPath string
	var transcriptFilename string

	if len(opts.transcriptFiles) > 0 {
		if opts.postAction == "" && !opts.autoSelect && !opts.tags {
			return fmt.Errorf("-action, --auto or -tags is required when using -transcript")
		}
		if opts.tags && len(opts.transcriptFiles) > 1 {
			return fmt.Errorf("-tags works with a single -transcript file")
		}

		var combined strings.Builder
		for idx, file := range opts.transcriptFiles {
			if _, err := os.Stat(file); os.IsNotExist(err) {
				return fmt.Errorf("transcript file '%s' not found", file)
			}

			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("reading transcript file '%s': %w", file, err)
			}

			content := string(data)
			if len(opts.transcriptFiles) == 1 {
				transcription = content
			} else {
				fmt.Fprintf(&combined, "Transcript %d (%s):\n\n%s", idx+1, file, content)
				if idx < len(opts.transcriptFiles)-1 {
					combined.WriteString("\n\n" + strings.Repeat("-", 70) + "\n\n")
				}
			}

			fmt.Printf("Loaded transcript from %s\n", file)
		}

		if len(opts.transcriptFiles) > 1 {
			transcription = combined.String()
		}
	} else {
		if len(opts.args) < 1 {
			return fmt.Errorf("audio file path is required")
		}
		audioPath = opts.args[0]

		if !config.IsValidTranscriber(transcriber) {
			return fmt.Errorf("invalid transcriber '%s' (valid: %s)", transcriber, strings.Join(config.ValidTranscribers, ", "))
		}

		if _, err := os.Stat(audioPath); os.IsNotExist(err) {
			return fmt.Errorf("audio file '%s' not found", audioPath)
		}

		if opts.output == "" {
			ext := filepath.Ext(audioPath)
			baseName := strings.TrimSuffix(audioPath, ext)
			transcriptFilename = baseName + "-transcript.txt"
		} else {
			transcriptFilename = opts.output
		}

		fmt.Printf("Transcribing audio with %s...\n", transcriber)
		transcription, err = provider.Transcribe(audioPath, provider.TranscribeOptions{
			Transcriber: transcriber,
			Provider:    activeProvider,
			OpenAIKey:   apiKey,
			GeminiKey:   geminiKey,
			GeminiModel: geminiModel,
			ElevenLabs: elevenlabs.Options{
				APIKey:      elevenLabsKey,
				Model:       cfg.ElevenLabsModel,
				Keyterms:    keyterms,
				NumSpeakers: numSpeakers,
				Verbatim:    cfg.ElevenLabsVerbatim,
			},
			WhisperModel: whisperModel,
			Language:     language,
			Fallback:     enableFallback,
		})
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}

		err = os.WriteFile(transcriptFilename, []byte(transcription), 0644)
		if err != nil {
			return fmt.Errorf("writing transcript file: %w", err)
		}
		fmt.Printf("Raw transcript saved to %s\n", transcriptFilename)
	}

	var processedFiles []string
	var actionIDs []string

	if opts.autoSelect {
		fmt.Printf("\n🤖 Analyzing transcript with %s to select best actions...\n", activeProvider)
		selectedActions, err := provider.SelectBestActions(transcription, postActions, activeProvider, apiKey, geminiKey, geminiModel)
		if err != nil {
			fmt.Printf("⚠ Warning: Auto-selection failed: %v\n", err)
			fmt.Println("Continuing without post-processing.")
		} else {
			actionIDs = selectedActions
			fmt.Printf("✓ Selected %d action(s): %s\n", len(actionIDs), strings.Join(actionIDs, ", "))
		}
	} else if opts.postAction != "" {
		actionIDs = strings.Split(opts.postAction, ",")
	}

	for i, id := range actionIDs {
		actionIDs[i] = strings.TrimSpace(id)
	}

	if len(actionIDs) > 0 {
		fmt.Printf("\nProcessing %d action(s)...\n", len(actionIDs))

		for idx, actionID := range actionIDs {
			if actionID == "" {
				continue
			}

			action := config.FindAction(postActions, actionID)
			if action == nil {
				return fmt.Errorf("unknown action '%s'. Use -list-actions to see available options", actionID)
			}

			fmt.Printf("\n[%d/%d] Applying post-processing with %s: %s...\n", idx+1, len(actionIDs), activeProvider, action.Name)
			processed, err := provider.ProcessChunked(transcription, action, activeProvider, apiKey, geminiKey, geminiModel, enableFallback)
			if err != nil {
				fmt.Printf("⚠ Warning: Post-processing failed: %v\n", err)
				if len(opts.transcriptFiles) == 0 && len(actionIDs) == 1 {
					fmt.Println("Only raw transcript was saved.")
				}
			} else {
				var processedFilename string
				if len(opts.transcriptFiles) > 0 {
					first := opts.transcriptFiles[0]
					ext := filepath.Ext(first)
					baseName := strings.TrimSuffix(first, ext)
					if len(opts.transcriptFiles) == 1 {
						processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
					} else {
						processedFilename = fmt.Sprintf("%s+%d-%s.txt", baseName, len(opts.transcriptFiles)-1, action.ID)
					}
				} else {
					ext := filepath.Ext(audioPath)
					baseName := strings.TrimSuffix(audioPath, ext)
					processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
				}

				err = os.WriteFile(processedFilename, []byte(processed), 0644)
				if err != nil {
					fmt.Printf("⚠ Error writing processed file: %v\n", err)
				} else {
					fmt.Printf("✓ Post-processed output saved to %s\n", processedFilename)
					processedFiles = append(processedFiles, processedFilename)
				}
			}
		}

		if len(processedFiles) > 0 {
			fmt.Printf("\n✓ Post-processing completed! Generated %d file(s)\n", len(processedFiles))
		}
	}

	if opts.tags {
		source := transcriptFilename
		if len(opts.transcriptFiles) > 0 {
			source = opts.transcriptFiles[0]
		}
		tagsRoot := opts.tagsRoot
		if tagsRoot == "" {
			tagsRoot = cfg.TagsRoot
		}
		fmt.Printf("\n🏷  Tagging transcript with %s...\n", activeProvider)
		tagsFile, err := writeTags(source, transcription, tagsRoot, func(action *config.PostAction) (string, error) {
			return provider.ProcessChunked(transcription, action, activeProvider, apiKey, geminiKey, geminiModel, enableFallback)
		})
		if err != nil {
			// Like actions, a tagging failure must not lose the transcript.
			fmt.Printf("⚠ Warning: Tagging failed: %v\n", err)
		} else {
			fmt.Printf("✓ Tags saved to %s\n", tagsFile)
			processedFiles = append(processedFiles, tagsFile)
		}
	}

	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Summary:\n")
	if len(opts.transcriptFiles) > 0 {
		if len(opts.transcriptFiles) == 1 {
			fmt.Printf("  Transcript: %s\n", opts.transcriptFiles[0])
		} else {
			fmt.Printf("  Transcripts (%d):\n", len(opts.transcriptFiles))
			for _, tf := range opts.transcriptFiles {
				fmt.Printf("    - %s\n", tf)
			}
		}
	} else {
		fmt.Printf("  Audio file: %s\n", audioPath)
		fmt.Printf("  Transcript: %s\n", transcriptFilename)
	}
	if len(processedFiles) > 0 {
		fmt.Printf("  Processed files (%d):\n", len(processedFiles))
		for _, pf := range processedFiles {
			fmt.Printf("    - %s\n", pf)
		}
	}
	if apiKey != "XXXX" && len(apiKey) > 0 {
		fmt.Printf("  API key:    [configured]\n")
	}
	fmt.Println(strings.Repeat("=", 70))

	return nil
}

// writeTags asks the model for tags (via process) and writes them next to source.
// The vocabulary comes from existing tag files under root (default: the parent
// of source's folder, i.e. the records root in a records/<date>/ layout).
func writeTags(source, transcription, root string, process func(*config.PostAction) (string, error)) (string, error) {
	if n := len(strings.Fields(transcription)); n < tags.MinWords {
		return "", fmt.Errorf("transcript too short to tag (%d words, need %d)", n, tags.MinWords)
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	if root == "" {
		root = filepath.Dir(filepath.Dir(abs))
	}

	action := &config.PostAction{
		ID:          "tags",
		Name:        "Tags",
		Prompt:      tags.Prompt(tags.CollectVocabulary(root)),
		Model:       "gpt-4o",
		Temperature: 0.2,
		MaxTokens:   1000,
	}
	answer, err := process(action)
	if err != nil {
		return "", err
	}
	t, err := tags.Parse(answer)
	if err != nil {
		return "", err
	}
	t.Date = tags.DateFromName(source)

	path := tags.Path(source)
	if err := tags.Write(path, t); err != nil {
		return "", err
	}
	return path, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "search" {
		os.Exit(runSearch(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "record" {
		os.Exit(runRecord(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "assist" {
		os.Exit(runAssist(os.Args[2:], os.Stdout, os.Stderr))
	}

	if len(os.Args) > 1 {
		normalized, err := normalizeArgs(os.Args[1:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		os.Args = append([]string{os.Args[0]}, normalized...)
	}

	apiKey := flag.String("k", "", "OpenAI API key (default: from config)")
	geminiKey := flag.String("gemini-key", "", "Gemini API key")
	providerFlag := flag.String("provider", "", "AI provider for actions: openai, gemini, claude or codex (default: from config or openai)")
	transcriberFlag := flag.String("transcriber", "", "Transcription engine: local, elevenlabs, openai or gemini (default: from config, else same as -provider, or local with claude/codex)")
	elevenLabsKey := flag.String("elevenlabs-key", "", "ElevenLabs API key (default: from config or $ELEVENLABS_API_KEY)")
	keytermsFlag := flag.String("keyterms", "", "Comma-separated names/jargon ElevenLabs should favor, e.g. \"Acme,Globex\" (default: from config; 20% surcharge)")
	numSpeakers := flag.Int("num-speakers", 0, "Max number of speakers for ElevenLabs diarization (default: from config, else auto)")
	whisperModel := flag.String("whisper-model", "", "Whisper model for -transcriber local (default: from config or small)")
	language := flag.String("language", "", "Audio language code, e.g. en or fr, for local and elevenlabs (default: auto-detect)")
	noFallback := flag.Bool("no-fallback", false, "Disable automatic fallback to alternate provider on failure")
	output := flag.String("o", "", "Transcript file name (default: <audio name>-transcript.txt next to the audio)")
	listActions := flag.Bool("list-actions", false, "List available post-processing actions")
	postAction := flag.String("action", "", "Post-processing action ID(s), comma-separated (use -list-actions to see options)")
	tagsFlag := flag.Bool("tags", false, "Write <name>-tags.yml (type, summary, topics, people, tickets, projects) next to the transcript, for grep")
	tagsRoot := flag.String("tags-root", "", "Folder whose existing tag files seed the tag vocabulary (default: config tags_root, else the transcript folder's parent)")
	autoSelect := flag.Bool("auto", false, "Automatically select best post-processing actions based on transcript content")
	configFile := flag.String("config", "", "Config file: keys, defaults and post-processing actions (default: ~/.goscribe/config.yml)")
	initConfig := flag.Bool("init", false, "Reset config file to defaults (overwrites ~/.goscribe/config.yml)")
	setKey := flag.String("set-key", "", "Store OpenAI API key in config file")
	setGeminiKey := flag.String("set-gemini-key", "", "Store Gemini API key in config file")
	setProviderFlag := flag.String("set-provider", "", "Set default AI provider in config file (openai, gemini, claude or codex)")
	setTranscriberFlag := flag.String("set-transcriber", "", "Set default transcription engine in config file (local, elevenlabs, openai or gemini)")
	var transcriptFiles multiStringFlag
	flag.Var(&transcriptFiles, "transcript", "Process existing transcript file(s) (skips transcription)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "goscribe - AI-powered audio transcription with OpenAI, Gemini, ElevenLabs or local Whisper\n\n")
		fmt.Fprintf(os.Stderr, "USAGE:\n")
		fmt.Fprintf(os.Stderr, "  goscribe [options] <audio_file>\n")
		fmt.Fprintf(os.Stderr, "  goscribe search [flags] [terms...]   find meetings by tags (goscribe search -h)\n")
		fmt.Fprintf(os.Stderr, "  goscribe record [flags]               record mic and/or computer sound to mp3 (goscribe record -h)\n")
		fmt.Fprintf(os.Stderr, "  goscribe assist [flags]               live meeting copilot only you can see (goscribe assist -h)\n\n")
		fmt.Fprintf(os.Stderr, "OPTIONS:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEXAMPLES:\n")
		fmt.Fprintf(os.Stderr, "  # Basic transcription (OpenAI)\n")
		fmt.Fprintf(os.Stderr, "  goscribe -k YOUR_API_KEY meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcription with Gemini\n")
		fmt.Fprintf(os.Stderr, "  goscribe -gemini-key YOUR_KEY -provider gemini meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcribe locally (offline), summarize with OpenAI\n")
		fmt.Fprintf(os.Stderr, "  goscribe -transcriber local -action openai-meeting-summary meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcribe a meeting with ElevenLabs (speaker turns with timestamps)\n")
		fmt.Fprintf(os.Stderr, "  ELEVENLABS_API_KEY=... goscribe -transcriber elevenlabs -num-speakers 4 -keyterms \"Acme,Globex\" meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Run actions with your local Claude Code (or codex) login\n")
		fmt.Fprintf(os.Stderr, "  goscribe -transcriber local -provider claude -action openai-meeting-summary meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcribe with meeting summary\n")
		fmt.Fprintf(os.Stderr, "  goscribe -action openai-meeting-summary meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Custom output file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -o transcript.txt audio.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # List all available post-processing actions\n")
		fmt.Fprintf(os.Stderr, "  goscribe -list-actions\n\n")
		fmt.Fprintf(os.Stderr, "  # Tag a transcript for grep (also works on new audio: add -tags)\n")
		fmt.Fprintf(os.Stderr, "  goscribe -provider codex -transcript meeting-transcript.txt -tags\n")
		fmt.Fprintf(os.Stderr, "  grep -rlw dark-mode --include='*-tags.yml' ~/records\n\n")
		fmt.Fprintf(os.Stderr, "  # Process existing transcript file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -transcript meeting.txt -action openai-meeting-summary\n\n")
		fmt.Fprintf(os.Stderr, "  # Automatically select best actions\n")
		fmt.Fprintf(os.Stderr, "  goscribe --auto meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Store API keys in config file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-key YOUR_OPENAI_KEY\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-gemini-key YOUR_GEMINI_KEY\n\n")
		fmt.Fprintf(os.Stderr, "  # Set default provider\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-provider gemini\n\n")
		fmt.Fprintf(os.Stderr, "  # Set default transcription engine\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-transcriber local\n\n")
		fmt.Fprintf(os.Stderr, "  # Reset config to defaults\n")
		fmt.Fprintf(os.Stderr, "  goscribe -init\n\n")
		fmt.Fprintf(os.Stderr, "OUTPUT FILES:\n")
		fmt.Fprintf(os.Stderr, "  <filename>-transcript.txt              Raw transcription\n")
		fmt.Fprintf(os.Stderr, "  <filename>-<action-id>.txt             Post-processed output\n")
		fmt.Fprintf(os.Stderr, "  <filename>-tags.yml                    Tags for grep (with -tags)\n\n")
		fmt.Fprintf(os.Stderr, "CONFIGURATION:\n")
		fmt.Fprintf(os.Stderr, "  Config file: ~/.goscribe/config.yml\n")
		fmt.Fprintf(os.Stderr, "  - provider: default AI provider (openai or gemini)\n")
		fmt.Fprintf(os.Stderr, "  - openai_api_key: your OpenAI API key\n")
		fmt.Fprintf(os.Stderr, "  - gemini_api_key: your Gemini API key\n")
		fmt.Fprintf(os.Stderr, "  - gemini_model: default Gemini model\n")
		fmt.Fprintf(os.Stderr, "  - transcriber: default transcription engine\n")
		fmt.Fprintf(os.Stderr, "  - elevenlabs_api_key / elevenlabs_model: ElevenLabs settings (model default: scribe_v2)\n")
		fmt.Fprintf(os.Stderr, "  - elevenlabs_keyterms / elevenlabs_num_speakers / elevenlabs_verbatim: ElevenLabs tuning\n")
		fmt.Fprintf(os.Stderr, "  - whisper_model: local Whisper model (default: small)\n")
		fmt.Fprintf(os.Stderr, "  - language: audio language code for local and elevenlabs\n\n")
		fmt.Fprintf(os.Stderr, "PROVIDERS:\n")
		fmt.Fprintf(os.Stderr, "  openai    OpenAI Whisper (transcription) + GPT (processing)\n")
		fmt.Fprintf(os.Stderr, "  gemini    Google Gemini (transcription + processing)\n")
		fmt.Fprintf(os.Stderr, "  claude    Local Claude Code CLI (processing only; uses your claude login and default model)\n")
		fmt.Fprintf(os.Stderr, "  codex     Local Codex CLI (processing only; uses your codex login and default model)\n\n")
		fmt.Fprintf(os.Stderr, "TRANSCRIBERS (-transcriber, audio to text only; -provider still runs actions):\n")
		fmt.Fprintf(os.Stderr, "  local       faster-whisper on this machine, offline (needs: uv tool install whisper-ctranslate2)\n")
		fmt.Fprintf(os.Stderr, "  elevenlabs  ElevenLabs Scribe: speaker turns with timestamps, filler words removed (needs an API key)\n")
		fmt.Fprintf(os.Stderr, "  openai      OpenAI Whisper API\n")
		fmt.Fprintf(os.Stderr, "  gemini      Google Gemini\n\n")
		fmt.Fprintf(os.Stderr, "FALLBACK:\n")
		fmt.Fprintf(os.Stderr, "  When both API keys are configured, goscribe automatically\n")
		fmt.Fprintf(os.Stderr, "  falls back to the other provider if one fails.\n")
		fmt.Fprintf(os.Stderr, "  Use -no-fallback to disable this behavior.\n\n")
		fmt.Fprintf(os.Stderr, "For more information, visit: https://github.com/fabienpiette/goscribe\n")
	}

	flag.Parse()

	if *setKey != "" {
		if err := config.StoreAPIKey(*setKey); err != nil {
			fmt.Printf("Error storing API key: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *setGeminiKey != "" {
		if err := config.StoreGeminiAPIKey(*setGeminiKey); err != nil {
			fmt.Printf("Error storing Gemini API key: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *setProviderFlag != "" {
		if err := config.SetDefaultProvider(*setProviderFlag); err != nil {
			fmt.Printf("Error setting provider: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *setTranscriberFlag != "" {
		if err := config.SetDefaultTranscriber(*setTranscriberFlag); err != nil {
			fmt.Printf("Error setting transcriber: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *initConfig {
		if err := config.Reset(); err != nil {
			fmt.Printf("Error resetting config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	opts := runOptions{
		apiKey:          *apiKey,
		geminiKey:       *geminiKey,
		provider:        *providerFlag,
		transcriber:     *transcriberFlag,
		elevenLabsKey:   *elevenLabsKey,
		keyterms:        *keytermsFlag,
		numSpeakers:     *numSpeakers,
		whisperModel:    *whisperModel,
		language:        *language,
		enableFallback:  !*noFallback,
		output:          *output,
		listActions:     *listActions,
		postAction:      *postAction,
		tags:            *tagsFlag,
		tagsRoot:        *tagsRoot,
		autoSelect:      *autoSelect,
		configFile:      *configFile,
		transcriptFiles: transcriptFiles,
		args:            flag.Args(),
	}

	if err := run(opts); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
