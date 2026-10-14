package recorder

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFFmpeg installs a shell script as the ffmpeg binary. The script gets the
// real ffmpeg args; "$out" is set to the last arg (the output file).
func fakeFFmpeg(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-ffmpeg")
	script := "#!/bin/sh\nfor a; do out=\"$a\"; done\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	origBin, origStop, origTick := Binary, StopTimeout, Tick
	Binary, StopTimeout, Tick = path, 300*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { Binary, StopTimeout, Tick = origBin, origStop, origTick })
}

func testConfig(t *testing.T) Config {
	return Config{
		Inputs:  Inputs{Mic: "mic-dev", System: "sink.monitor"},
		Quality: "voice",
		Output:  filepath.Join(t.TempDir(), "rec.mp3"),
	}
}

func TestRecordGracefulStop(t *testing.T) {
	// Writes audio, reports silence on the mic (fd 3), then waits for "q" on stdin.
	fakeFFmpeg(t, `printf 'ID3audio' > "$out"
echo "lavfi.silence_start=0" >&3
read -r line
exit 0`)
	c := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)

	var status bytes.Buffer
	res, err := Record(ctx, c, UI{Status: &status})
	if err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, status.String())
	}
	if !res.Stopped || res.Size != int64(len("ID3audio")) || res.Output != c.Output {
		t.Errorf("result = %+v", res)
	}
	if !strings.Contains(status.String(), "⚠ no sound from mic for 30s") {
		t.Errorf("status missing silence warning:\n%s", status.String())
	}
}

func TestRecordKillsWhenQIgnored(t *testing.T) {
	fakeFFmpeg(t, `printf 'x' > "$out"
trap '' INT TERM
sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	res, err := Record(ctx, testConfig(t), UI{Status: &bytes.Buffer{}})
	if time.Since(start) > 5*time.Second {
		t.Fatal("Record did not kill ffmpeg after StopTimeout")
	}
	if err != nil || !res.Stopped || res.Size != 1 {
		t.Errorf("killed after timeout should keep the file: res=%+v err=%v", res, err)
	}
}

func TestRecordForceKill(t *testing.T) {
	fakeFFmpeg(t, `printf 'x' > "$out"
trap '' INT TERM
sleep 30`)
	StopTimeout = time.Hour // only Force can end it quickly
	ctx, cancel := context.WithCancel(context.Background())
	force := make(chan struct{})
	time.AfterFunc(100*time.Millisecond, cancel)
	time.AfterFunc(200*time.Millisecond, func() { close(force) })

	start := time.Now()
	if _, err := Record(ctx, testConfig(t), UI{Status: &bytes.Buffer{}, Force: force}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("Force did not kill ffmpeg")
	}
}

func TestRecordFailsAtStart(t *testing.T) {
	fakeFFmpeg(t, `echo "pulse: Connection refused" >&2
exit 1`)
	c := testConfig(t)
	_, err := Record(context.Background(), c, UI{Status: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("error = %v, want ffmpeg stderr tail", err)
	}
	if _, statErr := os.Stat(c.Output); statErr == nil {
		t.Error("no file should be left behind")
	}
}

func TestRecordFailureKeepsPartialFile(t *testing.T) {
	fakeFFmpeg(t, `printf 'ID3partial' > "$out"
echo "device unplugged" >&2
exit 1`)
	c := testConfig(t)
	res, err := Record(context.Background(), c, UI{Status: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "device unplugged") {
		t.Errorf("error = %v", err)
	}
	if res.Size == 0 {
		t.Error("partial recording should be kept")
	}
	if _, statErr := os.Stat(c.Output); statErr != nil {
		t.Errorf("partial file missing: %v", statErr)
	}
}

func TestRecordDurationEndsOnItsOwn(t *testing.T) {
	fakeFFmpeg(t, `printf 'ID3done' > "$out"
exit 0`)
	res, err := Record(context.Background(), testConfig(t), UI{Status: &bytes.Buffer{}})
	if err != nil || res.Stopped || res.Size == 0 {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

// Review Focus 4: stopped before any audio was written.
func TestRecordNothingRecorded(t *testing.T) {
	fakeFFmpeg(t, `: > "$out"
read -r line
exit 0`)
	c := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := Record(ctx, c, UI{Status: &bytes.Buffer{}})
	if !errors.Is(err, ErrNothingRecorded) {
		t.Errorf("error = %v, want ErrNothingRecorded", err)
	}
	if _, statErr := os.Stat(c.Output); statErr == nil {
		t.Error("empty file should be removed")
	}
}

// Review Focus 5: long recordings.
func TestFormatting(t *testing.T) {
	if got := FormatElapsed(3725 * time.Second); got != "01:02:05" {
		t.Errorf("FormatElapsed = %q", got)
	}
	if got := FormatElapsed(9 * time.Second); got != "00:00:09" {
		t.Errorf("FormatElapsed = %q", got)
	}
	for n, want := range map[int64]string{0: "0.0 MB", 6186598: "5.9 MB", 1288490189: "1.2 GB"} {
		if got := FormatSize(n); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", n, got, want)
		}
	}
}
