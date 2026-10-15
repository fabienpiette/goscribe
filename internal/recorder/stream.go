package recorder

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// StreamRate is the sample rate of Stream's output: 16 kHz mono s16le, what
// realtime speech-to-text expects.
const StreamRate = 16000

// ChunkBytes is the size of each chunk Stream sends: 200 ms of audio.
const ChunkBytes = StreamRate * 2 / 5

// StreamArgs builds the ffmpeg command line (without the binary) that captures
// one pulse device as raw PCM on stdout. A small fragment size keeps capture
// latency low.
func StreamArgs(device string) []string {
	return []string{
		"-hide_banner", "-nostats", "-loglevel", "error",
		"-f", "pulse", "-thread_queue_size", "1024", "-fragment_size", "3200", "-i", device,
		"-ac", "1", "-ar", fmt.Sprint(StreamRate), "-f", "s16le", "-",
	}
}

// Stream captures device and sends ChunkBytes chunks of PCM on out until ctx
// is cancelled (a normal stop: it returns nil) or ffmpeg exits on its own (an
// error with ffmpeg's last messages). It never blocks on out: when out is
// full, it drops the oldest chunk, so a slow consumer loses old audio rather
// than stalling the capture. out is closed when Stream returns.
func Stream(ctx context.Context, device string, out chan []byte) error {
	defer close(out)
	// ffmpeg's Pdeathsig (Linux) is tied to the thread that starts it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	bin, err := exec.LookPath(Binary)
	if err != nil {
		return fmt.Errorf("%s not found on PATH", Binary)
	}
	cmd := exec.Command(bin, StreamArgs(device)...)
	isolate(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tail{max: 20}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", Binary, err)
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			b := make([]byte, ChunkBytes)
			if _, err := io.ReadFull(stdout, b); err != nil {
				return
			}
			for {
				select {
				case out <- b:
				default:
					select {
					case <-out: // drop the oldest chunk and retry
					default:
					}
					continue
				}
				break
			}
		}
	}()

	select {
	case <-ctx.Done():
		// Raw PCM has nothing to finalize: stop ffmpeg at once.
		kill(cmd)
		<-readDone
		_ = cmd.Wait()
		return nil
	case <-readDone:
		err := cmd.Wait()
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("ended")
		}
		return fmt.Errorf("%s stopped capturing %s: %v\n%s", Binary, device, err, stderr.String())
	}
}
