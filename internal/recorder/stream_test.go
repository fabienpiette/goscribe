package recorder

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStreamArgs(t *testing.T) {
	got := strings.Join(StreamArgs("alsa_input.usb-mic"), " ")
	for _, want := range []string{
		"-f pulse -thread_queue_size 1024 -fragment_size 3200 -i alsa_input.usb-mic",
		"-ac 1 -ar 16000 -f s16le -",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, " -") {
		t.Errorf("PCM must go to stdout (last arg \"-\"): %s", got)
	}
}

func TestStreamSendsChunksAndStopsOnCancel(t *testing.T) {
	// 2.5 chunks of audio, then waits like a live device.
	fakeFFmpeg(t, `head -c 16000 /dev/zero
sleep 30`)
	out := make(chan []byte, 10)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Stream(ctx, "mic-dev", out) }()

	for i := 0; i < 2; i++ {
		select {
		case b := <-out:
			if len(b) != ChunkBytes {
				t.Fatalf("chunk %d has %d bytes, want %d", i, len(b), ChunkBytes)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("chunk %d never arrived", i)
		}
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancel is a normal stop, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stream did not stop ffmpeg after cancel")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("stopping should kill ffmpeg at once: there is nothing to finalize")
	}
	for range out {
	} // out must be closed, or this would block
}

func TestStreamReportsUnexpectedExit(t *testing.T) {
	fakeFFmpeg(t, `echo "pulse: No such device" >&2
exit 1`)
	out := make(chan []byte, 1)
	err := Stream(context.Background(), "nope", out)
	if err == nil || !strings.Contains(err.Error(), "No such device") {
		t.Fatalf("err = %v, want the ffmpeg error", err)
	}
	if _, open := <-out; open {
		t.Error("out must be closed")
	}
}

func TestStreamDropsOldestWhenFull(t *testing.T) {
	// 5 chunks: 0x01..0x05 repeated, then waits.
	fakeFFmpeg(t, `for b in 1 2 3 4 5; do head -c 6400 /dev/zero | tr '\0' "\\00$b"; done
sleep 30`)
	out := make(chan []byte, 2) // nobody reads until all 5 chunks are read
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Stream(ctx, "mic-dev", out) }()

	time.Sleep(300 * time.Millisecond)
	var got []byte
	for i := 0; i < 2; i++ {
		select {
		case b := <-out:
			got = append(got, b[0])
		case <-time.After(2 * time.Second):
			t.Fatal("missing chunk")
		}
	}
	if string(got) != "\x04\x05" {
		t.Errorf("kept chunks %v, want the newest (4, 5)", got)
	}
}
