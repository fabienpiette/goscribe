package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bytes"
)

// makeGeminiRequest makes an HTTP request to Gemini API with retry logic
func makeGeminiRequest(model string, contents []GeminiContent, apiKey string, maxRetries int) (*GeminiResponse, error) {
	var lastErr error
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", geminiBaseURL, model)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		reqBody := GeminiRequest{Contents: contents}
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

		// Handle rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := parseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("Gemini API rate limit: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return nil, lastErr
		}

		// Handle other non-OK status codes
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

		// Success! Parse and return response
		var geminiResp GeminiResponse
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

// transcribeWithGemini transcribes audio using Gemini API
func transcribeWithGemini(audioPath, apiKey, model string) (string, error) {
	// Read and encode audio file
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}

	// Check file size (20MB limit for inline data)
	const geminiMaxInlineSize = 20 * 1024 * 1024
	if len(audioData) > geminiMaxInlineSize {
		return "", fmt.Errorf("audio file too large for Gemini inline upload (max 20MB, got %dMB)", len(audioData)/(1024*1024))
	}

	// Determine MIME type from extension
	ext := filepath.Ext(audioPath)
	mimeType := getMimeType(ext)
	if mimeType == "" {
		return "", fmt.Errorf("unsupported audio format: %s (supported: wav, mp3, aiff, aac, ogg, flac, m4a, webm)", ext)
	}

	// Base64 encode the audio
	base64Audio := base64.StdEncoding.EncodeToString(audioData)

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: "Transcribe this audio file accurately. Output only the transcription text, no additional commentary or formatting."},
				{
					InlineData: &GeminiInlineData{
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

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		return "", err
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// transcribeWithGeminiWithSplitting handles large audio files by splitting them
func transcribeWithGeminiWithSplitting(audioPath, apiKey, model string) (string, error) {
	fileSize, err := getFileSize(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %w", err)
	}

	// Gemini inline limit is 20MB
	const geminiMaxInlineSize = 20 * 1024 * 1024

	if fileSize <= geminiMaxInlineSize {
		return transcribeWithGemini(audioPath, apiKey, model)
	}

	// File is too large, need to split
	fmt.Printf("Audio file is large (%dMB), splitting into chunks...\n", fileSize/(1024*1024))

	// Split into 5-minute chunks (should be well under 20MB for most formats)
	chunks, err := splitAudioFile(audioPath, 300)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	defer func() {
		// Clean up temp files
		if len(chunks) > 0 {
			os.RemoveAll(filepath.Dir(chunks[0]))
		}
	}()

	fmt.Printf("Split into %d chunk(s)\n", len(chunks))

	var allTranscripts []string
	for i, chunk := range chunks {
		fmt.Printf("Transcribing chunk %d/%d with Gemini...\n", i+1, len(chunks))
		transcript, err := transcribeWithGemini(chunk, apiKey, model)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk %d: %w", i+1, err)
		}
		allTranscripts = append(allTranscripts, transcript)

		// Add delay between chunks to avoid rate limits
		if i < len(chunks)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	return strings.Join(allTranscripts, " "), nil
}

// processWithGemini processes transcript with Gemini API
func processWithGemini(transcript string, action *PostAction, apiKey, model string) (string, error) {
	basePrompt := "You are a helpful assistant that processes transcribed text according to user instructions.\n\nTranscript:\n%s\n\nPlease process this transcript according to the instructions above."
	fullPrompt := action.Prompt + "\n\n" + fmt.Sprintf(basePrompt, transcript)

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: fullPrompt},
			},
		},
	}

	// Use provided model or default
	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		return "", err
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// processWithGeminiChunked processes large transcripts with Gemini in chunks
func processWithGeminiChunked(transcript string, action *PostAction, apiKey, model string) (string, error) {
	if model == "" {
		model = "gemini-2.0-flash"
	}

	maxTokens := getModelContextLimit(model)
	promptTokens := len(action.Prompt) / avgCharsPerToken
	transcriptTokens := len(transcript) / avgCharsPerToken
	estimatedTokens := promptTokens + transcriptTokens

	// If transcript fits in context, process normally
	if estimatedTokens <= maxTokens {
		return processWithGemini(transcript, action, apiKey, model)
	}

	// Transcript is too long, need to chunk
	fmt.Printf("  ⚠ Transcript is large (~%d tokens), processing in chunks...\n", estimatedTokens)

	// Calculate chunk size (leaving room for prompt and overlap)
	maxChunkChars := (maxTokens - promptTokens - 1000) * avgCharsPerToken
	_ = 500 // Overlap chars - reserved for future use

	// Split into sentences first for better boundaries
	sentences := splitIntoSentences(transcript)

	// Group sentences into chunks
	var chunks []string
	var currentChunk strings.Builder
	var lastSentences []string // Keep track of last few sentences for overlap

	for _, sentence := range sentences {
		// Check if adding this sentence would exceed the limit
		if currentChunk.Len()+len(sentence) > maxChunkChars && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.String())

			// Start new chunk with overlap (last few sentences)
			currentChunk.Reset()
			for _, s := range lastSentences {
				currentChunk.WriteString(s)
				currentChunk.WriteString(" ")
			}
		}

		currentChunk.WriteString(sentence)
		currentChunk.WriteString(" ")

		// Update overlap buffer
		lastSentences = append(lastSentences, sentence)
		if len(lastSentences) > 3 {
			lastSentences = lastSentences[1:]
		}
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	fmt.Printf("  → Split into %d chunk(s) for processing\n", len(chunks))

	// Process each chunk
	var results []string
	for i, chunk := range chunks {
		fmt.Printf("  → Processing chunk %d/%d...\n", i+1, len(chunks))

		result, err := processWithGemini(chunk, action, apiKey, model)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}
		results = append(results, result)

		// Add delay between chunks
		if i < len(chunks)-1 {
			fmt.Printf("  ⏸ Waiting 2 seconds before next chunk...\n")
			time.Sleep(2 * time.Second)
		}
	}

	// Merge results
	fmt.Printf("  ✓ All chunks processed, merging results\n")
	return mergeChunkResultsWithGemini(results, action, apiKey, model)
}

