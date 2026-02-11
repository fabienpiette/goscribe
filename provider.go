package main

import "fmt"

// transcribeAudioWithProvider transcribes audio using the specified provider with optional fallback
func transcribeAudioWithProvider(audioPath, provider, openaiKey, geminiKey, geminiModel string, enableFallback bool) (string, error) {
	var primaryErr error
	var result string

	switch provider {
	case "gemini":
		if geminiKey == "" {
			return "", fmt.Errorf("Gemini API key required. Use -gemini-key or -set-gemini-key")
		}
		result, primaryErr = transcribeWithGeminiWithSplitting(audioPath, geminiKey, geminiModel)
	case "openai":
		fallthrough
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return "", fmt.Errorf("OpenAI API key required. Use -k or -set-key")
		}
		result, primaryErr = transcribeAudioWithSplitting(audioPath, openaiKey)
	}

	// If primary succeeded, return result
	if primaryErr == nil {
		return result, nil
	}

	// Try fallback if enabled and alternate key is available
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
				result, fallbackErr = transcribeWithGeminiWithSplitting(audioPath, altKey, geminiModel)
			} else {
				result, fallbackErr = transcribeAudioWithSplitting(audioPath, altKey)
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

// processWithProviderChunked processes transcript using the specified provider with optional fallback
func processWithProviderChunked(transcript string, action *PostAction, provider, openaiKey, geminiKey, geminiModel string, enableFallback bool) (string, error) {
	var primaryErr error
	var result string

	switch provider {
	case "gemini":
		if geminiKey == "" {
			return "", fmt.Errorf("Gemini API key required")
		}
		result, primaryErr = processWithGeminiChunked(transcript, action, geminiKey, geminiModel)
	case "openai":
		fallthrough
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return "", fmt.Errorf("OpenAI API key required")
		}
		result, primaryErr = processWithOpenAIChunked(transcript, action, openaiKey)
	}

	// If primary succeeded, return result
	if primaryErr == nil {
		return result, nil
	}

	// Try fallback if enabled and alternate key is available
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
				result, fallbackErr = processWithGeminiChunked(transcript, action, altKey, geminiModel)
			} else {
				result, fallbackErr = processWithOpenAIChunked(transcript, action, altKey)
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

// selectBestActionsWithProvider selects best actions using the specified provider
func selectBestActionsWithProvider(transcript, provider, openaiKey, geminiKey, geminiModel string) ([]string, error) {
	switch provider {
	case "gemini":
		if geminiKey == "" {
			return nil, fmt.Errorf("Gemini API key required for auto-selection")
		}
		return selectBestActionsWithGemini(transcript, geminiKey, geminiModel)
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return nil, fmt.Errorf("OpenAI API key required for auto-selection")
		}
		return selectBestActions(transcript, openaiKey)
	}
}
