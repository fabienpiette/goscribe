package song

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	providerPkg "goscribe/internal/provider"
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
	vp := filepath.Join(tmpDir, "htdemucs", stem, "vocals.wav")
	return vp, func() { os.RemoveAll(tmpDir) }, nil
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
	model := geminiModel
	if prov != "gemini" {
		model = "gpt-4o-mini"
	}
	action := config.PostAction{
		ID:          "lyrics-validate",
		Name:        "Lyrics Validation",
		Type:        prov,
		Model:       model,
		MaxTokens:   2000,
		Temperature: 0.2,
		Prompt:      strings.ReplaceAll(validationPrompt, "{transcript}", transcript),
	}
	result, err := providerPkg.ProcessChunked(transcript, &action, prov, openaiKey, geminiKey, model, fallback)
	if err != nil {
		return nil, fmt.Errorf("lyrics validation: %w", err)
	}
	var v lyrics.LyricsValidation
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		return nil, fmt.Errorf("parsing lyrics validation response: %w", err)
	}
	return &v, nil
}
