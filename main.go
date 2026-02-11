package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type TranscriptionResponse struct {
	Text string `json:"text"`
}

type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Gemini API types
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text       string           `json:"text,omitempty"`
	InlineData *GeminiInlineData `json:"inlineData,omitempty"`
}

type GeminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // base64 encoded
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
	Error      *GeminiError      `json:"error,omitempty"`
}

type GeminiCandidate struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}

type GeminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type PostAction struct {
	ID          string  `yaml:"id"`
	Name        string  `yaml:"name"`
	Description string  `yaml:"description"`
	Type        string  `yaml:"type"`
	Prompt      string  `yaml:"prompt"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxTokens   int     `yaml:"max_tokens"`
}

type Config struct {
	Provider     string       `yaml:"provider"`       // "openai" or "gemini" (default: openai)
	OpenAIAPIKey string       `yaml:"openai_api_key"`
	GeminiAPIKey string       `yaml:"gemini_api_key"`
	GeminiModel  string       `yaml:"gemini_model"`   // default Gemini model
	PostActions  []PostAction `yaml:"post_actions"`
}

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

var postActions = []PostAction{}

// Base URLs for API endpoints (overridable in tests)
var openAIBaseURL = "https://api.openai.com/v1"
var geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

const maxFileSizeBytes = 25 * 1024 * 1024 // 25MB - OpenAI Whisper API limit

// Approximate token limits for different models (leaving room for prompt and response)
const avgCharsPerToken = 4 // Rough estimate: 1 token ≈ 4 characters

// getModelContextLimit returns the safe context limit for a model (input tokens only)
func getModelContextLimit(model string) int {
	switch {
	// OpenAI models
	case model == "gpt-4":
		return 6000 // 8K total, leaving 2K for completion
	case model == "gpt-4-32k":
		return 24000 // 32K total, leaving 8K for completion
	case strings.HasPrefix(model, "gpt-4-turbo") || model == "gpt-4-1106-preview" || model == "gpt-4-0125-preview":
		return 100000 // 128K total, leaving 28K for completion
	case strings.HasPrefix(model, "gpt-4o"):
		return 100000 // 128K total
	case strings.HasPrefix(model, "gpt-3.5-turbo"):
		return 12000 // 16K total, leaving 4K for completion
	// Gemini models
	case strings.HasPrefix(model, "gemini-2"):
		return 900000 // 1M context, leaving room for response
	case strings.HasPrefix(model, "gemini-1.5"):
		return 900000 // 1M context
	case strings.HasPrefix(model, "gemini-1.0"):
		return 28000 // 32K context
	case strings.HasPrefix(model, "gemini"):
		return 28000 // Conservative default for unknown Gemini models
	default:
		return 6000 // Conservative default
	}
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

// runOptions holds all resolved CLI options for the run function
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
	args            []string // remaining positional args
}

// run contains the core application logic, separated from main for testability
func run(opts runOptions) error {
	// Determine which config file to use
	configPath := opts.configFile
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("getting home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, ".goscribe", "config.yml")

		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("Config file not found. Creating default config...")
			if err := createDefaultConfig(); err != nil {
				return fmt.Errorf("creating default config: %w", err)
			}
		}
	}

	config, err := loadConfigActions(configPath)
	if err != nil {
		return fmt.Errorf("loading config file: %w", err)
	}

	// Resolve API keys from config
	apiKey := opts.apiKey
	if apiKey == "XXXX" && config.OpenAIAPIKey != "" {
		apiKey = config.OpenAIAPIKey
		fmt.Println("Using API key from config file")
	}

	geminiKey := opts.geminiKey
	if geminiKey == "" && config.GeminiAPIKey != "" {
		geminiKey = config.GeminiAPIKey
	}

	// Determine active provider: flag > config > default
	activeProvider := "openai"
	if opts.provider != "" {
		activeProvider = opts.provider
	} else if config.Provider != "" {
		activeProvider = config.Provider
	}

	geminiModel := config.GeminiModel
	if geminiModel == "" {
		geminiModel = "gemini-2.0-flash"
	}

	enableFallback := opts.enableFallback

	// List actions and exit if requested
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
	var transcriptFilename string

	// Handle transcript file mode
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

		if opts.output == "" {
			ext := filepath.Ext(audioPath)
			baseName := strings.TrimSuffix(audioPath, ext)
			transcriptFilename = baseName + "-transcript.txt"
		} else {
			transcriptFilename = opts.output
		}

		fmt.Printf("Transcribing audio with %s...\n", activeProvider)
		transcription, err = transcribeAudioWithProvider(audioPath, activeProvider, apiKey, geminiKey, geminiModel, enableFallback)
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}

		err = os.WriteFile(transcriptFilename, []byte(transcription), 0644)
		if err != nil {
			return fmt.Errorf("writing transcript file: %w", err)
		}
		fmt.Printf("Raw transcript saved to %s\n", transcriptFilename)
	}

	// Resolve action IDs
	var processedFiles []string
	var actionIDs []string

	if opts.autoSelect {
		fmt.Printf("\n🤖 Analyzing transcript with %s to select best actions...\n", activeProvider)
		selectedActions, err := selectBestActionsWithProvider(transcription, activeProvider, apiKey, geminiKey, geminiModel)
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

	// Process selected actions
	if len(actionIDs) > 0 {
		fmt.Printf("\nProcessing %d action(s)...\n", len(actionIDs))

		for idx, actionID := range actionIDs {
			if actionID == "" {
				continue
			}

			action := findAction(actionID)
			if action == nil {
				return fmt.Errorf("unknown action '%s'. Use -list-actions to see available options", actionID)
			}

			fmt.Printf("\n[%d/%d] Applying post-processing with %s: %s...\n", idx+1, len(actionIDs), activeProvider, action.Name)
			processed, err := processWithProviderChunked(transcription, action, activeProvider, apiKey, geminiKey, geminiModel, enableFallback)
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

	// Print confirmation summary
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
	if apiKey != "XXXX" {
		fmt.Printf("  API key:    %s\n", apiKey)
	}
	fmt.Println(strings.Repeat("=", 70))

	return nil
}

func main() {
	// Preprocess arguments
	if len(os.Args) > 1 {
		normalized, err := normalizeArgs(os.Args[1:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		os.Args = append([]string{os.Args[0]}, normalized...)
	}

	// Define command-line flags
	apiKey := flag.String("k", "XXXX", "OpenAI API key")
	geminiKey := flag.String("gemini-key", "", "Gemini API key")
	provider := flag.String("provider", "", "AI provider: openai or gemini (default: from config or openai)")
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

	// Custom usage message
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

	// Handle one-shot config commands
	if *setKey != "" {
		if err := storeAPIKey(*setKey); err != nil {
			fmt.Printf("Error storing API key: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *setGeminiKey != "" {
		if err := storeGeminiAPIKey(*setGeminiKey); err != nil {
			fmt.Printf("Error storing Gemini API key: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *setProviderFlag != "" {
		if err := setDefaultProvider(*setProviderFlag); err != nil {
			fmt.Printf("Error setting provider: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *initConfig {
		if err := resetConfig(); err != nil {
			fmt.Printf("Error resetting config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Run core logic
	opts := runOptions{
		apiKey:          *apiKey,
		geminiKey:       *geminiKey,
		provider:        *provider,
		enableFallback:  !*noFallback,
		output:          *output,
		listActions:     *listActions,
		postAction:      *postAction,
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

func findAction(id string) *PostAction {
	for i := range postActions {
		if postActions[i].ID == id {
			return &postActions[i]
		}
	}
	return nil
}

func loadConfigActions(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Load actions from config file
	postActions = config.PostActions
	fmt.Printf("Loaded %d action(s) from config file\n", len(config.PostActions))

	return &config, nil
}

func validateConfig(config *Config) error {
	// Validate provider if set
	if config.Provider != "" && config.Provider != "openai" && config.Provider != "gemini" {
		return fmt.Errorf("invalid provider '%s' (valid: openai, gemini)", config.Provider)
	}

	if len(config.PostActions) == 0 {
		return fmt.Errorf("no post-processing actions defined in config")
	}

	// Track unique IDs
	seenIDs := make(map[string]bool)

	// Valid types and models
	validTypes := map[string]bool{
		"openai": true,
		"gemini": true,
	}

	validOpenAIModels := map[string]bool{
		"gpt-3.5-turbo": true,
		"gpt-4":         true,
		"gpt-4-turbo":   true,
		"gpt-4o":        true,
		"gpt-4o-mini":   true,
	}

	validGeminiModels := map[string]bool{
		"gemini-2.0-flash":    true,
		"gemini-1.5-pro":      true,
		"gemini-1.5-flash":    true,
		"gemini-1.5-flash-8b": true,
		"gemini-1.0-pro":      true,
	}

	for i, action := range config.PostActions {
		// Check required fields
		if action.ID == "" {
			return fmt.Errorf("action at index %d is missing 'id' field", i)
		}
		if action.Name == "" {
			return fmt.Errorf("action '%s' is missing 'name' field", action.ID)
		}
		if action.Type == "" {
			return fmt.Errorf("action '%s' is missing 'type' field", action.ID)
		}
		if action.Prompt == "" {
			return fmt.Errorf("action '%s' is missing 'prompt' field", action.ID)
		}
		if action.Model == "" {
			return fmt.Errorf("action '%s' is missing 'model' field", action.ID)
		}

		// Check for duplicate IDs
		if seenIDs[action.ID] {
			return fmt.Errorf("duplicate action ID '%s' found", action.ID)
		}
		seenIDs[action.ID] = true

		// Validate type
		if !validTypes[action.Type] {
			return fmt.Errorf("action '%s' has invalid type '%s' (valid: openai, gemini)", action.ID, action.Type)
		}

		// Validate temperature range
		if action.Temperature < 0 || action.Temperature > 2 {
			return fmt.Errorf("action '%s' has invalid temperature %.2f (must be between 0 and 2)", action.ID, action.Temperature)
		}

		// Validate max_tokens
		if action.MaxTokens <= 0 {
			return fmt.Errorf("action '%s' has invalid max_tokens %d (must be > 0)", action.ID, action.MaxTokens)
		}

		// Validate model names based on type
		if action.Type == "openai" && !validOpenAIModels[action.Model] {
			fmt.Printf("Warning: action '%s' uses model '%s' which may not be a recognized OpenAI model\n", action.ID, action.Model)
		}
		if action.Type == "gemini" && !validGeminiModels[action.Model] {
			fmt.Printf("Warning: action '%s' uses model '%s' which may not be a recognized Gemini model\n", action.ID, action.Model)
		}
	}

	return nil
}

func createDefaultConfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".goscribe")
	configFile := filepath.Join(configDir, "config.yml")

	// Create directory if it doesn't exist
	err = os.MkdirAll(configDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Get default config content with all built-in actions
	defaultConfig := getDefaultConfigContent()

	// Write config file
	err = os.WriteFile(configFile, []byte(defaultConfig), 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("✓ Created default config file at: %s\n", configFile)
	fmt.Println("\nYou can now:")
	fmt.Println("  1. Edit the config file to customize your actions")
	fmt.Printf("  2. Use: goscribe -list-actions to see all available actions\n")
	fmt.Printf("  3. Use: goscribe -action openai-meeting-summary audio.mp3\n")

	return nil
}

func resetConfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configFile := filepath.Join(homeDir, ".goscribe", "config.yml")

	// Check if config exists
	if _, err := os.Stat(configFile); err == nil {
		fmt.Printf("⚠ Warning: This will overwrite your existing config at: %s\n", configFile)
		fmt.Print("Continue? [y/N] ")

		var response string
		if _, err := fmt.Scanln(&response); err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Println("Config reset cancelled.")
			return nil
		}
	}

	// Use createDefaultConfig to write the new config
	err = createDefaultConfig()
	if err != nil {
		return err
	}

	fmt.Println("✓ Config file reset to defaults")
	return nil
}

func storeAPIKey(apiKey string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".goscribe")
	configFile := filepath.Join(configDir, "config.yml")

	// Create default config if it doesn't exist
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Config file not found. Creating default config...")
		if err := createDefaultConfig(); err != nil {
			return fmt.Errorf("failed to create default config: %w", err)
		}
	}

	// Read existing config
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Update API key
	config.OpenAIAPIKey = apiKey

	// Marshal back to YAML
	updatedData, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write updated config
	err = os.WriteFile(configFile, updatedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("✓ API key stored successfully in: %s\n", configFile)
	fmt.Println("\nYou can now use goscribe without the -k flag:")
	fmt.Println("  goscribe audio.mp3")
	fmt.Println("  goscribe -action openai-meeting-summary meeting.mp3")

	return nil
}

func storeGeminiAPIKey(apiKey string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".goscribe")
	configFile := filepath.Join(configDir, "config.yml")

	// Create default config if it doesn't exist
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Config file not found. Creating default config...")
		if err := createDefaultConfig(); err != nil {
			return fmt.Errorf("failed to create default config: %w", err)
		}
	}

	// Read existing config
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Update Gemini API key
	config.GeminiAPIKey = apiKey

	// Marshal back to YAML
	updatedData, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write updated config
	err = os.WriteFile(configFile, updatedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("✓ Gemini API key stored successfully in: %s\n", configFile)
	fmt.Println("\nYou can now use goscribe with Gemini:")
	fmt.Println("  goscribe -provider gemini audio.mp3")
	fmt.Println("  goscribe -provider gemini -action openai-meeting-summary meeting.mp3")

	return nil
}

func setDefaultProvider(providerName string) error {
	if providerName != "openai" && providerName != "gemini" {
		return fmt.Errorf("invalid provider '%s' (valid: openai, gemini)", providerName)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".goscribe")
	configFile := filepath.Join(configDir, "config.yml")

	// Create default config if it doesn't exist
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Config file not found. Creating default config...")
		if err := createDefaultConfig(); err != nil {
			return fmt.Errorf("failed to create default config: %w", err)
		}
	}

	// Read existing config
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Update provider
	config.Provider = providerName

	// Marshal back to YAML
	updatedData, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write updated config
	err = os.WriteFile(configFile, updatedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("✓ Default provider set to '%s' in: %s\n", providerName, configFile)

	return nil
}

// parseRateLimitWaitTime extracts the wait time from OpenAI rate limit error messages
// Example: "Please try again in 9.798s" -> 9.798
func parseRateLimitWaitTime(errorBody string) time.Duration {
	// Try to parse "Please try again in X.XXXs" pattern
	re := regexp.MustCompile(`try again in ([\d.]+)s`)
	matches := re.FindStringSubmatch(errorBody)
	if len(matches) >= 2 {
		if seconds, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return time.Duration(seconds*1000) * time.Millisecond
		}
	}
	// Default fallback: 10 seconds if parsing fails
	return 10 * time.Second
}

// makeOpenAIRequest makes an HTTP request to OpenAI API with retry logic
func makeOpenAIRequest(reqBody ChatCompletionRequest, apiKey string, maxRetries int) (*ChatCompletionResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", openAIBaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Handle rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := parseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("API request failed with status 429: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return nil, lastErr
		}

		// Handle other non-OK status codes
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}

		// Success! Parse and return response
		var chatResp ChatCompletionResponse
		err = json.Unmarshal(respBody, &chatResp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if len(chatResp.Choices) == 0 {
			return nil, fmt.Errorf("no response from API")
		}

		return &chatResp, nil
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// makeGeminiRequest makes an HTTP request to Gemini API with retry logic
func makeGeminiRequest(model string, contents []GeminiContent, apiKey string, maxRetries int) (*GeminiResponse, error) {
	var lastErr error
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", geminiBaseURL, model)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		reqBody := GeminiRequest{Contents: contents}
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("x-goog-api-key", apiKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Handle rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := parseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("Gemini API rate limit: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return nil, lastErr
		}

		// Handle other non-OK status codes
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(respBody))
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}

		// Success! Parse and return response
		var geminiResp GeminiResponse
		err = json.Unmarshal(respBody, &geminiResp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
		}

		if geminiResp.Error != nil {
			return nil, fmt.Errorf("Gemini API error: %s", geminiResp.Error.Message)
		}

		if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("no response from Gemini API")
		}

		return &geminiResp, nil
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// getMimeType returns the MIME type for an audio file extension
func getMimeType(ext string) string {
	mimeTypes := map[string]string{
		".wav":  "audio/wav",
		".mp3":  "audio/mp3",
		".aiff": "audio/aiff",
		".aac":  "audio/aac",
		".ogg":  "audio/ogg",
		".flac": "audio/flac",
		".m4a":  "audio/mp4",
		".mp4":  "audio/mp4",
		".mpeg": "audio/mpeg",
		".mpga": "audio/mpeg",
		".webm": "audio/webm",
	}
	return mimeTypes[strings.ToLower(ext)]
}

// transcribeWithGemini transcribes audio using Gemini API
func transcribeWithGemini(audioPath, apiKey, model string) (string, error) {
	// Read and encode audio file
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}

	// Check file size (20MB limit for inline data)
	const geminiMaxInlineSize = 20 * 1024 * 1024
	if len(audioData) > geminiMaxInlineSize {
		return "", fmt.Errorf("audio file too large for Gemini inline upload (max 20MB, got %dMB)", len(audioData)/(1024*1024))
	}

	// Determine MIME type from extension
	ext := filepath.Ext(audioPath)
	mimeType := getMimeType(ext)
	if mimeType == "" {
		return "", fmt.Errorf("unsupported audio format: %s (supported: wav, mp3, aiff, aac, ogg, flac, m4a, webm)", ext)
	}

	// Base64 encode the audio
	base64Audio := base64.StdEncoding.EncodeToString(audioData)

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: "Transcribe this audio file accurately. Output only the transcription text, no additional commentary or formatting."},
				{
					InlineData: &GeminiInlineData{
						MimeType: mimeType,
						Data:     base64Audio,
					},
				},
			},
		},
	}

	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		return "", err
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// transcribeWithGeminiWithSplitting handles large audio files by splitting them
func transcribeWithGeminiWithSplitting(audioPath, apiKey, model string) (string, error) {
	fileSize, err := getFileSize(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %w", err)
	}

	// Gemini inline limit is 20MB
	const geminiMaxInlineSize = 20 * 1024 * 1024

	if fileSize <= geminiMaxInlineSize {
		return transcribeWithGemini(audioPath, apiKey, model)
	}

	// File is too large, need to split
	fmt.Printf("Audio file is large (%dMB), splitting into chunks...\n", fileSize/(1024*1024))

	// Split into 5-minute chunks (should be well under 20MB for most formats)
	chunks, err := splitAudioFile(audioPath, 300)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	defer func() {
		// Clean up temp files
		if len(chunks) > 0 {
			os.RemoveAll(filepath.Dir(chunks[0]))
		}
	}()

	fmt.Printf("Split into %d chunk(s)\n", len(chunks))

	var allTranscripts []string
	for i, chunk := range chunks {
		fmt.Printf("Transcribing chunk %d/%d with Gemini...\n", i+1, len(chunks))
		transcript, err := transcribeWithGemini(chunk, apiKey, model)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk %d: %w", i+1, err)
		}
		allTranscripts = append(allTranscripts, transcript)

		// Add delay between chunks to avoid rate limits
		if i < len(chunks)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return strings.Join(allTranscripts, " "), nil
}

// processWithGemini processes transcript with Gemini API
func processWithGemini(transcript string, action *PostAction, apiKey, model string) (string, error) {
	basePrompt := "You are a helpful assistant that processes transcribed text according to user instructions.\n\nTranscript:\n%s\n\nPlease process this transcript according to the instructions above."
	fullPrompt := action.Prompt + "\n\n" + fmt.Sprintf(basePrompt, transcript)

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: fullPrompt},
			},
		},
	}

	// Use provided model or default
	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		return "", err
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// processWithGeminiChunked processes large transcripts with Gemini in chunks
func processWithGeminiChunked(transcript string, action *PostAction, apiKey, model string) (string, error) {
	if model == "" {
		model = "gemini-2.0-flash"
	}

	maxTokens := getModelContextLimit(model)
	promptTokens := len(action.Prompt) / avgCharsPerToken
	transcriptTokens := len(transcript) / avgCharsPerToken
	estimatedTokens := promptTokens + transcriptTokens

	// If transcript fits in context, process normally
	if estimatedTokens <= maxTokens {
		return processWithGemini(transcript, action, apiKey, model)
	}

	// Transcript is too long, need to chunk
	fmt.Printf("  ⚠ Transcript is large (~%d tokens), processing in chunks...\n", estimatedTokens)

	// Calculate chunk size (leaving room for prompt and overlap)
	maxChunkChars := (maxTokens - promptTokens - 1000) * avgCharsPerToken
	_ = 500 // Overlap chars - reserved for future use

	// Split into sentences first for better boundaries
	sentences := splitIntoSentences(transcript)

	// Group sentences into chunks
	var chunks []string
	var currentChunk strings.Builder
	var lastSentences []string // Keep track of last few sentences for overlap

	for _, sentence := range sentences {
		// Check if adding this sentence would exceed the limit
		if currentChunk.Len()+len(sentence) > maxChunkChars && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.String())

			// Start new chunk with overlap (last few sentences)
			currentChunk.Reset()
			for _, s := range lastSentences {
				currentChunk.WriteString(s)
				currentChunk.WriteString(" ")
			}
		}

		currentChunk.WriteString(sentence)
		currentChunk.WriteString(" ")

		// Update overlap buffer
		lastSentences = append(lastSentences, sentence)
		if len(lastSentences) > 3 {
			lastSentences = lastSentences[1:]
		}
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	fmt.Printf("  → Split into %d chunk(s) for processing\n", len(chunks))

	// Process each chunk
	var results []string
	for i, chunk := range chunks {
		fmt.Printf("  → Processing chunk %d/%d...\n", i+1, len(chunks))

		result, err := processWithGemini(chunk, action, apiKey, model)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}
		results = append(results, result)

		// Add delay between chunks
		if i < len(chunks)-1 {
			fmt.Printf("  ⏸ Waiting 2 seconds before next chunk...\n")
			time.Sleep(2 * time.Second)
		}
	}

	// Merge results
	fmt.Printf("  ✓ All chunks processed, merging results\n")
	return mergeChunkResultsWithGemini(results, action, apiKey, model)
}

// mergeChunkResultsWithGemini merges chunk results using Gemini
func mergeChunkResultsWithGemini(chunkResults []string, action *PostAction, apiKey, model string) (string, error) {
	if len(chunkResults) == 1 {
		return chunkResults[0], nil
	}

	combinedChunks := strings.Join(chunkResults, "\n\n--- CHUNK BOUNDARY ---\n\n")

	mergePrompt := fmt.Sprintf(`You are merging multiple partial results from the same analysis that was split into chunks.

Original task: %s

Below are %d separate results from processing different parts of a transcript. Your job is to merge them into a single, coherent, comprehensive result that:
1. Removes duplicate information
2. Consolidates related points
3. Maintains the structure and format requested in the original task
4. Preserves all unique insights and details
5. Creates a unified narrative without chunk boundaries

Chunk results to merge:
%s

Provide the final merged result:`, action.Name, len(chunkResults), combinedChunks)

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: mergePrompt},
			},
		},
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		// Fall back to simple concatenation
		fmt.Printf("  ⚠ Merge failed, falling back to simple concatenation: %v\n", err)
		return strings.Join(chunkResults, "\n\n---\n\n"), nil
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// Provider dispatcher functions with fallback support

// transcribeAudioWithProvider transcribes audio using the specified provider with optional fallback
func transcribeAudioWithProvider(audioPath, provider, openaiKey, geminiKey, geminiModel string, enableFallback bool) (string, error) {
	var primaryErr error
	var result string

	switch provider {
	case "gemini":
		if geminiKey == "" {
			return "", fmt.Errorf("Gemini API key required. Use -gemini-key or -set-gemini-key")
		}
		result, primaryErr = transcribeWithGeminiWithSplitting(audioPath, geminiKey, geminiModel)
	case "openai":
		fallthrough
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return "", fmt.Errorf("OpenAI API key required. Use -k or -set-key")
		}
		result, primaryErr = transcribeAudioWithSplitting(audioPath, openaiKey)
	}

	// If primary succeeded, return result
	if primaryErr == nil {
		return result, nil
	}

	// Try fallback if enabled and alternate key is available
	if enableFallback {
		altProvider := ""
		altKey := ""
		if provider == "gemini" && openaiKey != "" && openaiKey != "XXXX" {
			altProvider = "openai"
			altKey = openaiKey
		} else if provider != "gemini" && geminiKey != "" {
			altProvider = "gemini"
			altKey = geminiKey
		}

		if altProvider != "" {
			fmt.Printf("  ⚠ %s failed, trying fallback provider %s...\n", provider, altProvider)

			var fallbackErr error
			if altProvider == "gemini" {
				result, fallbackErr = transcribeWithGeminiWithSplitting(audioPath, altKey, geminiModel)
			} else {
				result, fallbackErr = transcribeAudioWithSplitting(audioPath, altKey)
			}

			if fallbackErr == nil {
				fmt.Printf("  ✓ Fallback to %s succeeded\n", altProvider)
				return result, nil
			}

			return "", fmt.Errorf("primary (%s): %v; fallback (%s): %v", provider, primaryErr, altProvider, fallbackErr)
		}
	}

	return "", primaryErr
}

// processWithProviderChunked processes transcript using the specified provider with optional fallback
func processWithProviderChunked(transcript string, action *PostAction, provider, openaiKey, geminiKey, geminiModel string, enableFallback bool) (string, error) {
	var primaryErr error
	var result string

	switch provider {
	case "gemini":
		if geminiKey == "" {
			return "", fmt.Errorf("Gemini API key required")
		}
		result, primaryErr = processWithGeminiChunked(transcript, action, geminiKey, geminiModel)
	case "openai":
		fallthrough
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return "", fmt.Errorf("OpenAI API key required")
		}
		result, primaryErr = processWithOpenAIChunked(transcript, action, openaiKey)
	}

	// If primary succeeded, return result
	if primaryErr == nil {
		return result, nil
	}

	// Try fallback if enabled and alternate key is available
	if enableFallback {
		altProvider := ""
		altKey := ""
		if provider == "gemini" && openaiKey != "" && openaiKey != "XXXX" {
			altProvider = "openai"
			altKey = openaiKey
		} else if provider != "gemini" && geminiKey != "" {
			altProvider = "gemini"
			altKey = geminiKey
		}

		if altProvider != "" {
			fmt.Printf("  ⚠ %s failed, trying fallback provider %s...\n", provider, altProvider)

			var fallbackErr error
			if altProvider == "gemini" {
				result, fallbackErr = processWithGeminiChunked(transcript, action, altKey, geminiModel)
			} else {
				result, fallbackErr = processWithOpenAIChunked(transcript, action, altKey)
			}

			if fallbackErr == nil {
				fmt.Printf("  ✓ Fallback to %s succeeded\n", altProvider)
				return result, nil
			}

			return "", fmt.Errorf("primary (%s): %v; fallback (%s): %v", provider, primaryErr, altProvider, fallbackErr)
		}
	}

	return "", primaryErr
}

// selectBestActionsWithProvider selects best actions using the specified provider
func selectBestActionsWithProvider(transcript, provider, openaiKey, geminiKey, geminiModel string) ([]string, error) {
	switch provider {
	case "gemini":
		if geminiKey == "" {
			return nil, fmt.Errorf("Gemini API key required for auto-selection")
		}
		return selectBestActionsWithGemini(transcript, geminiKey, geminiModel)
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return nil, fmt.Errorf("OpenAI API key required for auto-selection")
		}
		return selectBestActions(transcript, openaiKey)
	}
}

// selectBestActionsWithGemini uses Gemini to select best actions
func selectBestActionsWithGemini(transcript, apiKey, model string) ([]string, error) {
	var actionDescriptions []string
	for _, action := range postActions {
		actionDescriptions = append(actionDescriptions, fmt.Sprintf("- %s: %s", action.ID, action.Description))
	}

	prompt := fmt.Sprintf(`Analyze the following transcript and select the 2-3 most appropriate post-processing actions from the list below.

Available actions:
%s

Transcript preview (first 2000 chars):
%s

Based on the content, which actions would provide the most value? Reply ONLY with a comma-separated list of action IDs (e.g., "openai-meeting-summary,openai-action-items"). Do not include any explanation.`,
		strings.Join(actionDescriptions, "\n"),
		truncateString(transcript, 2000))

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: prompt},
			},
		},
	}

	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	// Parse the response
	response := strings.TrimSpace(resp.Candidates[0].Content.Parts[0].Text)
	selectedIDs := strings.Split(response, ",")

	// Validate each ID
	var validIDs []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		for _, action := range postActions {
			if action.ID == id {
				validIDs = append(validIDs, id)
				break
			}
		}
	}

	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid action IDs selected by AI: %s", response)
	}

	return validIDs, nil
}

func processWithOpenAI(transcript string, action *PostAction, apiKey string) (string, error) {
	basePrompt := "You are a helpful assistant that processes transcribed text according to user instructions.\n\nTranscript:\n%s\n\nPlease process this transcript according to the instructions above."

	fullPrompt := action.Prompt + "\n\n" + fmt.Sprintf(basePrompt, transcript)

	reqBody := ChatCompletionRequest{
		Model: action.Model,
		Messages: []Message{
			{
				Role:    "user",
				Content: fullPrompt,
			},
		},
		Temperature: action.Temperature,
		MaxTokens:   action.MaxTokens,
	}

	// Use the retry-enabled request helper (3 max retries)
	chatResp, err := makeOpenAIRequest(reqBody, apiKey, 3)
	if err != nil {
		return "", err
	}

	return chatResp.Choices[0].Message.Content, nil
}

func processWithOpenAIChunked(transcript string, action *PostAction, apiKey string) (string, error) {
	// Get model-specific context limit
	maxTokens := getModelContextLimit(action.Model)

	// Estimate transcript + prompt tokens
	promptTokens := len(action.Prompt) / avgCharsPerToken
	transcriptTokens := len(transcript) / avgCharsPerToken
	estimatedTokens := promptTokens + transcriptTokens

	// If transcript fits in context, process normally
	if estimatedTokens <= maxTokens {
		return processWithOpenAI(transcript, action, apiKey)
	}

	// Transcript is too long, need to chunk
	fmt.Printf("  ⚠ Transcript is large (~%d tokens), processing in chunks...\n", estimatedTokens)

	// Calculate chunk size (leaving room for prompt and overlap)
	maxTranscriptTokensPerChunk := maxTokens - promptTokens - 500 // 500 token buffer
	maxCharsPerChunk := maxTranscriptTokensPerChunk * avgCharsPerToken

	// Split transcript into sentences for better chunking
	sentences := splitIntoSentences(transcript)

	var chunks []string
	var currentChunk strings.Builder
	currentSize := 0

	for i, sentence := range sentences {
		sentenceLen := len(sentence)

		// If adding this sentence exceeds chunk size, start new chunk
		if currentSize > 0 && currentSize+sentenceLen > maxCharsPerChunk {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()

			// Add overlap from previous chunk
			overlapStart := max(0, i-3) // Include last few sentences for context
			for j := overlapStart; j < i; j++ {
				currentChunk.WriteString(sentences[j])
				currentChunk.WriteString(" ")
			}
			currentSize = currentChunk.Len()
		}

		currentChunk.WriteString(sentence)
		currentChunk.WriteString(" ")
		currentSize += sentenceLen + 1
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	fmt.Printf("  → Split into %d chunk(s) for processing\n", len(chunks))

	// Process each chunk with retry logic
	var results []string
	for i, chunk := range chunks {
		fmt.Printf("  → Processing chunk %d/%d...\n", i+1, len(chunks))

		// processWithOpenAI now has built-in retry logic
		result, err := processWithOpenAI(chunk, action, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}
		results = append(results, result)

		// Add a small delay between chunks to avoid hitting rate limits
		// Skip delay after the last chunk
		if i < len(chunks)-1 {
			delaySeconds := 2
			fmt.Printf("  ⏸ Waiting %d seconds before next chunk...\n", delaySeconds)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}
	}

	// Intelligently merge chunk results using AI
	fmt.Printf("  ✓ All chunks processed, merging results intelligently\n")
	merged, err := mergeChunkResults(results, action, apiKey)
	if err != nil {
		fmt.Printf("  ⚠ Merge failed, falling back to simple concatenation: %v\n", err)
		return strings.Join(results, "\n\n---\n\n"), nil
	}
	return merged, nil
}

func mergeChunkResults(chunkResults []string, action *PostAction, apiKey string) (string, error) {
	// If only 1 chunk, no merge needed
	if len(chunkResults) == 1 {
		return chunkResults[0], nil
	}

	// Combine all chunk results into a single text for merging
	combinedChunks := strings.Join(chunkResults, "\n\n--- CHUNK BOUNDARY ---\n\n")

	// Estimate tokens for merge prompt
	estimatedTokens := len(combinedChunks) / avgCharsPerToken
	maxTokens := getModelContextLimit(action.Model)

	// If merge would exceed limits, do hierarchical merge
	if estimatedTokens > maxTokens/2 { // Leave room for prompt + response
		fmt.Printf("  → Chunk results too large, using hierarchical merge\n")
		return hierarchicalMerge(chunkResults, action, apiKey)
	}

	// Create a merge prompt that understands the original action's intent
	mergePrompt := fmt.Sprintf(`You are merging multiple partial results from the same analysis that was split into chunks.

Original task: %s

Below are %d separate results from processing different parts of a transcript. Your job is to merge them into a single, coherent, comprehensive result that:
1. Removes duplicate information
2. Consolidates related points
3. Maintains the structure and format requested in the original task
4. Preserves all unique insights and details
5. Creates a unified narrative without chunk boundaries

Chunk results to merge:
%s

Provide the final merged result:`, action.Name, len(chunkResults), combinedChunks)

	// Use same model as the action for consistency
	reqBody := ChatCompletionRequest{
		Model: action.Model,
		Messages: []Message{
			{
				Role:    "user",
				Content: mergePrompt,
			},
		},
		Temperature: 0.3, // Lower temperature for consistency
		MaxTokens:   action.MaxTokens,
	}

	// Use the retry-enabled request helper (3 max retries)
	chatResp, err := makeOpenAIRequest(reqBody, apiKey, 3)
	if err != nil {
		return "", fmt.Errorf("merge request failed: %w", err)
	}

	return chatResp.Choices[0].Message.Content, nil
}

func hierarchicalMerge(chunkResults []string, action *PostAction, apiKey string) (string, error) {
	// Merge in pairs until we have a single result
	currentLevel := chunkResults

	for len(currentLevel) > 1 {
		var nextLevel []string
		fmt.Printf("  → Hierarchical merge: processing %d results\n", len(currentLevel))

		// Process in pairs
		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 < len(currentLevel) {
				// Merge pair
				pair := []string{currentLevel[i], currentLevel[i+1]}
				merged, err := mergeChunkResults(pair, action, apiKey)
				if err != nil {
					return "", fmt.Errorf("hierarchical merge failed at level: %w", err)
				}
				nextLevel = append(nextLevel, merged)
			} else {
				// Odd one out, pass through
				nextLevel = append(nextLevel, currentLevel[i])
			}
		}

		currentLevel = nextLevel
	}

	return currentLevel[0], nil
}

func selectBestActions(transcript string, apiKey string) ([]string, error) {
	// Build list of available actions for AI to choose from
	var actionDescriptions []string
	for _, action := range postActions {
		actionDescriptions = append(actionDescriptions, fmt.Sprintf("- %s: %s", action.ID, action.Description))
	}

	prompt := fmt.Sprintf(`Analyze the following transcript and select the 2-3 most appropriate post-processing actions from the list below.

Available actions:
%s

Transcript preview (first 2000 chars):
%s

Based on the content, which actions would provide the most value? Reply ONLY with a comma-separated list of action IDs (e.g., "openai-meeting-summary,openai-action-items"). Do not include any explanation.`,
		strings.Join(actionDescriptions, "\n"),
		truncateString(transcript, 2000))

	reqBody := ChatCompletionRequest{
		Model: "gpt-3.5-turbo",
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   100,
	}

	// Use the retry-enabled request helper (3 max retries)
	chatResp, err := makeOpenAIRequest(reqBody, apiKey, 3)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	// Parse the response (comma-separated action IDs)
	response := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	selectedIDs := strings.Split(response, ",")

	// Trim and validate each ID
	var validIDs []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		// Verify the action exists
		if findAction(id) != nil {
			validIDs = append(validIDs, id)
		}
	}

	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid actions selected by AI")
	}

	return validIDs, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func splitIntoSentences(text string) []string {
	// Simple sentence splitter (splits on . ! ? followed by space)
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence endings
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			// Check if followed by space or end of text
			if i+1 >= len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' {
				sentence := strings.TrimSpace(current.String())
				if len(sentence) > 0 {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	// Add any remaining text
	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if len(sentence) > 0 {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func transcribeAudio(audioPath, apiKey string) (string, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Open the audio file
		file, err := os.Open(audioPath)
		if err != nil {
			return "", fmt.Errorf("failed to open audio file: %w", err)
		}

		// Create a multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add the file to the form
		part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
		if err != nil {
			file.Close()
			return "", fmt.Errorf("failed to create form file: %w", err)
		}
		_, err = io.Copy(part, file)
		if err != nil {
			file.Close()
			return "", fmt.Errorf("failed to copy file: %w", err)
		}
		file.Close()

		// Add the model field
		err = writer.WriteField("model", "whisper-1")
		if err != nil {
			return "", fmt.Errorf("failed to write model field: %w", err)
		}

		// Close the writer
		err = writer.Close()
		if err != nil {
			return "", fmt.Errorf("failed to close writer: %w", err)
		}

		// Create the HTTP request
		req, err := http.NewRequest("POST", openAIBaseURL+"/audio/transcriptions", body)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		// Send the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return "", lastErr
		}
		defer resp.Body.Close()

		// Read the response
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		// Handle rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := parseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("API request failed with status 429: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return "", lastErr
		}

		// Check for other errors
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return "", lastErr
		}

		// Parse the response
		var transcriptionResp TranscriptionResponse
		err = json.Unmarshal(respBody, &transcriptionResp)
		if err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		return transcriptionResp.Text, nil
	}

	return "", fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func getFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func splitAudioFile(audioPath string, chunkDurationSeconds int) ([]string, error) {
	// Create temporary directory for chunks
	tmpDir, err := os.MkdirTemp("", "goscribe-chunks-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	baseName := filepath.Base(audioPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	outputPattern := filepath.Join(tmpDir, nameWithoutExt+"_chunk_%03d"+ext)

	// Use ffmpeg to split the file
	cmd := fmt.Sprintf("ffmpeg -i %s -f segment -segment_time %d -c copy -reset_timestamps 1 %s",
		shellescape(audioPath),
		chunkDurationSeconds,
		shellescape(outputPattern))

	output, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	// Find all generated chunk files
	chunks, err := filepath.Glob(filepath.Join(tmpDir, nameWithoutExt+"_chunk_*"+ext))
	if err != nil {
		return nil, fmt.Errorf("failed to find chunk files: %w", err)
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks were created")
	}

	return chunks, nil
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func transcribeAudioWithSplitting(audioPath, apiKey string) (string, error) {
	// Check file size
	fileSize, err := getFileSize(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %w", err)
	}

	fileSizeMB := float64(fileSize) / (1024 * 1024)

	// If file is under the limit, transcribe normally
	if fileSize <= maxFileSizeBytes {
		return transcribeAudio(audioPath, apiKey)
	}

	// File is too large, need to split
	fmt.Printf("⚠ File size (%.1f MB) exceeds OpenAI limit (25 MB)\n", fileSizeMB)
	fmt.Println("Splitting audio file into chunks...")

	// Split into 10-minute chunks (600 seconds)
	// This ensures each chunk stays well under 25MB for most audio formats
	chunkDurationSeconds := 600

	chunks, err := splitAudioFile(audioPath, chunkDurationSeconds)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	defer func() {
		// Clean up chunks
		for _, chunk := range chunks {
			os.Remove(chunk)
		}
		os.Remove(filepath.Dir(chunks[0]))
	}()

	fmt.Printf("✓ Created %d chunks\n", len(chunks))

	// Transcribe each chunk
	var allTranscripts []string
	for i, chunk := range chunks {
		fmt.Printf("\n[%d/%d] Transcribing chunk %s...\n", i+1, len(chunks), filepath.Base(chunk))

		// Check chunk size
		chunkSize, _ := getFileSize(chunk)
		if chunkSize > maxFileSizeBytes {
			return "", fmt.Errorf("chunk %d is still too large (%.1f MB) - try a shorter chunk duration",
				i+1, float64(chunkSize)/(1024*1024))
		}

		transcript, err := transcribeAudio(chunk, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk %d: %w", i+1, err)
		}

		allTranscripts = append(allTranscripts, transcript)
		fmt.Printf("✓ Chunk %d/%d complete\n", i+1, len(chunks))
	}

	// Merge all transcripts
	fmt.Println("\n✓ All chunks transcribed successfully")
	return strings.Join(allTranscripts, " "), nil
}
