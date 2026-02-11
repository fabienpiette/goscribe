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
