package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxFileSizeBytes = 25 * 1024 * 1024 // 25MB - OpenAI Whisper API limit

// Approximate token limits for different models (leaving room for prompt and response)
const avgCharsPerToken = 4 // Rough estimate: 1 token ≈ 4 characters

// Base URLs for API endpoints (overridable in tests)
var openAIBaseURL = "https://api.openai.com/v1"
var geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

var postActions = []PostAction{}

// getModelContextLimit returns the safe context limit for a model (input tokens only)
func getModelContextLimit(model string) int {
	switch {
	// OpenAI models
	case model == "gpt-4":
		return 6000 // 8K total, leaving 2K for completion
	case model == "gpt-4-32k":
		return 24000 // 32K total, leaving 8K for completion
	case strings.HasPrefix(model, "gpt-4-turbo") || model == "gpt-4-1106-preview" || model == "gpt-4-0125-preview":
		return 100000 // 128K total, leaving 28K for completion
	case strings.HasPrefix(model, "gpt-4o"):
		return 100000 // 128K total
	case strings.HasPrefix(model, "gpt-3.5-turbo"):
		return 12000 // 16K total, leaving 4K for completion
	// Gemini models
	case strings.HasPrefix(model, "gemini-2"):
		return 900000 // 1M context, leaving room for response
	case strings.HasPrefix(model, "gemini-1.5"):
		return 900000 // 1M context
	case strings.HasPrefix(model, "gemini-1.0"):
		return 28000 // 32K context
	case strings.HasPrefix(model, "gemini"):
		return 28000 // Conservative default for unknown Gemini models
	default:
		return 6000 // Conservative default
	}
}

// parseRateLimitWaitTime extracts the wait time from OpenAI rate limit error messages
// Example: "Please try again in 9.798s" -> 9.798
func parseRateLimitWaitTime(errorBody string) time.Duration {
	// Try to parse "Please try again in X.XXXs" pattern
	re := regexp.MustCompile(`try again in ([\d.]+)s`)
	matches := re.FindStringSubmatch(errorBody)
	if len(matches) >= 2 {
		if seconds, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return time.Duration(seconds*1000) * time.Millisecond
		}
	}
	// Default fallback: 10 seconds if parsing fails
	return 10 * time.Second
}

// getMimeType returns the MIME type for an audio file extension
func getMimeType(ext string) string {
	mimeTypes := map[string]string{
		".wav":  "audio/wav",
		".mp3":  "audio/mp3",
		".aiff": "audio/aiff",
		".aac":  "audio/aac",
		".ogg":  "audio/ogg",
		".flac": "audio/flac",
		".m4a":  "audio/mp4",
		".mp4":  "audio/mp4",
		".mpeg": "audio/mpeg",
		".mpga": "audio/mpeg",
		".webm": "audio/webm",
	}
	return mimeTypes[strings.ToLower(ext)]
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func splitIntoSentences(text string) []string {
	// Simple sentence splitter (splits on . ! ? followed by space)
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence endings
		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			// Check if followed by space or end of text
			if i+1 >= len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' {
				sentence := strings.TrimSpace(current.String())
				if len(sentence) > 0 {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	// Add any remaining text
	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if len(sentence) > 0 {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func getFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func splitAudioFile(audioPath string, chunkDurationSeconds int) ([]string, error) {
	// Create temporary directory for chunks
	tmpDir, err := os.MkdirTemp("", "goscribe-chunks-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	baseName := filepath.Base(audioPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	outputPattern := filepath.Join(tmpDir, nameWithoutExt+"_chunk_%03d"+ext)

	// Use ffmpeg to split the file
	cmd := fmt.Sprintf("ffmpeg -i %s -f segment -segment_time %d -c copy -reset_timestamps 1 %s",
		shellescape(audioPath),
		chunkDurationSeconds,
		shellescape(outputPattern))

	output, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	// Find all generated chunk files
	chunks, err := filepath.Glob(filepath.Join(tmpDir, nameWithoutExt+"_chunk_*"+ext))
	if err != nil {
		return nil, fmt.Errorf("failed to find chunk files: %w", err)
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks were created")
	}

	return chunks, nil
}

func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func findAction(id string) *PostAction {
	for i := range postActions {
		if postActions[i].ID == id {
			return &postActions[i]
		}
	}
	return nil
}
