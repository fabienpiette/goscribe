package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goscribe/internal/recorder"
)

// fakeAudio stubs pactl and ffmpeg. The fake ffmpeg writes a few bytes and
// exits as if -duration ran out.
func fakeAudio(t *testing.T) {
	t.Helper()
	origRunner, origBin := recorder.Runner, recorder.Binary
	recorder.Runner = func(args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "list short sources":
			return "1\tsink.monitor\tPipeWire\ts32le\tIDLE\n2\tusb-mic\tPipeWire\ts16le\tRUNNING\n3\tlaptop-mic\tPipeWire\ts16le\tIDLE\n", nil
		case "get-default-source":
			return "usb-mic\n", nil
		case "get-default-sink":
			return "sink\n", nil
		}
		return "", nil
	}
	bin := filepath.Join(t.TempDir(), "fake-ffmpeg")
	script := "#!/bin/sh\nfor a; do out=\"$a\"; done\necho \"$@\" > \"$out.args\"\nprintf 'ID3audio' > \"$out\"\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	recorder.Binary = bin
	t.Cleanup(func() { recorder.Runner, recorder.Binary = origRunner, origBin })
}

func record(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runRecord(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestRecordingName(t *testing.T) {
	got := recordingName(time.Date(2026, 10, 14, 14, 30, 5, 0, time.Local))
	if got != "recording-2026-10-14_14-30-05.mp3" {
		t.Errorf("recordingName = %q", got)
	}
}

func TestRecordCommand(t *testing.T) {
	fakeAudio(t)
	t.Setenv("HOME", t.TempDir())
	outFile := filepath.Join(t.TempDir(), "my rec.mp3")

	stdout, stderr, code := record(t, "-o", outFile, "-duration", "1m", "-quality", "music")
	if code != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Saved "+outFile) {
		t.Errorf("stdout = %s", stdout)
	}
	args, _ := os.ReadFile(outFile + ".args")
	for _, want := range []string{"-i usb-mic", "-i sink.monitor", "-t 60", "-q:a 2"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("ffmpeg args missing %q: %s", want, args)
		}
	}

	// Never overwrite.
	if _, stderr, code := record(t, "-o", outFile); code != 2 || !strings.Contains(stderr, "already exists") {
		t.Errorf("overwrite: exit = %d, stderr %q", code, stderr)
	}
}

func TestRecordConfigDefaults(t *testing.T) {
	fakeAudio(t)
	t.Setenv("HOME", t.TempDir())
	cfg := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(cfg, []byte("record_source: mic\nrecord_mic: laptop-mic\n"), 0644); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(t.TempDir(), "c.mp3")

	if _, stderr, code := record(t, "-config", cfg, "-o", outFile); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	args, _ := os.ReadFile(outFile + ".args")
	if !strings.Contains(string(args), "-i laptop-mic") || strings.Contains(string(args), "sink.monitor") {
		t.Errorf("config defaults not applied: %s", args)
	}

	// A flag beats the config.
	outFile2 := filepath.Join(t.TempDir(), "d.mp3")
	if _, stderr, code := record(t, "-config", cfg, "-source", "system", "-o", outFile2); code != 0 {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	args, _ = os.ReadFile(outFile2 + ".args")
	if strings.Contains(string(args), "laptop-mic") || !strings.Contains(string(args), "-i sink.monitor") {
		t.Errorf("-source flag should beat config: %s", args)
	}
}

func TestRecordListSources(t *testing.T) {
	fakeAudio(t)
	stdout, _, code := record(t, "-list-sources")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"Inputs (mic):", "* usb-mic", "  laptop-mic", "Computer sound (monitors):", "* sink.monitor"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("-list-sources missing %q:\n%s", want, stdout)
		}
	}
}

func TestRecordUsageErrors(t *testing.T) {
	fakeAudio(t)
	t.Setenv("HOME", t.TempDir())
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-source", "loud"}, "invalid source"},
		{[]string{"-mic", "sink.monitor"}, "is computer sound"},
		{[]string{"-duration", "0s"}, "-duration must be positive"},                            // Review Focus 3
		{[]string{"-duration", "-5m"}, "-duration must be positive"},                           // Review Focus 3
		{[]string{"-o", filepath.Join(t.TempDir(), "nope", "x.mp3")}, "folder does not exist"}, // Review Focus 2
		{[]string{"stray-arg"}, "unexpected argument"},
	}
	for _, c := range cases {
		if _, stderr, code := record(t, c.args...); code != 2 || !strings.Contains(stderr, c.want) {
			t.Errorf("record %v: exit = %d, stderr %q, want %q", c.args, code, stderr, c.want)
		}
	}
}

func TestRecordTranscribe(t *testing.T) {
	fakeAudio(t)
	setupTranscriberTest(t) // HOME with a default config + OpenAI stub
	fakeWhisper(t)
	outFile := filepath.Join(t.TempDir(), "meeting-2026-10-14.mp3")

	stdout, stderr, code := record(t, "-o", outFile, "-duration", "10s", "-transcribe", "-transcriber", "local")
	if code != 0 {
		t.Fatalf("exit = %d\n%s\n%s", code, stdout, stderr)
	}
	transcript, err := os.ReadFile(strings.TrimSuffix(outFile, ".mp3") + "-transcript.txt")
	if err != nil || string(transcript) != "local words" {
		t.Errorf("transcript = %q, err = %v", transcript, err)
	}
}
