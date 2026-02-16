package util

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

const MaxFileSizeBytes = 25 * 1024 * 1024 // 25MB - OpenAI Whisper API limit

const AvgCharsPerToken = 4 // Rough estimate: 1 token ≈ 4 characters

func GetModelContextLimit(model string) int {
	switch {
	case model == "gpt-4":
		return 6000
	case model == "gpt-4-32k":
		return 24000
	case strings.HasPrefix(model, "gpt-4-turbo") || model == "gpt-4-1106-preview" || model == "gpt-4-0125-preview":
		return 100000
	case strings.HasPrefix(model, "gpt-4o"):
		return 100000
	case strings.HasPrefix(model, "gpt-3.5-turbo"):
		return 12000
	case strings.HasPrefix(model, "gemini-2"):
		return 900000
	case strings.HasPrefix(model, "gemini-1.5"):
		return 900000
	case strings.HasPrefix(model, "gemini-1.0"):
		return 28000
	case strings.HasPrefix(model, "gemini"):
		return 28000
	default:
		return 6000
	}
}

func ParseRateLimitWaitTime(errorBody string) time.Duration {
	re := regexp.MustCompile(`try again in ([\d.]+)s`)
	matches := re.FindStringSubmatch(errorBody)
	if len(matches) >= 2 {
		if seconds, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return time.Duration(seconds*1000) * time.Millisecond
		}
	}
	return 10 * time.Second
}

func GetMimeType(ext string) string {
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

func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func SplitIntoSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		if runes[i] == '.' || runes[i] == '!' || runes[i] == '?' {
			if i+1 >= len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' {
				sentence := strings.TrimSpace(current.String())
				if len(sentence) > 0 {
					sentences = append(sentences, sentence)
				}
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		sentence := strings.TrimSpace(current.String())
		if len(sentence) > 0 {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func GetFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func SplitAudioFile(audioPath string, chunkDurationSeconds int) ([]string, error) {
	tmpDir, err := os.MkdirTemp("", "goscribe-chunks-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	baseName := filepath.Base(audioPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := strings.TrimSuffix(baseName, ext)

	outputPattern := filepath.Join(tmpDir, nameWithoutExt+"_chunk_%03d"+ext)

	cmd := fmt.Sprintf("ffmpeg -i %s -f segment -segment_time %d -c copy -reset_timestamps 1 %s",
		Shellescape(audioPath),
		chunkDurationSeconds,
		Shellescape(outputPattern))

	output, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w\nOutput: %s", err, string(output))
	}

	chunks, err := filepath.Glob(filepath.Join(tmpDir, nameWithoutExt+"_chunk_*"+ext))
	if err != nil {
		return nil, fmt.Errorf("failed to find chunk files: %w", err)
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks were created")
	}

	return chunks, nil
}

func Shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
