package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goscribe/internal/util"
	"goscribe/pkg/config"
)

// BaseURL can be overridden in tests.
var BaseURL = "https://generativelanguage.googleapis.com/v1beta"

type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string           `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func makeRequest(model string, contents []geminiContent, apiKey string, maxRetries int) (*geminiResponse, error) {
	var lastErr error
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", BaseURL, model)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		reqBody := geminiRequest{Contents: contents}
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("x-goog-api-key", apiKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := util.ParseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("Gemini API rate limit: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("Gemini API error (status %d): %s", resp.StatusCode, string(respBody))
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return nil, lastErr
		}

		var geminiResp geminiResponse
		err = json.Unmarshal(respBody, &geminiResp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
		}

		if geminiResp.Error != nil {
			return nil, fmt.Errorf("Gemini API error: %s", geminiResp.Error.Message)
		}

		if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("no response from Gemini API")
		}

		return &geminiResp, nil
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func transcribe(audioPath, apiKey, model string) (string, error) {
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}

	const geminiMaxInlineSize = 20 * 1024 * 1024
	if len(audioData) > geminiMaxInlineSize {
		return "", fmt.Errorf("audio file too large for Gemini inline upload (max 20MB, got %dMB)", len(audioData)/(1024*1024))
	}

	ext := filepath.Ext(audioPath)
	mimeType := util.GetMimeType(ext)
	if mimeType == "" {
		return "", fmt.Errorf("unsupported audio format: %s (supported: wav, mp3, aiff, aac, ogg, flac, m4a, webm)", ext)
	}

	base64Audio := base64.StdEncoding.EncodeToString(audioData)

	contents := []geminiContent{
		{
			Parts: []geminiPart{
				{Text: "Transcribe this audio file accurately. Output only the transcription text, no additional commentary or formatting."},
				{
					InlineData: &geminiInlineData{
						MimeType: mimeType,
						Data:     base64Audio,
					},
				},
			},
		},
	}

	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeRequest(model, contents, apiKey, 3)
	if err != nil {
		return "", err
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

func TranscribeAudioWithSplitting(audioPath, apiKey, model string) (string, error) {
	fileSize, err := util.GetFileSize(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %w", err)
	}

	const geminiMaxInlineSize = 20 * 1024 * 1024

	if fileSize <= geminiMaxInlineSize {
		return transcribe(audioPath, apiKey, model)
	}

	fmt.Printf("Audio file is large (%dMB), splitting into chunks...\n", fileSize/(1024*1024))

	chunks, err := util.SplitAudioFile(audioPath, 300)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	defer func() {
		if len(chunks) > 0 {
			os.RemoveAll(filepath.Dir(chunks[0]))
		}
	}()

	fmt.Printf("Split into %d chunk(s)\n", len(chunks))

	var allTranscripts []string
	for i, chunk := range chunks {
		fmt.Printf("Transcribing chunk %d/%d with Gemini...\n", i+1, len(chunks))
		transcript, err := transcribe(chunk, apiKey, model)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk %d: %w", i+1, err)
		}
		allTranscripts = append(allTranscripts, transcript)

		if i < len(chunks)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return strings.Join(allTranscripts, " "), nil
}

func processWithGemini(transcript string, action *config.PostAction, apiKey, model string) (string, error) {
	basePrompt := "You are a helpful assistant that processes transcribed text according to user instructions.\n\nTranscript:\n%s\n\nPlease process this transcript according to the instructions above."
	fullPrompt := action.Prompt + "\n\n" + fmt.Sprintf(basePrompt, transcript)

	contents := []geminiContent{
		{
			Parts: []geminiPart{
				{Text: fullPrompt},
			},
		},
	}

	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeRequest(model, contents, apiKey, 3)
	if err != nil {
		return "", err
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

func ProcessChunked(transcript string, action *config.PostAction, apiKey, model string) (string, error) {
	if model == "" {
		model = "gemini-2.0-flash"
	}

	maxTokens := util.GetModelContextLimit(model)
	promptTokens := len(action.Prompt) / util.AvgCharsPerToken
	transcriptTokens := len(transcript) / util.AvgCharsPerToken
	estimatedTokens := promptTokens + transcriptTokens

	if estimatedTokens <= maxTokens {
		return processWithGemini(transcript, action, apiKey, model)
	}

	fmt.Printf("  ⚠ Transcript is large (~%d tokens), processing in chunks...\n", estimatedTokens)

	maxChunkChars := (maxTokens - promptTokens - 1000) * util.AvgCharsPerToken

	sentences := util.SplitIntoSentences(transcript)

	var chunks []string
	var currentChunk strings.Builder
	var lastSentences []string

	for _, sentence := range sentences {
		if currentChunk.Len()+len(sentence) > maxChunkChars && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.String())

			currentChunk.Reset()
			for _, s := range lastSentences {
				currentChunk.WriteString(s)
				currentChunk.WriteString(" ")
			}
		}

		currentChunk.WriteString(sentence)
		currentChunk.WriteString(" ")

		lastSentences = append(lastSentences, sentence)
		if len(lastSentences) > 3 {
			lastSentences = lastSentences[1:]
		}
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	fmt.Printf("  → Split into %d chunk(s) for processing\n", len(chunks))

	var results []string
	for i, chunk := range chunks {
		fmt.Printf("  → Processing chunk %d/%d...\n", i+1, len(chunks))

		result, err := processWithGemini(chunk, action, apiKey, model)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}
		results = append(results, result)

		if i < len(chunks)-1 {
			fmt.Printf("  ⏸ Waiting 2 seconds before next chunk...\n")
			time.Sleep(2 * time.Second)
		}
	}

	fmt.Printf("  ✓ All chunks processed, merging results\n")
	return mergeChunkResults(results, action, apiKey, model)
}

func mergeChunkResults(chunkResults []string, action *config.PostAction, apiKey, model string) (string, error) {
	if len(chunkResults) == 1 {
		return chunkResults[0], nil
	}

	combinedChunks := strings.Join(chunkResults, "\n\n--- CHUNK BOUNDARY ---\n\n")

	mergePrompt := fmt.Sprintf(`You are merging multiple partial results from the same analysis that was split into chunks.

