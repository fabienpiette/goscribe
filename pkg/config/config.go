package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if config.Provider != "" && config.Provider != "openai" && config.Provider != "gemini" {
		return fmt.Errorf("invalid provider '%s' (valid: openai, gemini)", config.Provider)
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
	if providerName != "openai" && providerName != "gemini" {
		return fmt.Errorf("invalid provider '%s' (valid: openai, gemini)", providerName)
	}

	return updateConfigField(func(config *Config) {
		config.Provider = providerName
	}, func(configFile string) {
		fmt.Printf("✓ Default provider set to '%s' in: %s\n", providerName, configFile)
	})
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
