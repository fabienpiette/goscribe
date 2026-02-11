package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// makeOpenAIRequest makes an HTTP request to OpenAI API with retry logic
func makeOpenAIRequest(reqBody ChatCompletionRequest, apiKey string, maxRetries int) (*ChatCompletionResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", openAIBaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+apiKey)
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
			lastErr = fmt.Errorf("API request failed with status 429: %s", string(respBody))

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
			lastErr = fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
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
		var chatResp ChatCompletionResponse
		err = json.Unmarshal(respBody, &chatResp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if len(chatResp.Choices) == 0 {
			return nil, fmt.Errorf("no response from API")
		}

		return &chatResp, nil
	}

	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func transcribeAudio(audioPath, apiKey string) (string, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Open the audio file
		file, err := os.Open(audioPath)
		if err != nil {
			return "", fmt.Errorf("failed to open audio file: %w", err)
		}

		// Create a multipart form
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add the file to the form
		part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
		if err != nil {
			file.Close()
			return "", fmt.Errorf("failed to create form file: %w", err)
		}
		_, err = io.Copy(part, file)
		if err != nil {
			file.Close()
			return "", fmt.Errorf("failed to copy file: %w", err)
		}
		file.Close()

		// Add the model field
		err = writer.WriteField("model", "whisper-1")
		if err != nil {
			return "", fmt.Errorf("failed to write model field: %w", err)
		}

		// Close the writer
		err = writer.Close()
		if err != nil {
			return "", fmt.Errorf("failed to close writer: %w", err)
		}

		// Create the HTTP request
		req, err := http.NewRequest("POST", openAIBaseURL+"/audio/transcriptions", body)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		// Set headers
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		// Send the request
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
			return "", lastErr
		}
		defer resp.Body.Close()

		// Read the response
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		// Handle rate limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := parseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("API request failed with status 429: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return "", lastErr
		}

		// Check for other errors
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * time.Second
				fmt.Printf("  ⚠ Request failed, retrying in %v (attempt %d/%d)...\n",
					backoff, attempt+1, maxRetries)
				time.Sleep(backoff)
				continue
			}
			return "", lastErr
		}

		// Parse the response
		var transcriptionResp TranscriptionResponse
		err = json.Unmarshal(respBody, &transcriptionResp)
		if err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		return transcriptionResp.Text, nil
	}

	return "", fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func transcribeAudioWithSplitting(audioPath, apiKey string) (string, error) {
	// Check file size
	fileSize, err := getFileSize(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %w", err)
	}

	fileSizeMB := float64(fileSize) / (1024 * 1024)

	// If file is under the limit, transcribe normally
	if fileSize <= maxFileSizeBytes {
		return transcribeAudio(audioPath, apiKey)
	}

	// File is too large, need to split
	fmt.Printf("⚠ File size (%.1f MB) exceeds OpenAI limit (25 MB)\n", fileSizeMB)
	fmt.Println("Splitting audio file into chunks...")

	// Split into 10-minute chunks (600 seconds)
	// This ensures each chunk stays well under 25MB for most audio formats
	chunkDurationSeconds := 600

	chunks, err := splitAudioFile(audioPath, chunkDurationSeconds)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	defer func() {
		// Clean up chunks
		for _, chunk := range chunks {
			os.Remove(chunk)
		}
		os.Remove(filepath.Dir(chunks[0]))
	}()

	fmt.Printf("✓ Created %d chunks\n", len(chunks))

	// Transcribe each chunk
	var allTranscripts []string
	for i, chunk := range chunks {
		fmt.Printf("\n[%d/%d] Transcribing chunk %s...\n", i+1, len(chunks), filepath.Base(chunk))

		// Check chunk size
		chunkSize, _ := getFileSize(chunk)
		if chunkSize > maxFileSizeBytes {
			return "", fmt.Errorf("chunk %d is still too large (%.1f MB) - try a shorter chunk duration",
				i+1, float64(chunkSize)/(1024*1024))
		}

		transcript, err := transcribeAudio(chunk, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk %d: %w", i+1, err)
		}

		allTranscripts = append(allTranscripts, transcript)
		fmt.Printf("✓ Chunk %d/%d complete\n", i+1, len(chunks))
	}

	// Merge all transcripts
	fmt.Println("\n✓ All chunks transcribed successfully")
	return strings.Join(allTranscripts, " "), nil
}

func processWithOpenAI(transcript string, action *PostAction, apiKey string) (string, error) {
	basePrompt := "You are a helpful assistant that processes transcribed text according to user instructions.\n\nTranscript:\n%s\n\nPlease process this transcript according to the instructions above."

	fullPrompt := action.Prompt + "\n\n" + fmt.Sprintf(basePrompt, transcript)

	reqBody := ChatCompletionRequest{
		Model: action.Model,
		Messages: []Message{
			{
				Role:    "user",
				Content: fullPrompt,
			},
		},
		Temperature: action.Temperature,
		MaxTokens:   action.MaxTokens,
	}

	// Use the retry-enabled request helper (3 max retries)
	chatResp, err := makeOpenAIRequest(reqBody, apiKey, 3)
	if err != nil {
		return "", err
	}

	return chatResp.Choices[0].Message.Content, nil
}

