package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"goscribe/internal/provider"
	"goscribe/internal/song"
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
	enableFallback  bool
	output          string
	listActions     bool
	postAction      string
	autoSelect      bool
	configFile      string
	transcriptFiles []string
	args            []string
	song            bool
	vocalExtractor  func(string) (string, func(), error)
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
	if apiKey == "XXXX" && cfg.OpenAIAPIKey != "" {
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

	geminiModel := cfg.GeminiModel
	if geminiModel == "" {
		geminiModel = "gemini-2.0-flash"
	}

	enableFallback := opts.enableFallback

	if opts.song && len(opts.transcriptFiles) > 0 {
		return fmt.Errorf("-song requires an audio file; use -transcript to process existing lyrics")
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

	var transcription string
	var audioPath string
	var originalAudioPath string
	var transcriptFilename string

	if len(opts.transcriptFiles) > 0 {
		if opts.postAction == "" && !opts.autoSelect {
			return fmt.Errorf("-action or --auto is required when using -transcript")
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

		if _, err := os.Stat(audioPath); os.IsNotExist(err) {
			return fmt.Errorf("audio file '%s' not found", audioPath)
		}
		originalAudioPath = audioPath

		if opts.song {
			fmt.Printf("Extracting vocals from %s (this may take a minute)...\n", filepath.Base(audioPath))
			extractor := opts.vocalExtractor
			if extractor == nil {
				extractor = song.ExtractVocals
			}
			vocalsPath, cleanup, err := extractor(audioPath)
			if err != nil {
				return fmt.Errorf("vocal extraction: %w", err)
			}
			defer cleanup()
			fmt.Printf("✓ Vocals extracted to %s\n", vocalsPath)
			audioPath = vocalsPath
		}

		if opts.output == "" {
			ext := filepath.Ext(originalAudioPath)
			baseName := strings.TrimSuffix(originalAudioPath, ext)
			transcriptFilename = baseName + "-transcript.txt"
		} else {
			transcriptFilename = opts.output
		}

		fmt.Printf("Transcribing audio with %s...\n", activeProvider)
		transcription, err = provider.TranscribeAudio(audioPath, activeProvider, apiKey, geminiKey, geminiModel, enableFallback)
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}

		err = os.WriteFile(transcriptFilename, []byte(transcription), 0644)
		if err != nil {
			return fmt.Errorf("writing transcript file: %w", err)
		}
		fmt.Printf("Raw transcript saved to %s\n", transcriptFilename)

		if opts.song {
			fmt.Printf("Validating lyrics with %s...\n", activeProvider)
			validation, valErr := song.ValidateLyrics(transcription, activeProvider, apiKey, geminiKey, geminiModel, enableFallback)
			if valErr != nil {
				fmt.Printf("⚠ Warning: lyrics validation failed: %v\n", valErr)
			} else {
				data, _ := json.MarshalIndent(validation, "", "  ")
				ext := filepath.Ext(opts.args[0])
				baseName := strings.TrimSuffix(opts.args[0], ext)
				validationFile := baseName + "-lyrics-validation.json"
				if err := os.WriteFile(validationFile, data, 0644); err != nil {
					fmt.Printf("⚠ Error writing validation file: %v\n", err)
				} else {
					fmt.Printf("✓ Lyrics validation saved to %s\n", validationFile)
				}
			}
		}
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
				} else if opts.song && originalAudioPath != "" {
					ext := filepath.Ext(originalAudioPath)
					baseName := strings.TrimSuffix(originalAudioPath, ext)
					processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
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
		displayPath := audioPath
		if opts.song && originalAudioPath != "" {
			displayPath = originalAudioPath
		}
		fmt.Printf("  Audio file: %s\n", displayPath)
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

func main() {
	if len(os.Args) > 1 {
		normalized, err := normalizeArgs(os.Args[1:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		os.Args = append([]string{os.Args[0]}, normalized...)
	}

	apiKey := flag.String("k", "XXXX", "OpenAI API key")
	geminiKey := flag.String("gemini-key", "", "Gemini API key")
	providerFlag := flag.String("provider", "", "AI provider: openai or gemini (default: from config or openai)")
	noFallback := flag.Bool("no-fallback", false, "Disable automatic fallback to alternate provider on failure")
	output := flag.String("o", "", "Output file name (default: same as audio file with .txt extension)")
	listActions := flag.Bool("list-actions", false, "List available post-processing actions")
	postAction := flag.String("action", "", "Post-processing action ID(s), comma-separated (use -list-actions to see options)")
	autoSelect := flag.Bool("auto", false, "Automatically select best post-processing actions based on transcript content")
	configFile := flag.String("config", "", "Path to YAML config file with custom post-actions (default: ~/.goscribe/config.yml)")
	initConfig := flag.Bool("init", false, "Reset config file to defaults (overwrites ~/.goscribe/config.yml)")
	setKey := flag.String("set-key", "", "Store OpenAI API key in config file")
	setGeminiKey := flag.String("set-gemini-key", "", "Store Gemini API key in config file")
	setProviderFlag := flag.String("set-provider", "", "Set default AI provider in config file (openai or gemini)")
	var transcriptFiles multiStringFlag
	flag.Var(&transcriptFiles, "transcript", "Process existing transcript file(s) (skips transcription)")
	songMode := flag.Bool("song", false, "Treat input as a song: extract vocals with demucs, transcribe, then validate lyrics")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "goscribe - AI-powered audio transcription with OpenAI or Gemini\n\n")
		fmt.Fprintf(os.Stderr, "USAGE:\n")
		fmt.Fprintf(os.Stderr, "  goscribe [options] <audio_file>\n\n")
		fmt.Fprintf(os.Stderr, "OPTIONS:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEXAMPLES:\n")
		fmt.Fprintf(os.Stderr, "  # Basic transcription (OpenAI)\n")
		fmt.Fprintf(os.Stderr, "  goscribe -k YOUR_API_KEY meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcription with Gemini\n")
		fmt.Fprintf(os.Stderr, "  goscribe -gemini-key YOUR_KEY -provider gemini meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcribe with meeting summary\n")
		fmt.Fprintf(os.Stderr, "  goscribe -action openai-meeting-summary meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Custom output file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -o transcript.txt audio.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # List all available post-processing actions\n")
		fmt.Fprintf(os.Stderr, "  goscribe -list-actions\n\n")
		fmt.Fprintf(os.Stderr, "  # Process existing transcript file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -transcript meeting.txt -action openai-meeting-summary\n\n")
		fmt.Fprintf(os.Stderr, "  # Automatically select best actions\n")
		fmt.Fprintf(os.Stderr, "  goscribe --auto meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Song mode: extract vocals, transcribe, validate lyrics\n")
		fmt.Fprintf(os.Stderr, "  goscribe -song concert.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Store API keys in config file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-key YOUR_OPENAI_KEY\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-gemini-key YOUR_GEMINI_KEY\n\n")
		fmt.Fprintf(os.Stderr, "  # Set default provider\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-provider gemini\n\n")
		fmt.Fprintf(os.Stderr, "  # Reset config to defaults\n")
		fmt.Fprintf(os.Stderr, "  goscribe -init\n\n")
		fmt.Fprintf(os.Stderr, "OUTPUT FILES:\n")
		fmt.Fprintf(os.Stderr, "  <filename>-transcript.txt              Raw transcription\n")
		fmt.Fprintf(os.Stderr, "  <filename>-<action-id>.txt             Post-processed output\n\n")
		fmt.Fprintf(os.Stderr, "CONFIGURATION:\n")
		fmt.Fprintf(os.Stderr, "  Config file: ~/.goscribe/config.yml\n")
		fmt.Fprintf(os.Stderr, "  - provider: default AI provider (openai or gemini)\n")
		fmt.Fprintf(os.Stderr, "  - openai_api_key: your OpenAI API key\n")
		fmt.Fprintf(os.Stderr, "  - gemini_api_key: your Gemini API key\n")
		fmt.Fprintf(os.Stderr, "  - gemini_model: default Gemini model\n\n")
		fmt.Fprintf(os.Stderr, "PROVIDERS:\n")
		fmt.Fprintf(os.Stderr, "  openai    OpenAI Whisper (transcription) + GPT (processing)\n")
		fmt.Fprintf(os.Stderr, "  gemini    Google Gemini (transcription + processing)\n\n")
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
		enableFallback:  !*noFallback,
		output:          *output,
		listActions:     *listActions,
		postAction:      *postAction,
		autoSelect:      *autoSelect,
		configFile:      *configFile,
		transcriptFiles: transcriptFiles,
		args:            flag.Args(),
		song:            *songMode,
	}

	if err := run(opts); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
