package realtime

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Local segments: speech is cut at the first pause after MinLocalSegment, or
// at MaxLocalSegment without one. Shorter segments mean less delay but worse
// accuracy and a model load per segment.
// Overridable for tests.
var (
	MinLocalSegment = 5 * time.Second
	MaxLocalSegment = 20 * time.Second
	LocalPause      = 800 * time.Millisecond
)

// LocalOptions configures StreamLocal.
type LocalOptions struct {
	// Transcribe turns one WAV file (16 kHz mono s16le) into text, e.g. with
	// faster-whisper.
	Transcribe func(wavPath string) (string, error)
}

// StreamLocal is the offline stand-in for Stream: it cuts the audio into
// segments at pauses, skips silence, and transcribes each segment with
// o.Transcribe, emitting one Committed event per segment. One segment at most
// waits while another is transcribed: when transcription is slower than
// speech, the oldest waiting one is dropped with a Warning, so the delay can't
// grow without bound. It returns nil once audio is closed and the cut
// segments are transcribed, or ctx.Err() when ctx is cancelled; emit is never
// called after it returns.
func StreamLocal(ctx context.Context, o LocalOptions, audio <-chan []byte, emit func(Event)) error {
	dir, err := os.MkdirTemp("", "goscribe-local-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}

	var mu sync.Mutex // guards stopped and every emit
	stopped := false
	send := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			emit(e)
		}
	}

	queue := make(chan []byte, 1)
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		defer os.RemoveAll(dir)
		for n := 0; ; n++ {
			pcm, ok := <-queue
			if !ok {
				return
			}
			text, err := transcribeSegment(dir, n, pcm, o.Transcribe)
			if err != nil {
				send(Event{Kind: Warning, Text: "local transcription failed: " + err.Error()})
				continue
			}
			if text != "" {
				send(Event{Kind: Committed, Text: text})
			}
		}
	}()

	enqueue := func(pcm []byte) {
		for {
			select {
			case queue <- pcm:
				return
			default:
				select {
				case old := <-queue:
					send(Event{Kind: Warning, Text: fmt.Sprintf("local transcription is falling behind: skipped %s of audio", pcmDuration(old).Round(time.Second))})
				default:
				}
			}
		}
	}

	var segment []byte
	var quiet time.Duration // quiet time at the end of the segment
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			stopped = true
			mu.Unlock()
			close(queue)
			return ctx.Err()
		case b, ok := <-audio:
			if !ok {
				if len(segment) > 0 {
					enqueue(segment)
				}
				close(queue)
				select {
				case <-worker:
				case <-ctx.Done():
				}
				mu.Lock()
				stopped = true
				mu.Unlock()
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return nil
			}
			loud := isLoud(b)
			if len(segment) == 0 && !loud {
				continue // silence between segments is not transcribed
			}
			segment = append(segment, b...)
			if loud {
				quiet = 0
			} else {
				quiet += pcmDuration(b)
			}
			length := pcmDuration(segment)
			if (length >= MinLocalSegment && quiet >= LocalPause) || length >= MaxLocalSegment {
				enqueue(segment)
				segment, quiet = nil, 0
			}
		}
	}
}

// transcribeSegment writes pcm as a WAV file in dir and transcribes it.
func transcribeSegment(dir string, n int, pcm []byte, transcribe func(string) (string, error)) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("segment-%04d.wav", n))
	if err := os.WriteFile(path, wav(pcm), 0600); err != nil {
		return "", err
	}
	defer os.Remove(path)
	text, err := transcribe(path)
	return strings.Join(strings.Fields(text), " "), err
}

// pcmDuration is the length of 16 kHz mono s16le audio.
func pcmDuration(pcm []byte) time.Duration {
	return time.Duration(len(pcm)) * time.Second / (SampleRate * 2)
}

// wav wraps 16 kHz mono s16le samples in a WAV header.
func wav(pcm []byte) []byte {
	b := make([]byte, 44, 44+len(pcm))
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(pcm)))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(b[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(b[22:], 1)  // mono
	binary.LittleEndian.PutUint32(b[24:], SampleRate)
	binary.LittleEndian.PutUint32(b[28:], SampleRate*2) // byte rate
	binary.LittleEndian.PutUint16(b[32:], 2)            // block align
	binary.LittleEndian.PutUint16(b[34:], 16)           // bits per sample
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)))
	return append(b, pcm...)
}
