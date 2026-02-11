package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test findAction function
func TestFindAction(t *testing.T) {
	// Setup test actions
	postActions = []PostAction{
		{ID: "test-action-1", Name: "Test Action 1"},
		{ID: "test-action-2", Name: "Test Action 2"},
		{ID: "test-action-3", Name: "Test Action 3"},
	}

	tests := []struct {
		name     string
		actionID string
		want     *PostAction
	}{
		{
			name:     "Find existing action",
			actionID: "test-action-2",
			want:     &postActions[1],
		},
		{
			name:     "Action not found",
			actionID: "non-existent",
			want:     nil,
		},
		{
			name:     "Empty action ID",
			actionID: "",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findAction(tt.actionID)
			if got != tt.want {
				if got == nil && tt.want != nil {
					t.Errorf("findAction() = nil, want %v", tt.want)
				} else if got != nil && tt.want == nil {
					t.Errorf("findAction() = %v, want nil", got)
				} else if got != nil && tt.want != nil && got.ID != tt.want.ID {
					t.Errorf("findAction() ID = %v, want %v", got.ID, tt.want.ID)
				}
			}
		})
	}
}

// Test validateConfig function
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid config",
			config: &Config{
				OpenAIAPIKey: "test-key",
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Empty config",
			config: &Config{
				PostActions: []PostAction{},
			},
			wantErr: true,
		},
		{
			name: "Missing ID",
			config: &Config{
				PostActions: []PostAction{
					{
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing name",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing type",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing prompt",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Missing model",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Duplicate IDs",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action 1",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
					{
						ID:          "test-action",
						Name:        "Test Action 2",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid type",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "invalid-type",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Temperature too low",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: -0.1,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Temperature too high",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 2.1,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid max tokens",
			config: &Config{
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   0,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test loadConfigActions function
func TestLoadConfigActions(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yml")

	validConfig := `openai_api_key: "test-api-key"

post_actions:
  - id: "test-action"
    name: "Test Action"
    description: "Test description"
    type: "openai"
    prompt: "Test prompt"
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1000
`

	err := os.WriteFile(configPath, []byte(validConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("loadConfigActions() error = %v, want nil", err)
	}

	if config.OpenAIAPIKey != "test-api-key" {
		t.Errorf("loadConfigActions() apiKey = %v, want test-api-key", config.OpenAIAPIKey)
	}

	if len(postActions) != 1 {
		t.Errorf("loadConfigActions() loaded %d actions, want 1", len(postActions))
	}

	if len(postActions) > 0 {
		action := postActions[0]
		if action.ID != "test-action" {
			t.Errorf("Action ID = %v, want test-action", action.ID)
		}
		if action.Name != "Test Action" {
			t.Errorf("Action Name = %v, want Test Action", action.Name)
		}
		if action.Type != "openai" {
			t.Errorf("Action Type = %v, want openai", action.Type)
		}
		if action.Model != "gpt-3.5-turbo" {
			t.Errorf("Action Model = %v, want gpt-3.5-turbo", action.Model)
		}
		if action.Temperature != 0.3 {
			t.Errorf("Action Temperature = %v, want 0.3", action.Temperature)
		}
		if action.MaxTokens != 1000 {
			t.Errorf("Action MaxTokens = %v, want 1000", action.MaxTokens)
		}
	}
}

// Test loadConfigActions with invalid config
func TestLoadConfigActionsInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid-config.yml")

	invalidConfig := `openai_api_key: "test-api-key"

post_actions:
  - id: "test-action"
    name: "Test Action"
    type: "openai"
    # Missing prompt field
    model: "gpt-3.5-turbo"
    temperature: 0.3
    max_tokens: 1000
`

	err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = loadConfigActions(configPath)
	if err == nil {
		t.Error("loadConfigActions() expected error for invalid config, got nil")
	}
}

// Test loadConfigActions with non-existent file
func TestLoadConfigActionsNonExistent(t *testing.T) {
	_, err := loadConfigActions("/non/existent/path/config.yml")
	if err == nil {
		t.Error("loadConfigActions() expected error for non-existent file, got nil")
	}
}

// Test storeAPIKey function
func TestStoreAPIKey(t *testing.T) {
	// Save original HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Create temporary home directory
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	// First, create a default config
	err := createDefaultConfig()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	// Store API key
	testKey := "sk-test-api-key-12345"
	err = storeAPIKey(testKey)
	if err != nil {
		t.Errorf("storeAPIKey() error = %v, want nil", err)
	}

	// Verify the key was stored
	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	config, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("Failed to load config after storing key: %v", err)
	}

	if config.OpenAIAPIKey != testKey {
		t.Errorf("Stored API key = %v, want %v", config.OpenAIAPIKey, testKey)
	}
}

// Test createDefaultConfig function
func TestCreateDefaultConfig(t *testing.T) {
	// Save original HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Create temporary home directory
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	err := createDefaultConfig()
	if err != nil {
		t.Errorf("createDefaultConfig() error = %v, want nil", err)
	}

	// Verify config file was created
	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("createDefaultConfig() did not create config file")
	}

	// Verify config is valid and loads correctly
	config, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("Failed to load default config: %v", err)
	}

	// Default config should have empty API key
	if config.OpenAIAPIKey != "" {
		t.Errorf("Default config API key = %v, want empty string", config.OpenAIAPIKey)
	}

	// Should have multiple actions
	if len(postActions) == 0 {
		t.Error("Default config has no actions")
	}
}

// Test getDefaultConfigContent function
func TestGetDefaultConfigContent(t *testing.T) {
	content := getDefaultConfigContent()

	if content == "" {
		t.Error("getDefaultConfigContent() returned empty string")
	}

	// Check for expected content
	expectedStrings := []string{
		"openai_api_key:",
		"post_actions:",
		"openai-meeting-summary",
		"openai-action-items",
	}

	for _, expected := range expectedStrings {
		if !contains(content, expected) {
			t.Errorf("getDefaultConfigContent() missing expected string: %s", expected)
		}
	}
}

// Test parsing multiple action IDs
func TestMultipleActions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Single action",
			input:    "action1",
			expected: []string{"action1"},
		},
		{
			name:     "Two actions",
			input:    "action1,action2",
			expected: []string{"action1", "action2"},
		},
		{
			name:     "Three actions",
			input:    "action1,action2,action3",
			expected: []string{"action1", "action2", "action3"},
		},
		{
			name:     "Actions with spaces",
			input:    "action1, action2, action3",
			expected: []string{"action1", "action2", "action3"},
		},
		{
			name:     "Actions with extra spaces",
			input:    "action1 , action2 , action3",
			expected: []string{"action1", "action2", "action3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Split and trim like in main
			actionIDs := strings.Split(tt.input, ",")
			for i, id := range actionIDs {
				actionIDs[i] = strings.TrimSpace(id)
			}

			if len(actionIDs) != len(tt.expected) {
				t.Errorf("got %d actions, want %d", len(actionIDs), len(tt.expected))
			}

			for i, id := range actionIDs {
				if i >= len(tt.expected) {
					break
				}
				if id != tt.expected[i] {
					t.Errorf("action[%d] = %v, want %v", i, id, tt.expected[i])
				}
			}
		})
	}
}

