// Package elevenlabs transcribes audio with the ElevenLabs Scribe speech-to-text API.
// Defaults are tuned for meeting recordings: speaker diarization, no filler words,
// no audio-event tags, and a timestamp at the start of each speaker turn.
package elevenlabs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"goscribe/internal/util"
)

// BaseURL can be overridden in tests.
var BaseURL = "https://api.elevenlabs.io"

const DefaultModel = "scribe_v2"

// Options configures a transcription request.
type Options struct {
	APIKey   string
	Model    string // default DefaultModel
	Language string // ISO-639-1/3 code; empty = auto-detect
	// Keyterms bias recognition toward names and jargon (e.g. product names).
	// ElevenLabs bills a 20% surcharge when set.
	Keyterms []string
	// NumSpeakers caps the number of speakers diarization may find (0 = auto, max 32).
	NumSpeakers int
	// Verbatim keeps filler words and false starts. By default they are removed
	// (no_verbatim), which only scribe_v2 and later support.
	Verbatim bool
}

type word struct {
	Text      string   `json:"text"`
	Type      string   `json:"type"`
	Start     *float64 `json:"start"`
	SpeakerID string   `json:"speaker_id"`
}

type transcriptionResponse struct {
	Text  string `json:"text"`
	Words []word `json:"words"`
}

// formFields returns the multipart fields for o, in a stable order.
// Repeated keys (keyterms) are sent as one field per value, as the API expects.
func formFields(o Options) [][2]string {
	model := o.Model
	if model == "" {
		model = DefaultModel
	}
	fields := [][2]string{
		{"model_id", model},
		{"diarize", "true"},
		{"tag_audio_events", "false"},
		{"timestamps_granularity", "word"},
	}
	if !o.Verbatim && model != "scribe_v1" {
		fields = append(fields, [2]string{"no_verbatim", "true"})
	}
	if o.Language != "" {
		fields = append(fields, [2]string{"language_code", o.Language})
	}
	if o.NumSpeakers > 0 {
		fields = append(fields, [2]string{"num_speakers", strconv.Itoa(o.NumSpeakers)})
	}
	for _, term := range o.Keyterms {
		if term = strings.TrimSpace(term); term != "" {
			fields = append(fields, [2]string{"keyterms", term})
		}
	}
	return fields
}

// ErrQuota marks an exhausted ElevenLabs quota: callers can fall back to
// another engine.
var ErrQuota = errors.New("ElevenLabs quota exceeded")

func Transcribe(audioPath string, o Options) (string, error) {
	if o.APIKey == "" {
		return "", fmt.Errorf("ElevenLabs API key required. Use -elevenlabs-key, elevenlabs_api_key in config, or ELEVENLABS_API_KEY")
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
		for _, f := range formFields(o) {
			if err := writer.WriteField(f[0], f[1]); err != nil {
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
	req.Header.Set("xi-api-key", o.APIKey)
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
		if bytes.Contains(body, []byte("quota_exceeded")) {
			return "", fmt.Errorf("%w (status %d): %s", ErrQuota, resp.StatusCode, util.TruncateString(string(body), 300))
		}
		return "", fmt.Errorf("ElevenLabs API error (status %d): %s", resp.StatusCode, util.TruncateString(string(body), 300))
	}

	var result transcriptionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	return formatTranscript(result), nil
}

// formatTranscript groups consecutive words by speaker into
// "[HH:MM:SS] speaker_N: ..." paragraphs, the timestamp being where the turn
// starts in the recording. Without diarization data it returns the plain text.
func formatTranscript(r transcriptionResponse) string {
	type turn struct {
		speaker string
		start   *float64
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
		t := turns[len(turns)-1]
		if t.start == nil && w.Type == "word" {
			t.start = w.Start
		}
		t.text.WriteString(w.Text)
	}

	if len(turns) == 0 {
		return strings.TrimSpace(r.Text)
	}

	paragraphs := make([]string, 0, len(turns))
	for _, t := range turns {
		text := strings.TrimSpace(t.text.String())
		if text == "" {
			continue
		}
		prefix := ""
		if t.start != nil {
			prefix = "[" + formatTimestamp(*t.start) + "] "
		}
		paragraphs = append(paragraphs, prefix+t.speaker+": "+text)
	}
	return strings.Join(paragraphs, "\n\n")
}

func formatTimestamp(seconds float64) string {
	s := int(seconds)
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s%3600/60, s%60)
}
