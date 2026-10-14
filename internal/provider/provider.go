package provider

import (
	"fmt"

	"goscribe/internal/elevenlabs"
	"goscribe/internal/gemini"
	"goscribe/internal/harness"
	"goscribe/internal/openai"
	"goscribe/internal/whisperlocal"
	"goscribe/pkg/config"
)

// TranscribeOptions selects the engine that turns audio into text.
// Transcriber is one of config.ValidTranscribers; empty means "same as Provider".
type TranscribeOptions struct {
	Transcriber     string
	Provider        string
	OpenAIKey       string
	GeminiKey       string
	GeminiModel     string
	ElevenLabsKey   string
	ElevenLabsModel string
	WhisperModel    string
	Language        string
	Fallback        bool
}

// Transcribe dispatches to the selected engine. local and elevenlabs never fall
// back to a cloud LLM provider: an explicit engine choice should fail loudly.
func Transcribe(audioPath string, o TranscribeOptions) (string, error) {
	switch o.Transcriber {
	case "local":
		return whisperlocal.Transcribe(audioPath, o.WhisperModel, o.Language)
	case "elevenlabs":
		return elevenlabs.Transcribe(audioPath, o.ElevenLabsKey, o.ElevenLabsModel, o.Language)
	case "", "openai", "gemini":
		prov := o.Transcriber
		if prov == "" {
			prov = o.Provider
		}
		return TranscribeAudio(audioPath, prov, o.OpenAIKey, o.GeminiKey, o.GeminiModel, o.Fallback)
	default:
		return "", fmt.Errorf("unknown transcriber '%s'", o.Transcriber)
	}
}

func TranscribeAudio(audioPath, provider, openaiKey, geminiKey, geminiModel string, enableFallback bool) (string, error) {
	var primaryErr error
	var result string

	switch provider {
	case "gemini":
		if geminiKey == "" {
			return "", fmt.Errorf("Gemini API key required. Use -gemini-key or -set-gemini-key")
		}
		result, primaryErr = gemini.TranscribeAudioWithSplitting(audioPath, geminiKey, geminiModel)
	case "openai":
		fallthrough
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return "", fmt.Errorf("OpenAI API key required. Use -k or -set-key")
		}
		result, primaryErr = openai.TranscribeAudioWithSplitting(audioPath, openaiKey)
	}

	if primaryErr == nil {
		return result, nil
	}

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
				result, fallbackErr = gemini.TranscribeAudioWithSplitting(audioPath, altKey, geminiModel)
			} else {
				result, fallbackErr = openai.TranscribeAudioWithSplitting(audioPath, altKey)
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

func ProcessChunked(transcript string, action *config.PostAction, provider, openaiKey, geminiKey, geminiModel string, enableFallback bool) (string, error) {
	// Local harnesses (claude, codex) use their own login and never fall back to an API.
	if harness.IsHarness(provider) {
		return harness.Process(transcript, action, provider)
	}

	var primaryErr error
	var result string

	switch provider {
	case "gemini":
		if geminiKey == "" {
			return "", fmt.Errorf("Gemini API key required")
		}
		result, primaryErr = gemini.ProcessChunked(transcript, action, geminiKey, geminiModel)
	case "openai":
		fallthrough
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return "", fmt.Errorf("OpenAI API key required")
		}
		result, primaryErr = openai.ProcessChunked(transcript, action, openaiKey)
	}

	if primaryErr == nil {
		return result, nil
	}

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
				result, fallbackErr = gemini.ProcessChunked(transcript, action, altKey, geminiModel)
			} else {
				result, fallbackErr = openai.ProcessChunked(transcript, action, altKey)
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

func SelectBestActions(transcript string, actions []config.PostAction, provider, openaiKey, geminiKey, geminiModel string) ([]string, error) {
	switch provider {
	case "claude", "codex":
		return harness.SelectBestActions(transcript, actions, provider)
	case "gemini":
		if geminiKey == "" {
			return nil, fmt.Errorf("Gemini API key required for auto-selection")
		}
		return gemini.SelectBestActions(transcript, actions, geminiKey, geminiModel)
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return nil, fmt.Errorf("OpenAI API key required for auto-selection")
		}
		return openai.SelectBestActions(transcript, actions, openaiKey)
	}
}