// Test getFileSize function
func TestGetFileSize(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		fileSize int64
		wantErr  bool
	}{
		{
			name:     "Empty file",
			fileSize: 0,
			wantErr:  false,
		},
		{
			name:     "Small file (1KB)",
			fileSize: 1024,
			wantErr:  false,
		},
		{
			name:     "Medium file (1MB)",
			fileSize: 1024 * 1024,
			wantErr:  false,
		},
		{
			name:     "Large file (26MB)",
			fileSize: 26 * 1024 * 1024,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file with specific size
			testFile := filepath.Join(tmpDir, "test.dat")
			f, err := os.Create(testFile)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Write data to reach desired size
			if tt.fileSize > 0 {
				if err := f.Truncate(tt.fileSize); err != nil {
					t.Fatalf("Failed to truncate file: %v", err)
				}
			}
			f.Close()

			// Test getFileSize
			size, err := getFileSize(testFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("getFileSize() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err == nil && size != tt.fileSize {
				t.Errorf("getFileSize() = %d, want %d", size, tt.fileSize)
			}

			// Clean up for next iteration
			os.Remove(testFile)
		})
	}
}

// Test getFileSize with non-existent file
func TestGetFileSizeNonExistent(t *testing.T) {
	_, err := getFileSize("/non/existent/path/file.mp3")
	if err == nil {
		t.Error("getFileSize() expected error for non-existent file, got nil")
	}
}

// Test shellescape function
func TestShellescape(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple string",
			input:    "hello",
			expected: "'hello'",
		},
		{
			name:     "String with space",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "String with single quote",
			input:    "it's",
			expected: "'it'\\''s'",
		},
		{
			name:     "String with special chars",
			input:    "test$file",
			expected: "'test$file'",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellescape(tt.input)
			if got != tt.expected {
				t.Errorf("shellescape(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// Test getMimeType function
func TestGetMimeType(t *testing.T) {
	tests := []struct {
		name     string
		ext      string
		expected string
	}{
		{
			name:     "MP3 file",
			ext:      ".mp3",
			expected: "audio/mp3",
		},
		{
			name:     "WAV file",
			ext:      ".wav",
			expected: "audio/wav",
		},
		{
			name:     "M4A file",
			ext:      ".m4a",
			expected: "audio/mp4",
		},
		{
			name:     "OGG file",
			ext:      ".ogg",
			expected: "audio/ogg",
		},
		{
			name:     "FLAC file",
			ext:      ".flac",
			expected: "audio/flac",
		},
		{
			name:     "AAC file",
			ext:      ".aac",
			expected: "audio/aac",
		},
		{
			name:     "AIFF file",
			ext:      ".aiff",
			expected: "audio/aiff",
		},
		{
			name:     "WebM file",
			ext:      ".webm",
			expected: "audio/webm",
		},
		{
			name:     "MPEG file",
			ext:      ".mpeg",
			expected: "audio/mpeg",
		},
		{
			name:     "Uppercase extension",
			ext:      ".MP3",
			expected: "audio/mp3",
		},
		{
			name:     "Unknown extension",
			ext:      ".xyz",
			expected: "",
		},
		{
			name:     "Empty extension",
			ext:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMimeType(tt.ext)
			if got != tt.expected {
				t.Errorf("getMimeType(%q) = %q, want %q", tt.ext, got, tt.expected)
			}
		})
	}
}

// Test getModelContextLimit function
func TestGetModelContextLimit(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected int
	}{
		// OpenAI models
		{
			name:     "GPT-4",
			model:    "gpt-4",
			expected: 6000,
		},
		{
			name:     "GPT-4-turbo",
			model:    "gpt-4-turbo",
			expected: 100000,
		},
		{
			name:     "GPT-4o",
			model:    "gpt-4o",
			expected: 100000,
		},
		{
			name:     "GPT-3.5-turbo",
			model:    "gpt-3.5-turbo",
			expected: 12000,
		},
		// Gemini models
		{
			name:     "Gemini 2.0 flash",
			model:    "gemini-2.0-flash",
			expected: 900000,
		},
		{
			name:     "Gemini 1.5 pro",
			model:    "gemini-1.5-pro",
			expected: 900000,
		},
		{
			name:     "Gemini 1.5 flash",
			model:    "gemini-1.5-flash",
			expected: 900000,
		},
		{
			name:     "Gemini 1.0 pro",
			model:    "gemini-1.0-pro",
			expected: 28000,
		},
		{
			name:     "Unknown Gemini model",
			model:    "gemini-unknown",
			expected: 28000,
		},
		{
			name:     "Unknown model",
			model:    "unknown-model",
			expected: 6000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getModelContextLimit(tt.model)
			if got != tt.expected {
				t.Errorf("getModelContextLimit(%q) = %d, want %d", tt.model, got, tt.expected)
			}
		})
	}
}

