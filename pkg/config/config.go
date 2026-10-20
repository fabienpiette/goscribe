package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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
	Provider     string       `yaml:"provider"`
	OpenAIAPIKey string       `yaml:"openai_api_key"`
	GeminiAPIKey string       `yaml:"gemini_api_key"`
	GeminiModel  string       `yaml:"gemini_model"`
	PostActions  []PostAction `yaml:"post_actions"`

	// Folder whose tag files (and its subfolders') seed the tag vocabulary.
	// Default: the parent of the transcript's folder.
	TagsRoot string `yaml:"tags_root,omitempty"`

	// Transcription engine settings. Empty transcriber means "same as provider".
	Transcriber      string `yaml:"transcriber,omitempty"`
	ElevenLabsAPIKey string `yaml:"elevenlabs_api_key,omitempty"`
	ElevenLabsModel  string `yaml:"elevenlabs_model,omitempty"`
	// Terms (names, products, jargon) ElevenLabs should favor. 20% surcharge when set.
	ElevenLabsKeyterms []string `yaml:"elevenlabs_keyterms,omitempty"`
	// Upper bound on speakers for diarization (0 = auto).
	ElevenLabsNumSpeakers int `yaml:"elevenlabs_num_speakers,omitempty"`
	// Keep filler words and false starts (default: removed).
	ElevenLabsVerbatim bool   `yaml:"elevenlabs_verbatim,omitempty"`
	WhisperModel       string `yaml:"whisper_model,omitempty"`
	// Models for the claude and codex CLIs (empty: the CLI's default model).
	ClaudeModel string `yaml:"claude_model,omitempty"`
	CodexModel  string `yaml:"codex_model,omitempty"`
	HermesModel string `yaml:"hermes_model,omitempty"`
	// How far back the recap key (w) of goscribe assist goes (default 5m).
	AssistRecap time.Duration `yaml:"assist_recap,omitempty"`
	Language    string        `yaml:"language,omitempty"`

	// Read-only lookups ("goscribe assist" asking a Claude Code session).
	Librarian Librarian `yaml:"librarian,omitempty"`
}

// HarnessModels maps the claude and codex harnesses to their configured model.
func (c *Config) HarnessModels() map[string]string {
	models := map[string]string{}
	if c.ClaudeModel != "" {
		models["claude"] = c.ClaudeModel
	}
	if c.CodexModel != "" {
		models["codex"] = c.CodexModel
	}
	if c.HermesModel != "" {
		models["hermes"] = c.HermesModel
	}
	return models
}

// ValidProviders lists the engines that can run post-processing actions.
// claude and codex run through the local Claude Code / Codex CLIs.
var ValidProviders = []string{"openai", "gemini", "claude", "codex", "hermes"}

func IsValidProvider(name string) bool {
	for _, p := range ValidProviders {
		if p == name {
			return true
		}
	}
	return false
}

// ValidTranscribers lists the engines that can turn audio into text.
var ValidTranscribers = []string{"local", "elevenlabs", "openai", "gemini"}

func IsValidTranscriber(name string) bool {
	for _, t := range ValidTranscribers {
		if t == name {
			return true
		}
	}
	return false
}

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	if err := ValidateConfig(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	fmt.Printf("Loaded %d action(s) from config file\n", len(config.PostActions))

	return &config, nil
}

func ValidateConfig(config *Config) error {
	if config.Provider != "" && !IsValidProvider(config.Provider) {
		return fmt.Errorf("invalid provider '%s' (valid: %s)", config.Provider, strings.Join(ValidProviders, ", "))
	}

	if config.Transcriber != "" && !IsValidTranscriber(config.Transcriber) {
		return fmt.Errorf("invalid transcriber '%s' (valid: %s)", config.Transcriber, strings.Join(ValidTranscribers, ", "))
	}

	if len(config.PostActions) == 0 {
		return fmt.Errorf("no post-processing actions defined in config")
	}

	seenIDs := make(map[string]bool)

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

		if seenIDs[action.ID] {
			return fmt.Errorf("duplicate action ID '%s' found", action.ID)
		}
		seenIDs[action.ID] = true

		if !validTypes[action.Type] {
			return fmt.Errorf("action '%s' has invalid type '%s' (valid: openai, gemini)", action.ID, action.Type)
		}

		if action.Temperature < 0 || action.Temperature > 2 {
			return fmt.Errorf("action '%s' has invalid temperature %.2f (must be between 0 and 2)", action.ID, action.Temperature)
		}

		if action.MaxTokens <= 0 {
			return fmt.Errorf("action '%s' has invalid max_tokens %d (must be > 0)", action.ID, action.MaxTokens)
		}

		if action.Type == "openai" && !validOpenAIModels[action.Model] {
			fmt.Printf("Warning: action '%s' uses model '%s' which may not be a recognized OpenAI model\n", action.ID, action.Model)
		}
		if action.Type == "gemini" && !validGeminiModels[action.Model] {
			fmt.Printf("Warning: action '%s' uses model '%s' which may not be a recognized Gemini model\n", action.ID, action.Model)
		}
	}

	return nil
}

func configDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".goscribe"), nil
}

func configFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yml"), nil
}

func CreateDefault() error {
	dir, err := configDir()
	if err != nil {
		return err
	}

	configFile := filepath.Join(dir, "config.yml")

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	defaultConfig := getDefaultConfigContent()

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

func Reset() error {
	configFile, err := configFilePath()
	if err != nil {
		return err
	}

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

	err = CreateDefault()
	if err != nil {
		return err
	}

	fmt.Println("✓ Config file reset to defaults")
	return nil
}

func StoreAPIKey(apiKey string) error {
	return updateConfigField(func(config *Config) {
		config.OpenAIAPIKey = apiKey
	}, func(configFile string) {
		fmt.Printf("✓ API key stored successfully in: %s\n", configFile)
		fmt.Println("\nYou can now use goscribe without the -k flag:")
		fmt.Println("  goscribe audio.mp3")
		fmt.Println("  goscribe -action openai-meeting-summary meeting.mp3")
	})
}

func StoreGeminiAPIKey(apiKey string) error {
	return updateConfigField(func(config *Config) {
		config.GeminiAPIKey = apiKey
	}, func(configFile string) {
		fmt.Printf("✓ Gemini API key stored successfully in: %s\n", configFile)
		fmt.Println("\nYou can now use goscribe with Gemini:")
		fmt.Println("  goscribe -provider gemini audio.mp3")
		fmt.Println("  goscribe -provider gemini -action openai-meeting-summary meeting.mp3")
	})
}

func SetDefaultProvider(providerName string) error {
	if !IsValidProvider(providerName) {
		return fmt.Errorf("invalid provider '%s' (valid: %s)", providerName, strings.Join(ValidProviders, ", "))
	}

	return updateConfigField(func(config *Config) {
		config.Provider = providerName
	}, func(configFile string) {
		fmt.Printf("✓ Default provider set to '%s' in: %s\n", providerName, configFile)
	})
}

func SetDefaultTranscriber(name string) error {
	if !IsValidTranscriber(name) {
		return fmt.Errorf("invalid transcriber '%s' (valid: %s)", name, strings.Join(ValidTranscribers, ", "))
	}

	return updateConfigField(func(config *Config) {
		config.Transcriber = name
	}, func(configFile string) {
		fmt.Printf("✓ Default transcriber set to '%s' in: %s\n", name, configFile)
	})
}

// readQuiet unmarshals the config file (default ~/.goscribe/config.yml) into out,
// without validating or printing. It reports whether it succeeded.
func readQuiet(path string, out any) bool {
	if path == "" {
		p, err := configFilePath()
		if err != nil {
			return false
		}
		path = p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return yaml.Unmarshal(data, out) == nil
}

// ReadTagsRoot returns tags_root from the config file, or "" if it is unset or
// unreadable. Unlike LoadConfig it neither validates nor prints, so commands
// like search keep a clean stdout.
func ReadTagsRoot(path string) string {
	var cfg struct {
		TagsRoot string `yaml:"tags_root"`
	}
	readQuiet(path, &cfg)
	return cfg.TagsRoot
}

// RecordDefaults are the config defaults for "goscribe record".
type RecordDefaults struct {
	Source  string `yaml:"record_source"`
	Mic     string `yaml:"record_mic"`
	System  string `yaml:"record_system"`
	Quality string `yaml:"record_quality"`
}

// ReadRecordDefaults reads the record_* keys quietly; zero value on any problem.
func ReadRecordDefaults(path string) RecordDefaults {
	var d RecordDefaults
	if !readQuiet(path, &d) {
		return RecordDefaults{}
	}
	return d
}

func FindAction(actions []PostAction, id string) *PostAction {
	for i := range actions {
		if actions[i].ID == id {
			return &actions[i]
		}
	}
	return nil
}

func updateConfigField(mutate func(*Config), onSuccess func(string)) error {
	configFile, err := configFilePath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Println("Config file not found. Creating default config...")
		if err := CreateDefault(); err != nil {
			return fmt.Errorf("failed to create default config: %w", err)
		}
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	mutate(&config)

	updatedData, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	err = os.WriteFile(configFile, updatedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	onSuccess(configFile)
	return nil
}

func DefaultPostActions() []PostAction {
	var cfg Config
	if err := yaml.Unmarshal([]byte(getDefaultConfigContent()), &cfg); err != nil {
		panic("DefaultPostActions: malformed embedded YAML: " + err.Error())
	}
	return cfg.PostActions
}
