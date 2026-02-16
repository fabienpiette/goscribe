package openai

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

	"goscribe/internal/util"
	"goscribe/pkg/config"
)

// BaseURL can be overridden in tests.
var BaseURL = "https://api.openai.com/v1"

type transcriptionResponse struct {
	Text string `json:"text"`
}

type chatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

func makeRequest(reqBody chatCompletionRequest, apiKey string, maxRetries int) (*chatCompletionResponse, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest("POST", BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
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

		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := util.ParseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("API request failed with status 429: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return nil, lastErr
		}

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

		var chatResp chatCompletionResponse
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

func TranscribeAudio(audioPath, apiKey string) (string, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		file, err := os.Open(audioPath)
		if err != nil {
			return "", fmt.Errorf("failed to open audio file: %w", err)
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

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

		err = writer.WriteField("model", "whisper-1")
		if err != nil {
			return "", fmt.Errorf("failed to write model field: %w", err)
		}

		err = writer.Close()
		if err != nil {
			return "", fmt.Errorf("failed to close writer: %w", err)
		}

		req, err := http.NewRequest("POST", BaseURL+"/audio/transcriptions", body)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())

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

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			waitTime := util.ParseRateLimitWaitTime(string(respBody))
			lastErr = fmt.Errorf("API request failed with status 429: %s", string(respBody))

			if attempt < maxRetries {
				fmt.Printf("  ⏳ Rate limit hit, waiting %.1f seconds before retry %d/%d...\n",
					waitTime.Seconds(), attempt+1, maxRetries)
				time.Sleep(waitTime)
				continue
			}
			return "", lastErr
		}

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

		var transcriptionResp transcriptionResponse
		err = json.Unmarshal(respBody, &transcriptionResp)
		if err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		return transcriptionResp.Text, nil
	}

	return "", fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func TranscribeAudioWithSplitting(audioPath, apiKey string) (string, error) {
	fileSize, err := util.GetFileSize(audioPath)
	if err != nil {
		return "", fmt.Errorf("failed to get file size: %w", err)
	}

	fileSizeMB := float64(fileSize) / (1024 * 1024)

	if fileSize <= util.MaxFileSizeBytes {
		return TranscribeAudio(audioPath, apiKey)
	}

	fmt.Printf("⚠ File size (%.1f MB) exceeds OpenAI limit (25 MB)\n", fileSizeMB)
	fmt.Println("Splitting audio file into chunks...")

	chunkDurationSeconds := 600

	chunks, err := util.SplitAudioFile(audioPath, chunkDurationSeconds)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	defer func() {
		for _, chunk := range chunks {
			os.Remove(chunk)
		}
		os.Remove(filepath.Dir(chunks[0]))
	}()

	fmt.Printf("✓ Created %d chunks\n", len(chunks))

	var allTranscripts []string
	for i, chunk := range chunks {
		fmt.Printf("\n[%d/%d] Transcribing chunk %s...\n", i+1, len(chunks), filepath.Base(chunk))

		chunkSize, _ := util.GetFileSize(chunk)
		if chunkSize > util.MaxFileSizeBytes {
			return "", fmt.Errorf("chunk %d is still too large (%.1f MB) - try a shorter chunk duration",
				i+1, float64(chunkSize)/(1024*1024))
		}

		transcript, err := TranscribeAudio(chunk, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to transcribe chunk %d: %w", i+1, err)
		}

		allTranscripts = append(allTranscripts, transcript)
		fmt.Printf("✓ Chunk %d/%d complete\n", i+1, len(chunks))
	}

	fmt.Println("\n✓ All chunks transcribed successfully")
	return strings.Join(allTranscripts, " "), nil
}

func processWithOpenAI(transcript string, action *config.PostAction, apiKey string) (string, error) {
	basePrompt := "You are a helpful assistant that processes transcribed text according to user instructions.\n\nTranscript:\n%s\n\nPlease process this transcript according to the instructions above."

	fullPrompt := action.Prompt + "\n\n" + fmt.Sprintf(basePrompt, transcript)

	reqBody := chatCompletionRequest{
		Model: action.Model,
		Messages: []message{
			{
				Role:    "user",
				Content: fullPrompt,
			},
		},
		Temperature: action.Temperature,
		MaxTokens:   action.MaxTokens,
	}

	chatResp, err := makeRequest(reqBody, apiKey, 3)
	if err != nil {
		return "", err
	}

	return chatResp.Choices[0].Message.Content, nil
}

func ProcessChunked(transcript string, action *config.PostAction, apiKey string) (string, error) {
	maxTokens := util.GetModelContextLimit(action.Model)

	promptTokens := len(action.Prompt) / util.AvgCharsPerToken
	transcriptTokens := len(transcript) / util.AvgCharsPerToken
	estimatedTokens := promptTokens + transcriptTokens

	if estimatedTokens <= maxTokens {
		return processWithOpenAI(transcript, action, apiKey)
	}

	fmt.Printf("  ⚠ Transcript is large (~%d tokens), processing in chunks...\n", estimatedTokens)

	maxTranscriptTokensPerChunk := maxTokens - promptTokens - 500
	maxCharsPerChunk := maxTranscriptTokensPerChunk * util.AvgCharsPerToken

	sentences := util.SplitIntoSentences(transcript)

	var chunks []string
	var currentChunk strings.Builder
	currentSize := 0

	for i, sentence := range sentences {
		sentenceLen := len(sentence)

		if currentSize > 0 && currentSize+sentenceLen > maxCharsPerChunk {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()

			overlapStart := util.Max(0, i-3)
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

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	fmt.Printf("  → Split into %d chunk(s) for processing\n", len(chunks))

	var results []string
	for i, chunk := range chunks {
		fmt.Printf("  → Processing chunk %d/%d...\n", i+1, len(chunks))

		result, err := processWithOpenAI(chunk, action, apiKey)
		if err != nil {
			return "", fmt.Errorf("failed to process chunk %d: %w", i+1, err)
		}
		results = append(results, result)

		if i < len(chunks)-1 {
			delaySeconds := 2
			fmt.Printf("  ⏸ Waiting %d seconds before next chunk...\n", delaySeconds)
			time.Sleep(time.Duration(delaySeconds) * time.Second)
		}
	}

	fmt.Printf("  ✓ All chunks processed, merging results intelligently\n")
	merged, err := mergeChunkResults(results, action, apiKey)
	if err != nil {
		fmt.Printf("  ⚠ Merge failed, falling back to simple concatenation: %v\n", err)
		return strings.Join(results, "\n\n---\n\n"), nil
	}
	return merged, nil
}

func mergeChunkResults(chunkResults []string, action *config.PostAction, apiKey string) (string, error) {
	if len(chunkResults) == 1 {
		return chunkResults[0], nil
	}

	combinedChunks := strings.Join(chunkResults, "\n\n--- CHUNK BOUNDARY ---\n\n")

	estimatedTokens := len(combinedChunks) / util.AvgCharsPerToken
	maxTokens := util.GetModelContextLimit(action.Model)

	if estimatedTokens > maxTokens/2 {
		fmt.Printf("  → Chunk results too large, using hierarchical merge\n")
		return hierarchicalMerge(chunkResults, action, apiKey)
	}

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

	reqBody := chatCompletionRequest{
		Model: action.Model,
		Messages: []message{
			{
				Role:    "user",
				Content: mergePrompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   action.MaxTokens,
	}

	chatResp, err := makeRequest(reqBody, apiKey, 3)
	if err != nil {
		return "", fmt.Errorf("merge request failed: %w", err)
	}

	return chatResp.Choices[0].Message.Content, nil
}

func hierarchicalMerge(chunkResults []string, action *config.PostAction, apiKey string) (string, error) {
	currentLevel := chunkResults

	for len(currentLevel) > 1 {
		var nextLevel []string
		fmt.Printf("  → Hierarchical merge: processing %d results\n", len(currentLevel))

		for i := 0; i < len(currentLevel); i += 2 {
			if i+1 < len(currentLevel) {
				pair := []string{currentLevel[i], currentLevel[i+1]}
				merged, err := mergeChunkResults(pair, action, apiKey)
				if err != nil {
					return "", fmt.Errorf("hierarchical merge failed at level: %w", err)
				}
				nextLevel = append(nextLevel, merged)
			} else {
				nextLevel = append(nextLevel, currentLevel[i])
			}
		}

		currentLevel = nextLevel
	}

	return currentLevel[0], nil
}

func SelectBestActions(transcript string, actions []config.PostAction, apiKey string) ([]string, error) {
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

	reqBody := chatCompletionRequest{
		Model: "gpt-3.5-turbo",
		Messages: []message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature: 0.3,
		MaxTokens:   100,
	}

	chatResp, err := makeRequest(reqBody, apiKey, 3)
	if err != nil {
		return nil, fmt.Errorf("action selection failed: %w", err)
	}

	response := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	selectedIDs := strings.Split(response, ",")

	var validIDs []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		if config.FindAction(actions, id) != nil {
			validIDs = append(validIDs, id)
		}
	}

	if len(validIDs) == 0 {
		return nil, fmt.Errorf("no valid actions selected by AI")
	}

	return validIDs, nil
}