// Test validateConfig with Gemini provider
func TestValidateConfigGemini(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid Gemini config",
			config: &Config{
				Provider:     "gemini",
				GeminiAPIKey: "test-gemini-key",
				PostActions: []PostAction{
					{
						ID:          "gemini-test-action",
						Name:        "Gemini Test Action",
						Type:        "gemini",
						Prompt:      "Test prompt",
						Model:       "gemini-2.0-flash",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Invalid provider",
			config: &Config{
				Provider: "invalid-provider",
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Invalid action type",
			config: &Config{
				Provider: "gemini",
				PostActions: []PostAction{
					{
						ID:          "test-action",
						Name:        "Test Action",
						Type:        "invalid-type",
						Prompt:      "Test prompt",
						Model:       "gemini-2.0-flash",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Mixed OpenAI and Gemini actions",
			config: &Config{
				Provider: "openai",
				PostActions: []PostAction{
					{
						ID:          "openai-action",
						Name:        "OpenAI Action",
						Type:        "openai",
						Prompt:      "Test prompt",
						Model:       "gpt-3.5-turbo",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
					{
						ID:          "gemini-action",
						Name:        "Gemini Action",
						Type:        "gemini",
						Prompt:      "Test prompt",
						Model:       "gemini-2.0-flash",
						Temperature: 0.5,
						MaxTokens:   1000,
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test storeGeminiAPIKey function
func TestStoreGeminiAPIKey(t *testing.T) {
	// Save original HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Create temporary home directory
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	// First, create a default config
	err := createDefaultConfig()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	// Store Gemini API key
	testKey := "test-gemini-api-key-12345"
	err = storeGeminiAPIKey(testKey)
	if err != nil {
		t.Errorf("storeGeminiAPIKey() error = %v, want nil", err)
	}

	// Verify the key was stored
	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	config, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("Failed to load config after storing Gemini key: %v", err)
	}

	if config.GeminiAPIKey != testKey {
		t.Errorf("Stored Gemini API key = %v, want %v", config.GeminiAPIKey, testKey)
	}
}

// Test setDefaultProvider function
func TestSetDefaultProvider(t *testing.T) {
	// Save original HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Create temporary home directory
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	// First, create a default config
	err := createDefaultConfig()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{
			name:     "Set to gemini",
			provider: "gemini",
			wantErr:  false,
		},
		{
			name:     "Set to openai",
			provider: "openai",
			wantErr:  false,
		},
		{
			name:     "Invalid provider",
			provider: "invalid",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := setDefaultProvider(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("setDefaultProvider(%q) error = %v, wantErr %v", tt.provider, err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify the provider was set
				configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
				config, err := loadConfigActions(configPath)
				if err != nil {
					t.Errorf("Failed to load config after setting provider: %v", err)
				}

				if config.Provider != tt.provider {
					t.Errorf("Stored provider = %v, want %v", config.Provider, tt.provider)
				}
			}
		})
	}
}

// Test loadConfigActions with Gemini config
func TestLoadConfigActionsGemini(t *testing.T) {
	// Create a temporary config file with Gemini settings
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yml")

	geminiConfig := `provider: "gemini"
openai_api_key: "test-openai-key"
gemini_api_key: "test-gemini-key"
gemini_model: "gemini-1.5-pro"

post_actions:
  - id: "gemini-test-action"
    name: "Gemini Test Action"
    description: "Test description"
    type: "gemini"
    prompt: "Test prompt"
    model: "gemini-2.0-flash"
    temperature: 0.3
    max_tokens: 1000
`

	err := os.WriteFile(configPath, []byte(geminiConfig), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("loadConfigActions() error = %v, want nil", err)
	}

	// Verify Gemini-specific fields
	if config.Provider != "gemini" {
		t.Errorf("Provider = %v, want gemini", config.Provider)
	}
	if config.GeminiAPIKey != "test-gemini-key" {
		t.Errorf("GeminiAPIKey = %v, want test-gemini-key", config.GeminiAPIKey)
	}
	if config.GeminiModel != "gemini-1.5-pro" {
		t.Errorf("GeminiModel = %v, want gemini-1.5-pro", config.GeminiModel)
	}
	if config.OpenAIAPIKey != "test-openai-key" {
		t.Errorf("OpenAIAPIKey = %v, want test-openai-key", config.OpenAIAPIKey)
	}

	// Verify action was loaded
	if len(postActions) != 1 {
		t.Errorf("loadConfigActions() loaded %d actions, want 1", len(postActions))
	}

	if len(postActions) > 0 {
		action := postActions[0]
		if action.Type != "gemini" {
			t.Errorf("Action Type = %v, want gemini", action.Type)
		}
		if action.Model != "gemini-2.0-flash" {
			t.Errorf("Action Model = %v, want gemini-2.0-flash", action.Model)
		}
	}
}

// Test getDefaultConfigContent includes Gemini fields
func TestGetDefaultConfigContentGemini(t *testing.T) {
	content := getDefaultConfigContent()

	// Check for Gemini-specific content
	expectedStrings := []string{
		"provider:",
		"gemini_api_key:",
		"gemini_model:",
		"gemini-2.0-flash",
	}

	for _, expected := range expectedStrings {
		if !contains(content, expected) {
			t.Errorf("getDefaultConfigContent() missing expected Gemini field: %s", expected)
		}
	}
}

// Test parseRateLimitWaitTime function
func TestParseRateLimitWaitTime(t *testing.T) {
	tests := []struct {
		name        string
		errorBody   string
		expectedMin float64 // minimum expected seconds
		expectedMax float64 // maximum expected seconds
	}{
		{
			name:        "Standard rate limit message",
			errorBody:   "Rate limit exceeded. Please try again in 9.798s",
			expectedMin: 9.0,
			expectedMax: 10.0,
		},
		{
			name:        "Short wait time",
			errorBody:   "Please try again in 1.5s",
			expectedMin: 1.0,
			expectedMax: 2.0,
		},
		{
			name:        "No wait time in message",
			errorBody:   "Some other error message",
			expectedMin: 10.0, // Default fallback
			expectedMax: 10.0,
		},
		{
			name:        "Empty message",
			errorBody:   "",
			expectedMin: 10.0, // Default fallback
			expectedMax: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration := parseRateLimitWaitTime(tt.errorBody)
			seconds := duration.Seconds()

			if seconds < tt.expectedMin || seconds > tt.expectedMax {
				t.Errorf("parseRateLimitWaitTime(%q) = %.2f seconds, want between %.2f and %.2f",
					tt.errorBody, seconds, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Pure function tests ---

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"zero max len", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestSplitIntoSentences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single sentence", "Hello world.", []string{"Hello world."}},
		{"multiple sentences", "First. Second! Third?", []string{"First.", "Second!", "Third?"}},
		{"no punctuation", "just some text", []string{"just some text"}},
		{"trailing text after sentence", "Hello. world", []string{"Hello.", "world"}},
		{"empty string", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitIntoSentences(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitIntoSentences(%q) returned %d sentences, want %d: %v", tt.input, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("sentence[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMax(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{5, 3, 5},
		{3, 5, 5},
		{5, 5, 5},
		{-1, -5, -1},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d,%d", tt.a, tt.b), func(t *testing.T) {
			if got := max(tt.a, tt.b); got != tt.want {
				t.Errorf("max(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestMultiStringFlag(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		f := multiStringFlag{"a", "b", "c"}
		if got := f.String(); got != "a,b,c" {
			t.Errorf("String() = %q, want %q", got, "a,b,c")
		}
	})

	t.Run("Set single", func(t *testing.T) {
		var f multiStringFlag
		if err := f.Set("hello"); err != nil {
			t.Fatalf("Set() error: %v", err)
		}
		if len(f) != 1 || f[0] != "hello" {
			t.Errorf("after Set(\"hello\"), flag = %v", f)
		}
	})

	t.Run("Set comma-separated", func(t *testing.T) {
		var f multiStringFlag
		if err := f.Set("a, b, c"); err != nil {
			t.Fatalf("Set() error: %v", err)
		}
		if len(f) != 3 || f[0] != "a" || f[1] != "b" || f[2] != "c" {
			t.Errorf("after Set(\"a, b, c\"), flag = %v", f)
		}
	})

	t.Run("Set empty", func(t *testing.T) {
		var f multiStringFlag
		if err := f.Set(""); err != nil {
			t.Fatalf("Set() error: %v", err)
		}
		if len(f) != 0 {
			t.Errorf("after Set(\"\"), flag = %v, want empty", f)
		}
	})
}

// --- HTTP-mocked tests ---

// helper to write JSON responses in test handlers
func writeJSON(w http.ResponseWriter, v interface{}) {
	_ = json.NewEncoder(w).Encode(v)
}

// helper to save/restore base URLs
func overrideBaseURLs(t *testing.T, openAI, gemini string) {
	t.Helper()
	origOpenAI := openAIBaseURL
	origGemini := geminiBaseURL
	openAIBaseURL = openAI
	geminiBaseURL = gemini
	t.Cleanup(func() {
		openAIBaseURL = origOpenAI
		geminiBaseURL = origGemini
	})
}

func TestMakeOpenAIRequest(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
			}
			resp := ChatCompletionResponse{
				Choices: []struct {
					Message Message `json:"message"`
				}{
					{Message: Message{Role: "assistant", Content: "Hello!"}},
				},
			}
			writeJSON(w, resp)
		}))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		reqBody := ChatCompletionRequest{
			Model:    "gpt-4",
			Messages: []Message{{Role: "user", Content: "Hi"}},
		}
		got, err := makeOpenAIRequest(reqBody, "test-key", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Choices[0].Message.Content != "Hello!" {
			t.Errorf("got content %q, want %q", got.Choices[0].Message.Content, "Hello!")
		}
	})

	t.Run("429 rate limit no retry", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		_, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 0)
		if err == nil {
			t.Fatal("expected error for 429")
		}
		if !strings.Contains(err.Error(), "429") {
			t.Errorf("error should mention 429: %v", err)
		}
	})

	t.Run("500 error no retry", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		}))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		_, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 0)
		if err == nil {
			t.Fatal("expected error for 500")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error should mention 500: %v", err)
		}
	})

	t.Run("empty choices", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, ChatCompletionResponse{})
		}))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		_, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 0)
		if err == nil {
			t.Fatal("expected error for empty choices")
		}
		if !strings.Contains(err.Error(), "no response") {
			t.Errorf("error should mention no response: %v", err)
		}
	})
}

