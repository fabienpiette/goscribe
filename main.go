package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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
	OpenAIAPIKey string       `yaml:"openai_api_key"`
	PostActions  []PostAction `yaml:"post_actions"`
}

var postActions = []PostAction{}

func main() {
	// Define command-line flags
	apiKey := flag.String("k", "XXXX", "OpenAI API key")
	output := flag.String("o", "", "Output file name (default: same as audio file with .txt extension)")
	listActions := flag.Bool("list-actions", false, "List available post-processing actions")
	postAction := flag.String("action", "", "Post-processing action ID (use -list-actions to see options)")
	configFile := flag.String("config", "", "Path to YAML config file with custom post-actions (default: ~/.goscribe/config.yml)")
	initConfig := flag.Bool("init", false, "Reset config file to defaults (overwrites ~/.goscribe/config.yml)")
	setKey := flag.String("set-key", "", "Store OpenAI API key in config file")
	transcriptFile := flag.String("transcript", "", "Process existing transcript file (skips transcription)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "goscribe - AI-powered audio transcription with OpenAI Whisper\n\n")
		fmt.Fprintf(os.Stderr, "USAGE:\n")
		fmt.Fprintf(os.Stderr, "  goscribe [options] <audio_file>\n\n")
		fmt.Fprintf(os.Stderr, "OPTIONS:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEXAMPLES:\n")
		fmt.Fprintf(os.Stderr, "  # Basic transcription\n")
		fmt.Fprintf(os.Stderr, "  goscribe -k YOUR_API_KEY meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcribe with meeting summary\n")
		fmt.Fprintf(os.Stderr, "  goscribe -k YOUR_API_KEY -action openai-meeting-summary meeting.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Transcribe technical meeting\n")
		fmt.Fprintf(os.Stderr, "  goscribe -k YOUR_API_KEY -action openai-tech-meeting standup.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # Custom output file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -k YOUR_API_KEY -o transcript.txt audio.mp3\n\n")
		fmt.Fprintf(os.Stderr, "  # List all available post-processing actions\n")
		fmt.Fprintf(os.Stderr, "  goscribe -list-actions\n\n")
		fmt.Fprintf(os.Stderr, "  # Process existing transcript file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -transcript meeting-transcript.txt -action openai-meeting-summary\n\n")
		fmt.Fprintf(os.Stderr, "  # Store API key in config file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -set-key YOUR_API_KEY\n\n")
		fmt.Fprintf(os.Stderr, "  # Reset config to defaults\n")
		fmt.Fprintf(os.Stderr, "  goscribe -init\n\n")
		fmt.Fprintf(os.Stderr, "  # Use custom config file\n")
		fmt.Fprintf(os.Stderr, "  goscribe -config my-actions.yml -action custom-action audio.mp3\n\n")
		fmt.Fprintf(os.Stderr, "OUTPUT FILES:\n")
		fmt.Fprintf(os.Stderr, "  <filename>-transcript.txt              Raw transcription\n")
		fmt.Fprintf(os.Stderr, "  <filename>-<action-id>.txt             Post-processed output (if -action used)\n\n")
		fmt.Fprintf(os.Stderr, "CONFIGURATION:\n")
		fmt.Fprintf(os.Stderr, "  Config file: ~/.goscribe/config.yml\n")
		fmt.Fprintf(os.Stderr, "  - Store your OpenAI API key (openai_api_key field)\n")
		fmt.Fprintf(os.Stderr, "  - Customize or add your own post-processing actions\n\n")
		fmt.Fprintf(os.Stderr, "POPULAR ACTIONS:\n")
		fmt.Fprintf(os.Stderr, "  openai-meeting-summary      Comprehensive meeting summary\n")
		fmt.Fprintf(os.Stderr, "  openai-action-items         Extract action items and tasks\n")
		fmt.Fprintf(os.Stderr, "  openai-tech-meeting         Technical meeting summary\n")
		fmt.Fprintf(os.Stderr, "  openai-one-on-one           1:1 meeting notes\n")
		fmt.Fprintf(os.Stderr, "  openai-executive-brief      Executive summary\n\n")
		fmt.Fprintf(os.Stderr, "For more information, visit: https://github.com/yourusername/goscribe\n")
	}

	flag.Parse()

	// Store API key if requested
	if *setKey != "" {
		err := storeAPIKey(*setKey)
		if err != nil {
			fmt.Printf("Error storing API key: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Reset config if requested
	if *initConfig {
		err := resetConfig()
		if err != nil {
			fmt.Printf("Error resetting config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Determine which config file to use
	configPath := *configFile
	if configPath == "" {
		// Get default config location
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fmt.Printf("Error getting home directory: %v\n", err)
			os.Exit(1)
		}
		configPath = filepath.Join(homeDir, ".goscribe", "config.yml")

		// Create default config if it doesn't exist
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("Config file not found. Creating default config...")
			if err := createDefaultConfig(); err != nil {
				fmt.Printf("Error creating default config: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// Always load config file (required for actions)
	configAPIKey, err := loadConfigActions(configPath)
	if err != nil {
		fmt.Printf("Error loading config file: %v\n", err)
		os.Exit(1)
	}

	// Use config API key if command-line key is default
	if *apiKey == "XXXX" && configAPIKey != "" {
		*apiKey = configAPIKey
		fmt.Println("Using API key from config file")
	}

	// List actions and exit if requested
	if *listActions {
		fmt.Println("Available post-processing actions:")
		fmt.Println()
		for _, action := range postActions {
			fmt.Printf("ID: %s\n", action.ID)
			fmt.Printf("Name: %s\n", action.Name)
			fmt.Printf("Description: %s\n", action.Description)
			fmt.Printf("Model: %s\n", action.Model)
			fmt.Println(strings.Repeat("-", 70))
		}
		return
	}

	var transcription string
	var audioPath string
	var transcriptFilename string

	// Handle transcript file mode
	if *transcriptFile != "" {
		// Process existing transcript file
		if *postAction == "" {
			fmt.Println("Error: -action is required when using -transcript")
			os.Exit(1)
		}

		// Check if transcript file exists
		if _, err := os.Stat(*transcriptFile); os.IsNotExist(err) {
			fmt.Printf("Error: Transcript file '%s' not found.\n", *transcriptFile)
			os.Exit(1)
		}

		// Read the transcript file
		data, err := os.ReadFile(*transcriptFile)
		if err != nil {
			fmt.Printf("Error reading transcript file: %v\n", err)
			os.Exit(1)
		}
		transcription = string(data)
		transcriptFilename = *transcriptFile
		fmt.Printf("Loaded transcript from %s\n", transcriptFilename)
	} else {
		// Standard audio transcription mode
		// Get the audio file path from remaining arguments
		if flag.NArg() < 1 {
			fmt.Println("Error: Audio file path is required")
			fmt.Println("Usage: goscribe [options] <audio_path>")
			fmt.Println("   or: goscribe -transcript <transcript_file> -action <action_id>")
			flag.PrintDefaults()
			os.Exit(1)
		}
		audioPath = flag.Arg(0)

		// Check if audio file exists
		if _, err := os.Stat(audioPath); os.IsNotExist(err) {
			fmt.Printf("Error: Audio file '%s' not found.\n", audioPath)
			os.Exit(1)
		}

		// Generate output filename if not provided
		outputFilename := *output

		if outputFilename == "" {
			ext := filepath.Ext(audioPath)
			baseName := strings.TrimSuffix(audioPath, ext)
			transcriptFilename = baseName + "-transcript.txt"
		} else {
			// If user provides custom output, use it for transcript
			transcriptFilename = outputFilename
		}

		// Transcribe the audio file
		fmt.Println("Transcribing audio...")
		var err error
		transcription, err = transcribeAudio(audioPath, *apiKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		// Always save the raw transcript
		err = os.WriteFile(transcriptFilename, []byte(transcription), 0644)
		if err != nil {
			fmt.Printf("Error writing transcript file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Raw transcript saved to %s\n", transcriptFilename)
	}

	// Apply post-processing action if specified
	if *postAction != "" {
		action := findAction(*postAction)
		if action == nil {
			fmt.Printf("Error: Unknown action '%s'. Use -list-actions to see available options.\n", *postAction)
			os.Exit(1)
		}

		fmt.Printf("Applying post-processing: %s...\n", action.Name)
		processed, err := processWithOpenAI(transcription, action, *apiKey)
		if err != nil {
			fmt.Printf("Warning: Post-processing failed: %v\n", err)
			if *transcriptFile == "" {
				fmt.Println("Only raw transcript was saved.")
			}
		} else {
			// Generate filename for post-processed output
			var processedFilename string
			if *transcriptFile != "" {
				// For transcript mode, use the transcript filename as base
				ext := filepath.Ext(*transcriptFile)
				baseName := strings.TrimSuffix(*transcriptFile, ext)
				processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
			} else {
				// For audio mode, use the audio filename as base
				ext := filepath.Ext(audioPath)
				baseName := strings.TrimSuffix(audioPath, ext)
				processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
			}

			err = os.WriteFile(processedFilename, []byte(processed), 0644)
			if err != nil {
				fmt.Printf("Error writing processed file: %v\n", err)
			} else {
				fmt.Printf("Post-processed output saved to %s\n", processedFilename)
				fmt.Println("Post-processing completed successfully!")
			}
		}
	}

	// Print confirmation summary
	fmt.Println(strings.Repeat("=", 70))
	fmt.Printf("Summary:\n")
	if *transcriptFile != "" {
		fmt.Printf("  Transcript: %s\n", transcriptFilename)
	} else {
		fmt.Printf("  Audio file: %s\n", audioPath)
		fmt.Printf("  Transcript: %s\n", transcriptFilename)
	}
	if *postAction != "" {
		action := findAction(*postAction)
		if action != nil {
			var processedFilename string
			if *transcriptFile != "" {
				ext := filepath.Ext(*transcriptFile)
				baseName := strings.TrimSuffix(*transcriptFile, ext)
				processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
			} else {
				ext := filepath.Ext(audioPath)
				baseName := strings.TrimSuffix(audioPath, ext)
				processedFilename = fmt.Sprintf("%s-%s.txt", baseName, action.ID)
			}
			fmt.Printf("  Processed:  %s (%s)\n", processedFilename, action.Name)
		}
	}
	if *apiKey != "XXXX" {
		fmt.Printf("  API key:    %s\n", *apiKey)
	}
	fmt.Println(strings.Repeat("=", 70))

	// Print the transcript text to console (first 500 chars for transcript mode)
	if *transcriptFile != "" {
		fmt.Printf("\nTranscript preview (first 500 chars):\n")
		fmt.Println(strings.Repeat("-", 70))
		if len(transcription) > 500 {
			fmt.Println(transcription[:500] + "...")
		} else {
			fmt.Println(transcription)
		}
	} else {
		fmt.Printf("\nRaw transcript:\n")
		fmt.Println(strings.Repeat("-", 70))
		fmt.Println(transcription)
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

func loadConfigActions(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return "", fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return "", fmt.Errorf("config validation failed: %w", err)
	}

	// Load actions from config file
	postActions = config.PostActions
	fmt.Printf("Loaded %d action(s) from config file\n", len(config.PostActions))

	return config.OpenAIAPIKey, nil
}

func validateConfig(config *Config) error {
	if len(config.PostActions) == 0 {
		return fmt.Errorf("no post-processing actions defined in config")
	}

	// Track unique IDs
	seenIDs := make(map[string]bool)

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
		validTypes := map[string]bool{
			"openai": true,
		}
		if !validTypes[action.Type] {
			return fmt.Errorf("action '%s' has invalid type '%s' (valid: openai)", action.ID, action.Type)
		}

		// Validate temperature range
		if action.Temperature < 0 || action.Temperature > 2 {
			return fmt.Errorf("action '%s' has invalid temperature %.2f (must be between 0 and 2)", action.ID, action.Temperature)
		}

		// Validate max_tokens
		if action.MaxTokens <= 0 {
			return fmt.Errorf("action '%s' has invalid max_tokens %d (must be > 0)", action.ID, action.MaxTokens)
		}

		// Validate model names (basic check for OpenAI models)
		validModels := map[string]bool{
			"gpt-3.5-turbo": true,
			"gpt-4":         true,
			"gpt-4-turbo":   true,
			"gpt-4o":        true,
			"gpt-4o-mini":   true,
		}
		if action.Type == "openai" && !validModels[action.Model] {
			fmt.Printf("Warning: action '%s' uses model '%s' which may not be valid\n", action.ID, action.Model)
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
		fmt.Scanln(&response)
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

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatCompletionResponse
	err = json.Unmarshal(respBody, &chatResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	return chatResp.Choices[0].Message.Content, nil
}

func transcribeAudio(audioPath, apiKey string) (string, error) {
	// Open the audio file
	file, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	// Create a multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the file to the form
	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

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
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", body)
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
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read the response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the response
	var transcriptionResp TranscriptionResponse
	err = json.Unmarshal(respBody, &transcriptionResp)
	if err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return transcriptionResp.Text, nil
}
