package main

import (
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

	apiKey, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("loadConfigActions() error = %v, want nil", err)
	}

	if apiKey != "test-api-key" {
		t.Errorf("loadConfigActions() apiKey = %v, want test-api-key", apiKey)
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
	apiKey, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("Failed to load config after storing key: %v", err)
	}

	if apiKey != testKey {
		t.Errorf("Stored API key = %v, want %v", apiKey, testKey)
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
	apiKey, err := loadConfigActions(configPath)
	if err != nil {
		t.Errorf("Failed to load default config: %v", err)
	}

	// Default config should have empty API key
	if apiKey != "" {
		t.Errorf("Default config API key = %v, want empty string", apiKey)
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
