// Package elevenlabs transcribes audio with the ElevenLabs Scribe speech-to-text API.
package elevenlabs

import (
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
)

// BaseURL can be overridden in tests.
var BaseURL = "https://api.elevenlabs.io"

const DefaultModel = "scribe_v1"

type word struct {
	Text      string `json:"text"`
	Type      string `json:"type"`
	SpeakerID string `json:"speaker_id"`
}

type transcriptionResponse struct {
	Text  string `json:"text"`
	Words []word `json:"words"`
}

func Transcribe(audioPath, apiKey, model, language string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("ElevenLabs API key required. Use -elevenlabs-key, elevenlabs_api_key in config, or ELEVENLABS_API_KEY")
	}
	if model == "" {
		model = DefaultModel
	}

	file, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("opening audio file: %w", err)
	}
	defer file.Close()

	// Stream the multipart body so long recordings aren't loaded into memory.
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		fields := map[string]string{"model_id": model, "diarize": "true"}
		if language != "" {
			fields["language_code"] = language
		}
		for k, v := range fields {
			if err := writer.WriteField(k, v); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
		if err == nil {
			_, err = io.Copy(part, file)
		}
		if err == nil {
			err = writer.Close()
		}
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequest("POST", BaseURL+"/v1/speech-to-text", pr)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("xi-api-key", apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Long meetings can take several minutes to process server-side.
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ElevenLabs API error (status %d): %s", resp.StatusCode, util.TruncateString(string(body), 300))
	}

	var result transcriptionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return formatTranscript(result), nil
}

// formatTranscript groups consecutive words by speaker into "speaker_N: ..." paragraphs.
// Without diarization data it returns the plain text.
func formatTranscript(r transcriptionResponse) string {
	type turn struct {
		speaker string
		text    strings.Builder
	}
	var turns []*turn
	for _, w := range r.Words {
		if w.Type == "audio_event" || w.SpeakerID == "" {
			continue
		}
		if len(turns) == 0 || turns[len(turns)-1].speaker != w.SpeakerID {
			turns = append(turns, &turn{speaker: w.SpeakerID})
		}
		turns[len(turns)-1].text.WriteString(w.Text)
	}

	if len(turns) == 0 {
		return strings.TrimSpace(r.Text)
	}

	paragraphs := make([]string, 0, len(turns))
	for _, t := range turns {
		text := strings.TrimSpace(t.text.String())
		if text != "" {
			paragraphs = append(paragraphs, t.speaker+": "+text)
		}
	}
	return strings.Join(paragraphs, "\n\n")
}