func TestMakeGeminiRequest(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-goog-api-key") != "test-key" {
				t.Errorf("unexpected api key header: %s", r.Header.Get("x-goog-api-key"))
			}
			if !strings.Contains(r.URL.Path, "gemini-2.0-flash") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			resp := GeminiResponse{
				Candidates: []GeminiCandidate{
					{Content: struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					}{
						Parts: []struct {
							Text string `json:"text"`
						}{{Text: "Gemini says hello"}},
					}},
				},
			}
			writeJSON(w, resp)
		}))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		contents := []GeminiContent{{Parts: []GeminiPart{{Text: "Hi"}}}}
		got, err := makeGeminiRequest("gemini-2.0-flash", contents, "test-key", 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Candidates[0].Content.Parts[0].Text != "Gemini says hello" {
			t.Errorf("got %q, want %q", got.Candidates[0].Content.Parts[0].Text, "Gemini says hello")
		}
	})

	t.Run("429 rate limit", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		_, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 0)
		if err == nil {
			t.Fatal("expected error for 429")
		}
		if !strings.Contains(err.Error(), "rate limit") {
			t.Errorf("error should mention rate limit: %v", err)
		}
	})

	t.Run("API error in body", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := GeminiResponse{
				Error: &GeminiError{Code: 400, Message: "bad request", Status: "INVALID_ARGUMENT"},
			}
			writeJSON(w, resp)
		}))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		_, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 0)
		if err == nil {
			t.Fatal("expected error for API error in body")
		}
		if !strings.Contains(err.Error(), "bad request") {
			t.Errorf("error should contain message: %v", err)
		}
	})

	t.Run("empty candidates", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, GeminiResponse{})
		}))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		_, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 0)
		if err == nil {
			t.Fatal("expected error for empty candidates")
		}
		if !strings.Contains(err.Error(), "no response") {
			t.Errorf("error should mention no response: %v", err)
		}
	})
}

func TestProcessWithOpenAI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatCompletionResponse{
			Choices: []struct {
				Message Message `json:"message"`
			}{
				{Message: Message{Role: "assistant", Content: "Summary: meeting notes"}},
			},
		}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	action := &PostAction{
		Model:       "gpt-4",
		Prompt:      "Summarize this",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	got, err := processWithOpenAI("transcript text", action, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Summary: meeting notes" {
		t.Errorf("got %q, want %q", got, "Summary: meeting notes")
	}
}

func TestProcessWithGemini(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GeminiResponse{
			Candidates: []GeminiCandidate{
				{Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{{Text: "Gemini summary"}},
				}},
			},
		}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	action := &PostAction{
		Model:       "gemini-2.0-flash",
		Prompt:      "Summarize this",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	got, err := processWithGemini("transcript text", action, "test-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Gemini summary" {
		t.Errorf("got %q, want %q", got, "Gemini summary")
	}
}

func TestTranscribeWithGemini(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := GeminiResponse{
			Candidates: []GeminiCandidate{
				{Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{{Text: "transcribed audio text"}},
				}},
			},
		}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	// Create a small temp audio file
	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeWithGemini(tmpFile, "test-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "transcribed audio text" {
		t.Errorf("got %q, want %q", got, "transcribed audio text")
	}
}

func TestTranscribeAudio(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a multipart request
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("expected multipart request, got %s", r.Header.Get("Content-Type"))
		}
		resp := TranscriptionResponse{Text: "whisper transcription"}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("fake audio data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudio(tmpFile, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "whisper transcription" {
		t.Errorf("got %q, want %q", got, "whisper transcription")
	}
}

// --- run() integration tests ---

