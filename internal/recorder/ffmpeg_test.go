package recorder

import (
	"strings"
	"testing"
	"time"
)

func TestArgsBoth(t *testing.T) {
	args, err := Args(Config{
		Inputs:  Inputs{Mic: "mic-dev", System: "sink.monitor"},
		Quality: "voice", Duration: 90 * time.Second, Output: "out file.mp3",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-f pulse -thread_queue_size 1024 -i mic-dev -f pulse -thread_queue_size 1024 -i sink.monitor",
		"[0:a]aresample=async=1000,silencedetect=n=-50dB:d=30,ametadata=mode=print:file=/dev/fd/3[a0]",
		"[1:a]aresample=async=1000,silencedetect=n=-50dB:d=30,ametadata=mode=print:file=/dev/fd/4[a1]",
		"[a0][a1]amix=inputs=2:duration=longest:normalize=0",
		"-t 90",
		"-ac 1 -c:a libmp3lame -b:a 64k",
		"-n",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	if args[len(args)-1] != "out file.mp3" {
		t.Errorf("output must be the last, unsplit arg: %q", args[len(args)-1])
	}
}

func TestArgsSingleSourceAndMusic(t *testing.T) {
	args, err := Args(Config{Inputs: Inputs{System: "sink.monitor"}, Quality: "music", Output: "o.mp3", SilenceAfter: 2 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "amix") || strings.Contains(joined, "-t ") {
		t.Errorf("single source without duration should have no amix and no -t:\n%s", joined)
	}
	for _, want := range []string{"-i sink.monitor", "silencedetect=n=-50dB:d=2,ametadata=mode=print:file=/dev/fd/3", "-ac 2 -c:a libmp3lame -q:a 2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	if got := (Config{Inputs: Inputs{System: "s"}}).Labels(); strings.Join(got, ",") != "system" {
		t.Errorf("labels = %v", got)
	}
}

func TestArgsErrors(t *testing.T) {
	if _, err := Args(Config{Quality: "voice", Output: "o.mp3"}); err == nil {
		t.Error("no inputs should fail")
	}
	if _, err := Args(Config{Inputs: Inputs{Mic: "m"}, Quality: "hifi", Output: "o.mp3"}); err == nil {
		t.Error("unknown quality should fail")
	}
}
