package provider

import (
	"fmt"

	"goscribe/internal/gemini"
	"goscribe/internal/openai"
	"goscribe/pkg/config"
)

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