// mergeChunkResultsWithGemini merges chunk results using Gemini
func mergeChunkResultsWithGemini(chunkResults []string, action *PostAction, apiKey, model string) (string, error) {
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

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: mergePrompt},
			},
		},
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		// Fall back to simple concatenation
		fmt.Printf("  ⚠ Merge failed, falling back to simple concatenation: %v\n", err)
		return strings.Join(chunkResults, "\n\n---\n\n"), nil
	}

	return resp.Candidates[0].Content.Parts[0].Text, nil
}

// selectBestActionsWithGemini uses Gemini to select best actions
func selectBestActionsWithGemini(transcript, apiKey, model string) ([]string, error) {
	var actionDescriptions []string
	for _, action := range postActions {
		actionDescriptions = append(actionDescriptions, fmt.Sprintf("- %s: %s", action.ID, action.Description))
	}

	prompt := fmt.Sprintf(`Analyze the following transcript and select the 2-3 most appropriate post-processing actions from the list below.

Available actions:
%s

Transcript preview (first 2000 chars):
%s

Based on the content, which actions would provide the most value? Reply ONLY with a comma-separated list of action IDs (e.g., "openai-meeting-summary,openai-action-items"). Do not include any explanation.`,
		strings.Join(actionDescriptions, "\n"),
		truncateString(transcript, 2000))

	contents := []GeminiContent{
		{
			Parts: []GeminiPart{
				{Text: prompt},
			},
		},
	}

	if model == "" {
		model = "gemini-2.0-flash"
	}

	resp, err := makeGeminiRequest(model, contents, apiKey, 3)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	// Parse the response
	response := strings.TrimSpace(resp.Candidates[0].Content.Parts[0].Text)
	selectedIDs := strings.Split(response, ",")

	// Validate each ID
	var validIDs []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		for _, action := range postActions {
			if action.ID == id {
				validIDs = append(validIDs, id)
				break
			}
		}
	}

	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid action IDs selected by AI: %s", response)
	}

	return validIDs, nil
}
