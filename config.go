package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

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
