package song

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"goscribe/internal/gemini"
	"goscribe/internal/openai"
	"goscribe/pkg/config"
	"goscribe/pkg/lyrics"
)

func ExtractVocals(audioPath string) (vocalsPath string, cleanup func(), err error) {
	if _, err := exec.LookPath("demucs"); err != nil {
		return "", nil, fmt.Errorf("demucs not found: install with 'pip install demucs' or 'pipx install demucs'")
	}

	tmpDir, err := os.MkdirTemp("", "goscribe-demucs-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	output, err := exec.Command("demucs", "--two-stems=vocals", "-n", "htdemucs", "-o", tmpDir, audioPath).CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("demucs failed: %w\nOutput: %s", err, string(output))
	}

	stem := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	wavPath := filepath.Join(tmpDir, "htdemucs", stem, "vocals.wav")

	// Convert WAV to MP3 to stay within provider upload limits (Gemini: 20 MB, OpenAI: 25 MB).
	// A 3-minute vocals WAV is ~50 MB uncompressed; MP3 at 192 kbps is ~4 MB.
	mp3Path := filepath.Join(tmpDir, "vocals.mp3")
	conv, err := exec.Command("ffmpeg", "-i", wavPath, "-q:a", "0", "-y", mp3Path).CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("ffmpeg conversion failed: %w\nOutput: %s", err, string(conv))
	}

	return mp3Path, func() { os.RemoveAll(tmpDir) }, nil
}

const validationPrompt = `You are an expert in lyrics validation, linguistics, and speech-to-text error detection.
Your task is to evaluate the coherence and viability of transcribed song lyrics.

Return ONLY valid JSON. Do not include explanations outside the JSON.

--- INPUT ---
{transcript}
--- END INPUT ---

Analyze the lyrics and return:

{
  "coherence_score": number,
  "viability_score": number,
  "structure_score": number,
  "is_plausible_song": boolean,
  "coherence_issues": [{"line": string, "issue": string}],
  "structure_analysis": {
    "has_repetition": boolean,
    "has_chorus_pattern": boolean,
    "structure_consistent": boolean,
    "notes": string
  },
  "semantic_consistency": {
    "has_theme": boolean,
    "theme_description": string,
    "contradictions": [string]
  },
  "suspected_errors": [{"original": string, "suggestion": string, "reason": string}],
  "cleaned_lyrics": string,
  "confidence": number
}

Scoring guidelines (numeric 0-100):
- 0-30: corrupted / random text
- 30-60: partially plausible, many errors
- 60-85: mostly coherent
- 85-100: highly plausible lyrics

Important:
- Be conservative: do NOT hallucinate corrections
- Only suggest fixes when reasonably confident
- Keep cleaned_lyrics close to original`

func ValidateLyrics(transcript, prov, openaiKey, geminiKey, geminiModel string, fallback bool) (*lyrics.LyricsValidation, error) {
	primaryModel := geminiModel
	if prov != "gemini" {
		primaryModel = "gpt-4o-mini"
	}
	action := config.PostAction{
		ID:          "lyrics-validate",
		Name:        "Lyrics Validation",
		Type:        prov,
		Model:       primaryModel,
		MaxTokens:   2000,
		Temperature: 0.2,
		Prompt:      strings.ReplaceAll(validationPrompt, "{transcript}", transcript),
	}

	// Call providers directly instead of ProcessChunked to avoid double-injecting
	// the transcript: action.Prompt already contains the lyrics via {transcript}
	// replacement, and ProcessChunked would append it again via its basePrompt.
	var result string
	var primaryErr error

	switch prov {
	case "gemini":
		if geminiKey == "" {
			return nil, fmt.Errorf("Gemini API key required")
		}
		result, primaryErr = gemini.ProcessChunked("", &action, geminiKey, geminiModel)
		if primaryErr == nil {
			break
		}
		if fallback && openaiKey != "" && openaiKey != "XXXX" {
			fmt.Printf("  ⚠ %s failed, trying fallback provider openai...\n", prov)
			action.Model = "gpt-4o-mini"
			result, primaryErr = openai.ProcessChunked("", &action, openaiKey)
			if primaryErr == nil {
				fmt.Printf("  ✓ Fallback to openai succeeded\n")
			}
		}
	default:
		if openaiKey == "" || openaiKey == "XXXX" {
			return nil, fmt.Errorf("OpenAI API key required")
		}
		result, primaryErr = openai.ProcessChunked("", &action, openaiKey)
		if primaryErr == nil {
			break
		}
		if fallback && geminiKey != "" {
			fmt.Printf("  ⚠ %s failed, trying fallback provider gemini...\n", prov)
			action.Model = geminiModel
			result, primaryErr = gemini.ProcessChunked("", &action, geminiKey, geminiModel)
			if primaryErr == nil {
				fmt.Printf("  ✓ Fallback to gemini succeeded\n")
			}
		}
	}

	if primaryErr != nil {
		return nil, fmt.Errorf("lyrics validation: %w", primaryErr)
	}
	// Strip markdown code fences if the model wrapped the JSON (e.g. ```json\n{...}\n```)
	result = strings.TrimSpace(result)
	if strings.HasPrefix(result, "```") {
		result = strings.TrimPrefix(result, "```json")
		result = strings.TrimPrefix(result, "```")
		result = strings.TrimSuffix(result, "```")
		result = strings.TrimSpace(result)
	}
	var v lyrics.LyricsValidation
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		return nil, fmt.Errorf("parsing lyrics validation response: %w", err)
	}
	return &v, nil
}