func TestRunListActions(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	// Create config
	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey:     "XXXX",
		listActions: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTranscriptMode(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	// Setup mock server
	ts := httptest.NewServer(openAISuccessHandler("processed output"))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	// Create a transcript file
	transcriptFile := filepath.Join(t.TempDir(), "test-transcript.txt")
	if err := os.WriteFile(transcriptFile, []byte("This is a test transcript."), 0644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		enableFallback:  true,
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{transcriptFile},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTranscriptModeMultipleFiles(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	ts := httptest.NewServer(openAISuccessHandler("processed output"))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	dir := t.TempDir()
	file1 := filepath.Join(dir, "t1.txt")
	file2 := filepath.Join(dir, "t2.txt")
	if err := os.WriteFile(file1, []byte("Transcript one."), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if err := os.WriteFile(file2, []byte("Transcript two."), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		enableFallback:  true,
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{file1, file2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunTranscriptModeNoAction(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		transcriptFiles: []string{"file.txt"},
	})
	if err == nil {
		t.Fatal("expected error when no action specified")
	}
	if !strings.Contains(err.Error(), "-action or --auto") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunTranscriptFileNotFound(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		postAction:      "openai-meeting-summary",
		transcriptFiles: []string{"/nonexistent/file.txt"},
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunNoAudioFile(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{apiKey: "test-key"})
	if err == nil {
		t.Fatal("expected error for missing audio file")
	}
	if !strings.Contains(err.Error(), "audio file path is required") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunAudioFileNotFound(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	err := run(runOptions{
		apiKey: "test-key",
		args:   []string{"/nonexistent/audio.mp3"},
	})
	if err == nil {
		t.Fatal("expected error for missing audio")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunWithCustomConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := `openai_api_key: "from-config"
post_actions:
  - id: "test-action"
    name: "Test Action"
    type: "openai"
    prompt: "Test"
    model: "gpt-4"
    temperature: 0.3
    max_tokens: 1000
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := run(runOptions{
		apiKey:     "XXXX",
		configFile: configPath,
		listActions: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUnknownAction(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	tmpHome := t.TempDir()
	_ = os.Setenv("HOME", tmpHome)

	if err := createDefaultConfig(); err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	transcriptFile := filepath.Join(t.TempDir(), "t.txt")
	if err := os.WriteFile(transcriptFile, []byte("text"), 0644); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	err := run(runOptions{
		apiKey:          "test-key",
		postAction:      "nonexistent-action",
		transcriptFiles: []string{transcriptFile},
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestRunWithProviderFromConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	configContent := `provider: "gemini"
gemini_api_key: "gemini-from-config"
gemini_model: "gemini-2.0-flash"
post_actions:
  - id: "test"
    name: "Test"
    type: "gemini"
    prompt: "Test"
    model: "gemini-2.0-flash"
    temperature: 0.3
    max_tokens: 1000
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := run(runOptions{
		apiKey:     "XXXX",
		configFile: configPath,
		listActions: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- normalizeArgs tests ---

func TestNormalizeArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    []string
		wantErr bool
	}{
		{
			name:  "no transcript flag",
			input: []string{"-k", "key", "audio.mp3"},
			want:  []string{"-k", "key", "audio.mp3"},
		},
		{
			name:  "transcript with single file",
			input: []string{"-transcript", "file1.txt", "-action", "summary"},
			want:  []string{"-transcript", "file1.txt", "-action", "summary"},
		},
		{
			name:  "transcript with multiple files",
			input: []string{"-transcript", "file1.txt", "file2.txt", "-action", "summary"},
			want:  []string{"-transcript", "file1.txt,file2.txt", "-action", "summary"},
		},
		{
			name:  "transcript= with multiple files",
			input: []string{"-transcript=file1.txt", "file2.txt", "-action", "summary"},
			want:  []string{"-transcript=file1.txt,file2.txt", "-action", "summary"},
		},
		{
			name:    "transcript with no value",
			input:   []string{"-transcript"},
			wantErr: true,
		},
		{
			name:  "empty args",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "transcript with files at end",
			input: []string{"-transcript", "a.txt", "b.txt", "c.txt"},
			want:  []string{"-transcript", "a.txt,b.txt,c.txt"},
		},
		{
			name:  "other flags pass through",
			input: []string{"-k", "key", "-o", "out.txt", "audio.mp3"},
			want:  []string{"-k", "key", "-o", "out.txt", "audio.mp3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeArgs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeArgs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- Chunked processing tests ---

// geminiSuccessHandler returns an httptest handler that responds with the given text
func geminiSuccessHandler(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := GeminiResponse{
			Candidates: []GeminiCandidate{
				{Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{{Text: text}},
				}},
			},
		}
		writeJSON(w, resp)
	}
}

// openAISuccessHandler returns an httptest handler that responds with the given text
func openAISuccessHandler(text string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := ChatCompletionResponse{
			Choices: []struct {
				Message Message `json:"message"`
			}{
				{Message: Message{Role: "assistant", Content: text}},
			},
		}
		writeJSON(w, resp)
	}
}

func TestProcessWithOpenAIChunked(t *testing.T) {
	t.Run("small transcript fits in context", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("processed result"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		action := &PostAction{
			Model:       "gpt-4",
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		got, err := processWithOpenAIChunked("short transcript", action, "test-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "processed result" {
			t.Errorf("got %q, want %q", got, "processed result")
		}
	})

	t.Run("large transcript triggers chunking", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("merged output"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		action := &PostAction{
			Model:       "gpt-4", // 6000 token limit = ~24000 chars
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		// Create a transcript that exceeds gpt-4 context limit
		// gpt-4 limit = 6000 tokens * 4 chars = 24000 chars
		bigTranscript := strings.Repeat("This is a test sentence. ", 2000) // ~50000 chars
		got, err := processWithOpenAIChunked(bigTranscript, action, "test-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "merged output" {
			t.Errorf("got %q, want %q", got, "merged output")
		}
	})
}

func TestProcessWithGeminiChunked(t *testing.T) {
	t.Run("small transcript fits in context", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("gemini processed"))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		action := &PostAction{
			Model:       "gemini-2.0-flash",
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		got, err := processWithGeminiChunked("short text", action, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gemini processed" {
			t.Errorf("got %q, want %q", got, "gemini processed")
		}
	})

	t.Run("default model when empty", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("default model"))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		action := &PostAction{
			Prompt:      "Summarize",
			Temperature: 0.3,
			MaxTokens:   1000,
		}
		got, err := processWithGeminiChunked("short text", action, "key", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "default model" {
			t.Errorf("got %q, want %q", got, "default model")
		}
	})
}

func TestMergeChunkResults(t *testing.T) {
	t.Run("single chunk no merge", func(t *testing.T) {
		got, err := mergeChunkResults([]string{"only chunk"}, &PostAction{Model: "gpt-4", MaxTokens: 1000}, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "only chunk" {
			t.Errorf("got %q, want %q", got, "only chunk")
		}
	})

	t.Run("multiple chunks merged via API", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("merged result"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		action := &PostAction{
			Name:      "Test Action",
			Model:     "gpt-4o", // large context
			MaxTokens: 1000,
		}
		got, err := mergeChunkResults([]string{"chunk1", "chunk2"}, action, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "merged result" {
			t.Errorf("got %q, want %q", got, "merged result")
		}
	})
}

func TestMergeChunkResultsWithGemini(t *testing.T) {
	t.Run("single chunk no merge", func(t *testing.T) {
		got, err := mergeChunkResultsWithGemini([]string{"only chunk"}, &PostAction{}, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "only chunk" {
			t.Errorf("got %q, want %q", got, "only chunk")
		}
	})

	t.Run("multiple chunks merged via API", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("gemini merged"))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		action := &PostAction{Name: "Test", MaxTokens: 1000}
		got, err := mergeChunkResultsWithGemini([]string{"chunk1", "chunk2"}, action, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gemini merged" {
			t.Errorf("got %q, want %q", got, "gemini merged")
		}
	})

	t.Run("API error falls back to concatenation", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
		}))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		action := &PostAction{Name: "Test", MaxTokens: 1000}
		got, err := mergeChunkResultsWithGemini([]string{"chunk1", "chunk2"}, action, "key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should fall back to simple concatenation
		if !strings.Contains(got, "chunk1") || !strings.Contains(got, "chunk2") {
			t.Errorf("expected concatenated chunks, got %q", got)
		}
	})
}

func TestHierarchicalMerge(t *testing.T) {
	t.Run("single result passthrough", func(t *testing.T) {
		got, err := hierarchicalMerge([]string{"solo"}, &PostAction{Model: "gpt-4o", Name: "Test", MaxTokens: 1000}, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "solo" {
			t.Errorf("got %q, want %q", got, "solo")
		}
	})

	t.Run("two results merged", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("pair merged"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		action := &PostAction{Model: "gpt-4o", Name: "Test", MaxTokens: 1000}
		got, err := hierarchicalMerge([]string{"a", "b"}, action, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "pair merged" {
			t.Errorf("got %q, want %q", got, "pair merged")
		}
	})

	t.Run("three results with odd one out", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("final merge"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		action := &PostAction{Model: "gpt-4o", Name: "Test", MaxTokens: 1000}
		got, err := hierarchicalMerge([]string{"a", "b", "c"}, action, "key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "final merge" {
			t.Errorf("got %q, want %q", got, "final merge")
		}
	})
}

func TestSelectBestActions(t *testing.T) {
	// Setup test actions
	origActions := postActions
	defer func() { postActions = origActions }()
	postActions = []PostAction{
		{ID: "summary", Name: "Summary", Description: "Generate summary"},
		{ID: "action-items", Name: "Action Items", Description: "Extract action items"},
	}

	ts := httptest.NewServer(openAISuccessHandler("summary,action-items"))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	got, err := selectBestActions("transcript text", "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "summary" || got[1] != "action-items" {
		t.Errorf("got %v, want [summary, action-items]", got)
	}
}

func TestSelectBestActionsNoValid(t *testing.T) {
	origActions := postActions
	defer func() { postActions = origActions }()
	postActions = []PostAction{
		{ID: "summary", Name: "Summary", Description: "Generate summary"},
	}

	ts := httptest.NewServer(openAISuccessHandler("nonexistent-action"))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	_, err := selectBestActions("transcript text", "test-key")
	if err == nil {
		t.Fatal("expected error for no valid actions")
	}
	if !strings.Contains(err.Error(), "no valid actions") {
		t.Errorf("error = %q, want containing 'no valid actions'", err.Error())
	}
}

func TestSelectBestActionsWithGemini(t *testing.T) {
	origActions := postActions
	defer func() { postActions = origActions }()
	postActions = []PostAction{
		{ID: "summary", Name: "Summary", Description: "Generate summary"},
		{ID: "action-items", Name: "Action Items", Description: "Extract action items"},
	}

	ts := httptest.NewServer(geminiSuccessHandler("summary,action-items"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	got, err := selectBestActionsWithGemini("transcript", "key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "summary" || got[1] != "action-items" {
		t.Errorf("got %v, want [summary, action-items]", got)
	}
}

func TestSelectBestActionsWithGeminiNoValid(t *testing.T) {
	origActions := postActions
	defer func() { postActions = origActions }()
	postActions = []PostAction{
		{ID: "summary", Name: "Summary", Description: "Generate summary"},
	}

	ts := httptest.NewServer(geminiSuccessHandler("nonexistent"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	_, err := selectBestActionsWithGemini("transcript", "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error for no valid actions")
	}
}

// --- Provider dispatch success tests ---

func TestTranscribeAudioWithProviderSuccess(t *testing.T) {
	t.Run("openai success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, TranscriptionResponse{Text: "openai transcript"})
		}))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		tmpFile := filepath.Join(t.TempDir(), "test.mp3")
		if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}

		got, err := transcribeAudioWithProvider(tmpFile, "openai", "test-key", "", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "openai transcript" {
			t.Errorf("got %q, want %q", got, "openai transcript")
		}
	})

	t.Run("gemini success", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("gemini transcript"))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		tmpFile := filepath.Join(t.TempDir(), "test.mp3")
		if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}

		got, err := transcribeAudioWithProvider(tmpFile, "gemini", "", "test-key", "gemini-2.0-flash", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gemini transcript" {
			t.Errorf("got %q, want %q", got, "gemini transcript")
		}
	})
}

func TestProcessWithProviderChunkedSuccess(t *testing.T) {
	action := &PostAction{
		Model:       "gpt-4",
		Prompt:      "Summarize",
		Temperature: 0.3,
		MaxTokens:   1000,
	}

	t.Run("openai success", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("openai result"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		got, err := processWithProviderChunked("transcript", action, "openai", "test-key", "", "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "openai result" {
			t.Errorf("got %q, want %q", got, "openai result")
		}
	})

	t.Run("gemini success", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("gemini result"))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		got, err := processWithProviderChunked("transcript", action, "gemini", "", "test-key", "gemini-2.0-flash", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "gemini result" {
			t.Errorf("got %q, want %q", got, "gemini result")
		}
	})
}

func TestSelectBestActionsWithProviderSuccess(t *testing.T) {
	origActions := postActions
	defer func() { postActions = origActions }()
	postActions = []PostAction{
		{ID: "summary", Name: "Summary", Description: "Generate summary"},
	}

	t.Run("openai success", func(t *testing.T) {
		ts := httptest.NewServer(openAISuccessHandler("summary"))
		defer ts.Close()
		overrideBaseURLs(t, ts.URL, "")

		got, err := selectBestActionsWithProvider("transcript", "openai", "test-key", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "summary" {
			t.Errorf("got %v, want [summary]", got)
		}
	})

	t.Run("gemini success", func(t *testing.T) {
		ts := httptest.NewServer(geminiSuccessHandler("summary"))
		defer ts.Close()
		overrideBaseURLs(t, "", ts.URL)

		got, err := selectBestActionsWithProvider("transcript", "gemini", "", "test-key", "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != "summary" {
			t.Errorf("got %v, want [summary]", got)
		}
	})
}

// --- Additional coverage tests ---

func TestMakeOpenAIRequestRetryThenSuccess(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
			return
		}
		writeJSON(w, ChatCompletionResponse{
			Choices: []struct {
				Message Message `json:"message"`
			}{
				{Message: Message{Content: "success after retry"}},
			},
		})
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	got, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Choices[0].Message.Content != "success after retry" {
		t.Errorf("got %q", got.Choices[0].Message.Content)
	}
}

func TestMakeGeminiRequestRetryThenSuccess(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
			return
		}
		resp := GeminiResponse{
			Candidates: []GeminiCandidate{
				{Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{{Text: "success after retry"}},
				}},
			},
		}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	got, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Candidates[0].Content.Parts[0].Text != "success after retry" {
		t.Errorf("got %q", got.Candidates[0].Content.Parts[0].Text)
	}
}

func TestTranscribeAudioRetryThenSuccess(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("error"))
			return
		}
		writeJSON(w, TranscriptionResponse{Text: "retry success"})
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudio(tmpFile, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "retry success" {
		t.Errorf("got %q, want %q", got, "retry success")
	}
}

func TestTranscribeWithGeminiUnsupportedFormat(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.xyz")
	if err := os.WriteFile(tmpFile, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := transcribeWithGemini(tmpFile, "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported audio format") {
		t.Errorf("error = %q, want containing 'unsupported audio format'", err.Error())
	}
}

func TestTranscribeWithGeminiDefaultModel(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("transcribed"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeWithGemini(tmpFile, "key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "transcribed" {
		t.Errorf("got %q, want %q", got, "transcribed")
	}
}

func TestProcessWithGeminiDefaultModel(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("default model response"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	action := &PostAction{
		Prompt:      "Summarize",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	got, err := processWithGemini("transcript", action, "key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "default model response" {
		t.Errorf("got %q, want %q", got, "default model response")
	}
}

func TestSelectBestActionsWithGeminiDefaultModel(t *testing.T) {
	origActions := postActions
	defer func() { postActions = origActions }()
	postActions = []PostAction{
		{ID: "summary", Name: "Summary", Description: "Generate summary"},
	}

	ts := httptest.NewServer(geminiSuccessHandler("summary"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	got, err := selectBestActionsWithGemini("transcript", "key", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "summary" {
		t.Errorf("got %v, want [summary]", got)
	}
}

func TestResetConfig(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Logf("failed to restore HOME: %v", err)
		}
	}()

	tmpHome := t.TempDir()
	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("failed to set HOME: %v", err)
	}

	// When no config exists, resetConfig should create default without prompting
	err := resetConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify config was created
	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("resetConfig() did not create config file")
	}
}

func TestTranscribeAudioWithProviderFallback(t *testing.T) {
	// Test fallback from openai to gemini
	callCount := 0
	tsOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("openai down"))
	}))
	defer tsOpenAI.Close()

	tsGemini := httptest.NewServer(geminiSuccessHandler("gemini fallback"))
	defer tsGemini.Close()

	overrideBaseURLs(t, tsOpenAI.URL, tsGemini.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudioWithProvider(tmpFile, "openai", "openai-key", "gemini-key", "gemini-2.0-flash", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gemini fallback" {
		t.Errorf("got %q, want %q", got, "gemini fallback")
	}
}

func TestProcessWithProviderChunkedFallback(t *testing.T) {
	tsOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer tsOpenAI.Close()

	tsGemini := httptest.NewServer(geminiSuccessHandler("gemini fallback result"))
	defer tsGemini.Close()

	overrideBaseURLs(t, tsOpenAI.URL, tsGemini.URL)

	action := &PostAction{
		Model:       "gpt-4",
		Prompt:      "Summarize",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	got, err := processWithProviderChunked("transcript", action, "openai", "openai-key", "gemini-key", "gemini-2.0-flash", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gemini fallback result" {
		t.Errorf("got %q, want %q", got, "gemini fallback result")
	}
}

func TestTranscribeAudioWithProviderBothFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := transcribeAudioWithProvider(tmpFile, "openai", "openai-key", "gemini-key", "gemini-2.0-flash", true)
	if err == nil {
		t.Fatal("expected error when both providers fail")
	}
	if !strings.Contains(err.Error(), "primary") || !strings.Contains(err.Error(), "fallback") {
		t.Errorf("error should mention both primary and fallback: %v", err)
	}
}

func TestTranscribeWithGeminiFileTooLarge(t *testing.T) {
	// Create a file >20MB
	tmpFile := filepath.Join(t.TempDir(), "large.mp3")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := f.Truncate(21 * 1024 * 1024); err != nil {
		t.Fatalf("failed to truncate: %v", err)
	}
	f.Close()

	_, err = transcribeWithGemini(tmpFile, "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error for file too large")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, want containing 'too large'", err.Error())
	}
}

func TestTranscribeAudioNonExistentFile(t *testing.T) {
	_, err := transcribeAudio("/nonexistent/file.mp3", "key")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestTranscribeWithGeminiNonExistentFile(t *testing.T) {
	_, err := transcribeWithGemini("/nonexistent/file.mp3", "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// --- Splitting with small files (no ffmpeg needed) ---

func TestTranscribeAudioWithSplitting(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, TranscriptionResponse{Text: "small file transcript"})
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("small audio data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudioWithSplitting(tmpFile, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "small file transcript" {
		t.Errorf("got %q, want %q", got, "small file transcript")
	}
}

func TestTranscribeWithGeminiWithSplitting(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("gemini small file"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("small audio data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeWithGeminiWithSplitting(tmpFile, "key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gemini small file" {
		t.Errorf("got %q, want %q", got, "gemini small file")
	}
}

func TestTranscribeWithGeminiWithSplittingBadFile(t *testing.T) {
	_, err := transcribeWithGeminiWithSplitting("/nonexistent/file.mp3", "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestTranscribeAudioWithSplittingBadFile(t *testing.T) {
	_, err := transcribeAudioWithSplitting("/nonexistent/file.mp3", "key")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// --- Large transcript chunking (Gemini) ---

func TestProcessWithGeminiChunkedLargeTranscript(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("chunk result"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	action := &PostAction{
		Prompt:      "Summarize",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	// gemini-1.0-pro has 28000 token limit = ~112000 chars
	// Create a transcript just big enough to need 2 chunks but small enough
	// that the merge results (short strings from mock) fit in context
	model := "gemini-1.0-pro"
	bigTranscript := strings.Repeat("This is a test sentence. ", 6000) // ~150000 chars > 112000
	got, err := processWithGeminiChunked(bigTranscript, action, "key", model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty result")
	}
}

// --- Merge with hierarchical path ---

func TestHierarchicalMergeFourResults(t *testing.T) {
	ts := httptest.NewServer(openAISuccessHandler("merged"))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	action := &PostAction{Model: "gpt-4o", Name: "Test", MaxTokens: 1000}
	got, err := hierarchicalMerge([]string{"a", "b", "c", "d"}, action, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "merged" {
		t.Errorf("got %q, want %q", got, "merged")
	}
}

// --- Rate limit retry paths ---

func TestTranscribeAudioRateLimit(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Please try again in 0.1s"))
			return
		}
		writeJSON(w, TranscriptionResponse{Text: "after rate limit"})
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudio(tmpFile, "test-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "after rate limit" {
		t.Errorf("got %q, want %q", got, "after rate limit")
	}
}

func TestMakeOpenAIRequestRateLimit(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Please try again in 0.1s"))
			return
		}
		writeJSON(w, ChatCompletionResponse{
			Choices: []struct {
				Message Message `json:"message"`
			}{
				{Message: Message{Content: "after rate limit"}},
			},
		})
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	got, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Choices[0].Message.Content != "after rate limit" {
		t.Errorf("got %q", got.Choices[0].Message.Content)
	}
}

func TestMakeGeminiRequestRateLimit(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Please try again in 0.1s"))
			return
		}
		resp := GeminiResponse{
			Candidates: []GeminiCandidate{
				{Content: struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				}{
					Parts: []struct {
						Text string `json:"text"`
					}{{Text: "after rate limit"}},
				}},
			},
		}
		writeJSON(w, resp)
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	got, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Candidates[0].Content.Parts[0].Text != "after rate limit" {
		t.Errorf("got %q", got.Candidates[0].Content.Parts[0].Text)
	}
}

func TestMakeGeminiRequestNon200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	_, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400: %v", err)
	}
}

func TestMakeGeminiRequestBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	_, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 0)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse: %v", err)
	}
}

func TestMakeOpenAIRequestBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	_, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 0)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse: %v", err)
	}
}

func TestTranscribeAudioBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := transcribeAudio(tmpFile, "key")
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestTranscribeAudioAllRetriesFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("always fail"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := transcribeAudio(tmpFile, "key")
	if err == nil {
		t.Fatal("expected error when all retries fail")
	}
}

// --- Additional gap coverage ---

func TestTranscribeAudioWithProviderGeminiToOpenAIFallback(t *testing.T) {
	tsGemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("gemini down"))
	}))
	defer tsGemini.Close()

	tsOpenAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, TranscriptionResponse{Text: "openai fallback"})
	}))
	defer tsOpenAI.Close()

	overrideBaseURLs(t, tsOpenAI.URL, tsGemini.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudioWithProvider(tmpFile, "gemini", "openai-key", "gemini-key", "gemini-2.0-flash", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "openai fallback" {
		t.Errorf("got %q, want %q", got, "openai fallback")
	}
}

func TestTranscribeAudioWithProviderNoFallbackKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// openai fails, no gemini key for fallback
	_, err := transcribeAudioWithProvider(tmpFile, "openai", "openai-key", "", "", true)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessWithProviderChunkedGeminiToOpenAIFallback(t *testing.T) {
	tsGemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer tsGemini.Close()

	tsOpenAI := httptest.NewServer(openAISuccessHandler("openai fallback"))
	defer tsOpenAI.Close()

	overrideBaseURLs(t, tsOpenAI.URL, tsGemini.URL)

	action := &PostAction{Model: "gpt-4", Prompt: "test", Temperature: 0.3, MaxTokens: 100}
	got, err := processWithProviderChunked("transcript", action, "gemini", "openai-key", "gemini-key", "gemini-2.0-flash", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "openai fallback" {
		t.Errorf("got %q, want %q", got, "openai fallback")
	}
}

func TestProcessWithProviderChunkedBothFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, ts.URL)

	action := &PostAction{Model: "gpt-4", Prompt: "test", Temperature: 0.3, MaxTokens: 100}
	_, err := processWithProviderChunked("transcript", action, "openai", "openai-key", "gemini-key", "gemini-2.0-flash", true)
	if err == nil {
		t.Fatal("expected error when both fail")
	}
	if !strings.Contains(err.Error(), "primary") && !strings.Contains(err.Error(), "fallback") {
		t.Errorf("expected primary/fallback in error: %v", err)
	}
}

