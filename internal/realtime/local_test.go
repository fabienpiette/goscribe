package realtime

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// pcmChunk is 200 ms of 16 kHz mono s16le audio, loud or quiet.
func pcmChunk(loud bool) []byte {
	b := make([]byte, SampleRate*2/5)
	if loud {
		for i := 0; i+1 < len(b); i += 2 {
			b[i], b[i+1] = 0x00, 0x20 // 8192
		}
	}
	return b
}

// feed sends seconds of loud or quiet audio.
func feed(audio chan<- []byte, seconds float64, loud bool) {
	for i := 0; i < int(seconds*5); i++ {
		audio <- pcmChunk(loud)
	}
}

type fakeLocal struct {
	mu    sync.Mutex
	calls []time.Duration // length of each transcribed segment
	block chan struct{}   // when set, each call waits on it
}

func (f *fakeLocal) transcribe(wav string) (string, error) {
	data, err := os.ReadFile(wav)
	if err != nil {
		return "", err
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return "", os.ErrInvalid
	}
	f.mu.Lock()
	f.calls = append(f.calls, time.Duration(len(data)-44)*time.Second/(SampleRate*2))
	n := len(f.calls)
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return strings.Repeat("words ", n), nil
}

func (f *fakeLocal) lengths() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.calls...)
}

func collect() (func(Event), func() []Event) {
	var mu sync.Mutex
	var got []Event
	return func(e Event) {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}, func() []Event {
			mu.Lock()
			defer mu.Unlock()
			return append([]Event(nil), got...)
		}
}

// Speech is cut at a pause after the minimum length, quiet stretches are
// skipped, and the last segment is transcribed when the audio ends.
func TestStreamLocalSegments(t *testing.T) {
	f := &fakeLocal{}
	emit, events := collect()
	audio := make(chan []byte, 1000)
	feed(audio, 6, true)   // speech
	feed(audio, 1, false)  // a pause: cut here (7 s segment)
	feed(audio, 10, false) // silence only: skipped
	feed(audio, 3, true)   // last words, cut when the audio ends
	close(audio)
	if err := StreamLocal(context.Background(), LocalOptions{Transcribe: f.transcribe}, audio, emit); err != nil {
		t.Fatal(err)
	}
	lengths := f.lengths()
	if len(lengths) != 2 || lengths[0] < 6*time.Second || lengths[0] > 8*time.Second {
		t.Fatalf("segments = %v, want a 6–8 s one then the last words", lengths)
	}
	var committed []string
	for _, e := range events() {
		if e.Kind == Committed {
			committed = append(committed, e.Text)
		}
	}
	if strings.Join(committed, "|") != "words|words words" {
		t.Errorf("committed = %q", committed)
	}
}

// Without pauses, a segment is cut at the maximum length.
func TestStreamLocalMaxSegment(t *testing.T) {
	f := &fakeLocal{}
	emit, _ := collect()
	audio := make(chan []byte, 1000)
	feed(audio, 45, true)
	close(audio)
	if err := StreamLocal(context.Background(), LocalOptions{Transcribe: f.transcribe}, audio, emit); err != nil {
		t.Fatal(err)
	}
	lengths := f.lengths()
	if len(lengths) != 3 || lengths[0] != MaxLocalSegment || lengths[1] != MaxLocalSegment {
		t.Errorf("segments = %v, want two of %s and the rest", lengths, MaxLocalSegment)
	}
}

// When transcription is slower than speech, the oldest waiting segment is
// dropped with a warning, so the delay can't grow without bound.
func TestStreamLocalFallsBehind(t *testing.T) {
	f := &fakeLocal{block: make(chan struct{})}
	emit, events := collect()
	audio := make(chan []byte, 1000)
	for i := 0; i < 5; i++ { // five segments while the first one is stuck
		feed(audio, 6, true)
		feed(audio, 1, false)
	}
	close(audio)
	done := make(chan error, 1)
	go func() { done <- StreamLocal(context.Background(), LocalOptions{Transcribe: f.transcribe}, audio, emit) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		warned := false
		for _, e := range events() {
			if e.Kind == Warning && strings.Contains(e.Text, "falling behind") {
				warned = true
			}
		}
		if warned {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no falling-behind warning")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(f.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := len(f.lengths()); n >= 5 {
		t.Errorf("transcribed %d segments, want the dropped ones skipped", n)
	}
}

func TestStreamLocalCancel(t *testing.T) {
	f := &fakeLocal{}
	emit, _ := collect()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := StreamLocal(ctx, LocalOptions{Transcribe: f.transcribe}, make(chan []byte), emit); err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
