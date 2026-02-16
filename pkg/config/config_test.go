package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindAction(t *testing.T) {
	actions := []PostAction{
		{ID: "test-action-1", Name: "Test Action 1"},
		{ID: "test-action-2", Name: "Test Action 2"},
		{ID: "test-action-3", Name: "Test Action 3"},
	}

	tests := []struct {
		name     string
		actionID string
		wantNil  bool
		wantID   string
	}{
		{
			name:     "Find existing action",
			actionID: "test-action-2",
			wantNil:  false,
			wantID:   "test-action-2",
		},
		{
			name:     "Action not found",
			actionID: "non-existent",
			wantNil:  true,
		},
		{
			name:     "Empty action ID",
			actionID: "",
			wantNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindAction(actions, tt.actionID)
			if tt.wantNil && got != nil {
				t.Errorf("FindAction() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("FindAction() = nil, want action with ID %s", tt.wantID)
			}
			if !tt.wantNil && got != nil && got.ID != tt.wantID {
				t.Errorf("FindAction() ID = %v, want %v", got.ID, tt.wantID)
			}
		})
	}
}

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
			err := ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
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

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Errorf("LoadConfig() error = %v, want nil", err)
	}

	if cfg.OpenAIAPIKey != "test-api-key" {
		t.Errorf("LoadConfig() apiKey = %v, want test-api-key", cfg.OpenAIAPIKey)
	}

	if len(cfg.PostActions) != 1 {
		t.Errorf("LoadConfig() loaded %d actions, want 1", len(cfg.PostActions))
	}

	if len(cfg.PostActions) > 0 {
		action := cfg.PostActions[0]
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

func TestLoadConfigInvalid(t *testing.T) {
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

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid config, got nil")
	}
}

func TestLoadConfigNonExistent(t *testing.T) {
	_, err := LoadConfig("/non/existent/path/config.yml")
	if err == nil {
		t.Error("LoadConfig() expected error for non-existent file, got nil")
	}
}

func TestLoadConfigGemini(t *testing.T) {
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

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Errorf("LoadConfig() error = %v, want nil", err)
	}

	if cfg.Provider != "gemini" {
		t.Errorf("Provider = %v, want gemini", cfg.Provider)
	}
	if cfg.GeminiAPIKey != "test-gemini-key" {
		t.Errorf("GeminiAPIKey = %v, want test-gemini-key", cfg.GeminiAPIKey)
	}
	if cfg.GeminiModel != "gemini-1.5-pro" {
		t.Errorf("GeminiModel = %v, want gemini-1.5-pro", cfg.GeminiModel)
	}
	if cfg.OpenAIAPIKey != "test-openai-key" {
		t.Errorf("OpenAIAPIKey = %v, want test-openai-key", cfg.OpenAIAPIKey)
	}

	if len(cfg.PostActions) != 1 {
		t.Errorf("LoadConfig() loaded %d actions, want 1", len(cfg.PostActions))
	}

	if len(cfg.PostActions) > 0 {
		action := cfg.PostActions[0]
		if action.Type != "gemini" {
			t.Errorf("Action Type = %v, want gemini", action.Type)
		}
		if action.Model != "gemini-2.0-flash" {
			t.Errorf("Action Model = %v, want gemini-2.0-flash", action.Model)
		}
	}
}

func TestStoreAPIKey(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	err := CreateDefault()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	testKey := "sk-test-api-key-12345"
	err = StoreAPIKey(testKey)
	if err != nil {
		t.Errorf("StoreAPIKey() error = %v, want nil", err)
	}

	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Errorf("Failed to load config after storing key: %v", err)
	}

	if cfg.OpenAIAPIKey != testKey {
		t.Errorf("Stored API key = %v, want %v", cfg.OpenAIAPIKey, testKey)
	}
}

func TestStoreGeminiAPIKey(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	err := CreateDefault()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	testKey := "test-gemini-api-key-12345"
	err = StoreGeminiAPIKey(testKey)
	if err != nil {
		t.Errorf("StoreGeminiAPIKey() error = %v, want nil", err)
	}

	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Errorf("Failed to load config after storing Gemini key: %v", err)
	}

	if cfg.GeminiAPIKey != testKey {
		t.Errorf("Stored Gemini API key = %v, want %v", cfg.GeminiAPIKey, testKey)
	}
}

func TestSetDefaultProvider(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	err := CreateDefault()
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "Set to gemini", provider: "gemini", wantErr: false},
		{name: "Set to openai", provider: "openai", wantErr: false},
		{name: "Invalid provider", provider: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetDefaultProvider(tt.provider)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetDefaultProvider(%q) error = %v, wantErr %v", tt.provider, err, tt.wantErr)
			}

			if !tt.wantErr {
				configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
				cfg, err := LoadConfig(configPath)
				if err != nil {
					t.Errorf("Failed to load config after setting provider: %v", err)
				}

				if cfg.Provider != tt.provider {
					t.Errorf("Stored provider = %v, want %v", cfg.Provider, tt.provider)
				}
			}
		})
	}
}

func TestCreateDefault(t *testing.T) {
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)

	err := CreateDefault()
	if err != nil {
		t.Errorf("CreateDefault() error = %v, want nil", err)
	}

	configPath := filepath.Join(tmpHome, ".goscribe", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("CreateDefault() did not create config file")
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Errorf("Failed to load default config: %v", err)
	}

	if cfg.OpenAIAPIKey != "" {
		t.Errorf("Default config API key = %v, want empty string", cfg.OpenAIAPIKey)
	}

	if len(cfg.PostActions) == 0 {
		t.Error("Default config has no actions")
	}
}

func TestGetDefaultConfigContent(t *testing.T) {
	content := getDefaultConfigContent()

	if content == "" {
		t.Error("getDefaultConfigContent() returned empty string")
	}

	expectedStrings := []string{
		"openai_api_key:",
		"post_actions:",
		"openai-meeting-summary",
		"openai-action-items",
		"provider:",
		"gemini_api_key:",
		"gemini_model:",
		"gemini-2.0-flash",
	}

	for _, expected := range expectedStrings {
		if !containsSubstring(content, expected) {
			t.Errorf("getDefaultConfigContent() missing expected string: %s", expected)
		}
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