func processWithOpenAIChunked(transcript string, action *PostAction, apiKey string) (string, error) {
	// Get model-specific context limit
	maxTokens := getModelContextLimit(action.Model)

	// Estimate transcript + prompt tokens
	promptTokens := len(action.Prompt) / avgCharsPerToken
	transcriptTokens := len(transcript) / avgCharsPerToken
	estimatedTokens := promptTokens + transcriptTokens

	// If transcript fits in context, process normally
	if estimatedTokens <= maxTokens {
		return processWithOpenAI(transcript, action, apiKey)
	}

	// Transcript is too long, need to chunk
	fmt.Printf("  ⚠ Transcript is large (~%d tokens), processing in chunks...\n", estimatedTokens)

	// Calculate chunk size (leaving room for prompt and overlap)
	maxTranscriptTokensPerChunk := maxTokens - promptTokens - 500 // 500 token buffer
	maxCharsPerChunk := maxTranscriptTokensPerChunk * avgCharsPerToken

	// Split transcript into sentences for better chunking
	sentences := splitIntoSentences(transcript)

	var chunks []string
	var currentChunk strings.Builder
	currentSize := 0

	for i, sentence := range sentences {
		sentenceLen := len(sentence)

		// If adding this sentence exceeds chunk size, start new chunk
		if currentSize > 0 && currentSize+sentenceLen > maxCharsPerChunk {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()

			// Add overlap from previous chunk
			overlapStart := max(0, i-3) // Include last few sentences for context
			for j := overlapStart; j < i; j++ {
				currentChunk.WriteString(sentences[j])
				currentChunk.WriteString(" ")
			}
			currentSize = currentChunk.Len()
		}

		currentChunk.WriteString(sentence)
		currentChunk.WriteString(" ")
		currentSize += sentenceLen + 1
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	fmt.Printf("  → Split into %d chunk(s) for processing\n", len(chunks))

	// Process each chunk with retry logic
	var results []string
	for i, chunk := range chunks {
		fmt.Printf("  → Processing chunk %d/%d...\n", i+1, len(chunks))

		// processWithOpenAI now has built-in retry logic
		result, err := processWithOpenAI(chunk, action, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}
		results = append(results, result)

		// Add a small delay between chunks to avoid hitting rate limits
		// Skip delay after the last chunk
		if i < len(chunks)-1 {
			delaySeconds := 2
			fmt.Printf("  ⏸ Waiting %d seconds before next chunk...\n", delaySeconds)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}
	}

	// Intelligently merge chunk results using AI
	fmt.Printf("  ✓ All chunks processed, merging results intelligently\n")
	merged, err := mergeChunkResults(results, action, apiKey)
	if err != nil {
		fmt.Printf("  ⚠ Merge failed, falling back to simple concatenation: %v\n", err)
		return strings.Join(results, "\n\n---\n\n"), nil
	}
	return merged, nil
}

func mergeChunkResults(chunkResults []string, action *PostAction, apiKey string) (string, error) {
	// If only 1 chunk, no merge needed
	if len(chunkResults) == 1 {
		return chunkResults[0], nil
	}

	// Combine all chunk results into a single text for merging
	combinedChunks := strings.Join(chunkResults, "\n\n--- CHUNK BOUNDARY ---\n\n")

	// Estimate tokens for merge prompt
	estimatedTokens := len(combinedChunks) / avgCharsPerToken
	maxTokens := getModelContextLimit(action.Model)

	// If merge would exceed limits, do hierarchical merge
	if estimatedTokens > maxTokens/2 { // Leave room for prompt + response
		fmt.Printf("  → Chunk results too large, using hierarchical merge\n")
		return hierarchicalMerge(chunkResults, action, apiKey)
	}

	// Create a merge prompt that understands the original action's intent
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

	// Use same model as the action for consistency
	reqBody := ChatCompletionRequest{
		Model: action.Model,
		Messages: []Message{
			{
				Role:    "user",
				Content: mergePrompt,
			},
		},
		Temperature: 0.3, // Lower temperature for consistency
		MaxTokens:   action.MaxTokens,
	}

	// Use the retry-enabled request helper (3 max retries)
	chatResp, err := makeOpenAIRequest(reqBody, apiKey, 3)
	if err != nil {
		return "", fmt.Errorf("merge request failed: %w", err)
	}

	return chatResp.Choices[0].Message.Content, nil
}

func hierarchicalMerge(chunkResults []string, action *PostAction, apiKey string) (string, error) {
	// Merge in pairs until we have a single result
	currentLevel := chunkResults

	for len(currentLevel) > 1 {
		var nextLevel []string
		fmt.Printf("  → Hierarchical merge: processing %d results\n", len(currentLevel))

		// Process in pairs
		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 < len(currentLevel) {
				// Merge pair
				pair := []string{currentLevel[i], currentLevel[i+1]}
				merged, err := mergeChunkResults(pair, action, apiKey)
				if err != nil {
					return "", fmt.Errorf("hierarchical merge failed at level: %w", err)
				}
				nextLevel = append(nextLevel, merged)
			} else {
				// Odd one out, pass through
				nextLevel = append(nextLevel, currentLevel[i])
			}
		}

		currentLevel = nextLevel
	}

	return currentLevel[0], nil
}

func selectBestActions(transcript string, apiKey string) ([]string, error) {
	// Build list of available actions for AI to choose from
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

	reqBody := ChatCompletionRequest{
		Model: "gpt-3.5-turbo",
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   100,
	}

	// Use the retry-enabled request helper (3 max retries)
	chatResp, err := makeOpenAIRequest(reqBody, apiKey, 3)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	// Parse the response (comma-separated action IDs)
	response := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	selectedIDs := strings.Split(response, ",")

	// Trim and validate each ID
	var validIDs []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		// Verify the action exists
		if findAction(id) != nil {
			validIDs = append(validIDs, id)
		}
	}

	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid actions selected by AI")
	}

	return validIDs, nil
}