func TestProcessWithProviderChunkedNoFallbackKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, ts.URL)

	action := &PostAction{Model: "gpt-4", Prompt: "test", Temperature: 0.3, MaxTokens: 100}
	// gemini fails, no openai key for fallback
	_, err := processWithProviderChunked("transcript", action, "gemini", "", "gemini-key", "", true)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTranscribeAudioWithSplittingSmallFile(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, TranscriptionResponse{Text: "under limit"})
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	// Create a file under 25MB limit
	if err := os.WriteFile(tmpFile, []byte("small"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeAudioWithSplitting(tmpFile, "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "under limit" {
		t.Errorf("got %q, want %q", got, "under limit")
	}
}

func TestTranscribeWithGeminiWithSplittingSmallFile(t *testing.T) {
	ts := httptest.NewServer(geminiSuccessHandler("under limit"))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("small"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := transcribeWithGeminiWithSplitting(tmpFile, "key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "under limit" {
		t.Errorf("got %q, want %q", got, "under limit")
	}
}

func TestMakeOpenAIRequestNetworkError(t *testing.T) {
	// Point at a closed server to trigger network error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close() // Close immediately to cause network error

	overrideBaseURLs(t, url, "")
	_, err := makeOpenAIRequest(ChatCompletionRequest{}, "key", 0)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestMakeGeminiRequestNetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close()

	overrideBaseURLs(t, "", url)
	_, err := makeGeminiRequest("gemini-2.0-flash", nil, "key", 0)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestTranscribeAudioNetworkError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := ts.URL
	ts.Close()

	overrideBaseURLs(t, url, "")
	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := transcribeAudio(tmpFile, "key")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestProcessWithOpenAIChunkedError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	action := &PostAction{
		Model:       "gpt-4", // 6000 token limit
		Prompt:      "Summarize",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	// Large transcript that needs chunking
	bigTranscript := strings.Repeat("This is a test sentence. ", 2000)
	_, err := processWithOpenAIChunked(bigTranscript, action, "key")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to process chunk") {
		t.Errorf("error should mention chunk failure: %v", err)
	}
}

func TestProcessWithGeminiChunkedError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	action := &PostAction{
		Prompt:      "Summarize",
		Temperature: 0.3,
		MaxTokens:   1000,
	}
	model := "gemini-1.0-pro" // 28000 token limit
	bigTranscript := strings.Repeat("This is a test sentence. ", 6000)
	_, err := processWithGeminiChunked(bigTranscript, action, "key", model)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHierarchicalMergeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	action := &PostAction{Model: "gpt-4o", Name: "Test", MaxTokens: 1000}
	_, err := hierarchicalMerge([]string{"a", "b"}, action, "key")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMergeChunkResultsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	action := &PostAction{Name: "Test", Model: "gpt-4o", MaxTokens: 1000}
	_, err := mergeChunkResults([]string{"chunk1", "chunk2"}, action, "key")
	if err == nil {
		t.Fatal("expected error from merge")
	}
}

func TestSelectBestActionsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	_, err := selectBestActions("transcript", "key")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSelectBestActionsWithGeminiError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	_, err := selectBestActionsWithGemini("transcript", "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessWithGeminiError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	action := &PostAction{Prompt: "Summarize", Temperature: 0.3, MaxTokens: 1000}
	_, err := processWithGemini("transcript", action, "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTranscribeWithGeminiError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, "", ts.URL)

	tmpFile := filepath.Join(t.TempDir(), "test.mp3")
	if err := os.WriteFile(tmpFile, []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	_, err := transcribeWithGemini(tmpFile, "key", "gemini-2.0-flash")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessWithOpenAIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer ts.Close()
	overrideBaseURLs(t, ts.URL, "")

	action := &PostAction{Model: "gpt-4", Prompt: "test", Temperature: 0.3, MaxTokens: 1000}
	_, err := processWithOpenAI("transcript", action, "key")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Provider dispatch error tests ---

func TestTranscribeAudioWithProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		openai   string
		gemini   string
		wantErr  string
	}{
		{"gemini missing key", "gemini", "openai-key", "", "Gemini API key required"},
		{"openai missing key", "openai", "", "gemini-key", "OpenAI API key required"},
		{"openai XXXX key", "openai", "XXXX", "gemini-key", "OpenAI API key required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := transcribeAudioWithProvider("dummy.mp3", tt.provider, tt.openai, tt.gemini, "", false)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestProcessWithProviderChunked(t *testing.T) {
	action := &PostAction{
		Model:       "gpt-4",
		Prompt:      "test",
		Temperature: 0.3,
		MaxTokens:   100,
	}
	tests := []struct {
		name     string
		provider string
		openai   string
		gemini   string
		wantErr  string
	}{
		{"gemini missing key", "gemini", "openai-key", "", "Gemini API key required"},
		{"openai missing key", "openai", "", "gemini-key", "OpenAI API key required"},
		{"openai XXXX key", "openai", "XXXX", "gemini-key", "OpenAI API key required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := processWithProviderChunked("transcript", action, tt.provider, tt.openai, tt.gemini, "", false)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSelectBestActionsWithProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		openai   string
		gemini   string
		wantErr  string
	}{
		{"gemini missing key", "gemini", "openai-key", "", "Gemini API key required"},
		{"openai missing key", "openai", "", "gemini-key", "OpenAI API key required"},
		{"openai XXXX key", "openai", "XXXX", "gemini-key", "OpenAI API key required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := selectBestActionsWithProvider("transcript", tt.provider, tt.openai, tt.gemini, "")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}