Original task: %s

Below are %d separate results from processing different parts of a transcript. Your job is to merge them into a single, coherent, comprehensive result that:
1. Removes duplicate information
2. Consolidates related points
3. Maintains the structure and format requested in the original task
4. Preserves all unique insights and details
5. Creates a unified narrative without chunk boundaries

Chunk results to merge:
%s

Provide the final merged result:`, action.Name, len(chunkResults), combinedChunks)

	contents := []geminiContent{
		{
			Parts: []geminiPart{
				{Text: mergePrompt},
			},
		},
	}

	resp, err := makeRequest(model, contents, apiKey, 3)
	if err != nil {
		fmt.Printf("  ⚠ Merge failed, falling back to simple concatenation: %v\n", err)
		return strings.Join(chunkResults, "\n\n---\n\n"), nil
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

func SelectBestActions(transcript string, actions []config.PostAction, apiKey, model string) ([]string, error) {
	var actionDescriptions []string
	for _, action := range actions {
		actionDescriptions = append(actionDescriptions, fmt.Sprintf("- %s: %s", action.ID, action.Description))
	}

	prompt := fmt.Sprintf(`Analyze the following transcript and select the 2-3 most appropriate post-processing actions from the list below.

Available actions:
%s

Transcript preview (first 2000 chars):
%s

Based on the content, which actions would provide the most value? Reply ONLY with a comma-separated list of action IDs (e.g., "openai-meeting-summary,openai-action-items"). Do not include any explanation.`,
		strings.Join(actionDescriptions, "\n"),
		util.TruncateString(transcript, 2000))

	contents := []geminiContent{
		{
			Parts: []geminiPart{
				{Text: prompt},
			},
		},
	}

	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeRequest(model, contents, apiKey, 3)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	response := strings.TrimSpace(resp.Candidates[0].Content.Parts[0].Text)
	selectedIDs := strings.Split(response, ",")

	var validIDs []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		if config.FindAction(actions, id) != nil {
			validIDs = append(validIDs, id)
		}
	}

	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid action IDs selected by AI: %s", response)
	}

	return validIDs, nil
}
