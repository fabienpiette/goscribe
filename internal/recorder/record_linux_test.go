//go:build linux

package recorder

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRecordHelperProcess is not a real test: TestFFmpegDiesWithGoscribe runs
// the test binary again with GOSCRIBE_RECORD_HELPER=1 to get a separate
// "goscribe" process it can kill.
func TestRecordHelperProcess(t *testing.T) {
	if os.Getenv("GOSCRIBE_RECORD_HELPER") != "1" {
		t.Skip("helper process for TestFFmpegDiesWithGoscribe")
	}
	Binary = os.Getenv("FAKE_FFMPEG")
	_, _ = Record(context.Background(), Config{Inputs: Inputs{Mic: "mic"}, Quality: "voice", Output: os.Getenv("OUT")}, UI{Status: io.Discard})
	os.Exit(0)
}

// If goscribe dies without its stop path (terminal closed, SIGKILL, panic),
// ffmpeg must not keep recording the mic in the background.
func TestFFmpegDiesWithGoscribe(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ffmpeg")
	script := "#!/bin/sh\nfor a; do out=\"$a\"; done\necho $$ > \"$out.pid\"\nexec sleep 30\n"
	if err := os.WriteFile(fake, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "rec.mp3")

	helper := exec.Command(os.Args[0], "-test.run=^TestRecordHelperProcess$")
	helper.Env = append(os.Environ(), "GOSCRIBE_RECORD_HELPER=1", "FAKE_FFMPEG="+fake, "OUT="+out)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}

	var pid int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err := os.ReadFile(out + ".pid"); err == nil && strings.TrimSpace(string(data)) != "" {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			break
		}
	}
	if pid == 0 {
		_ = helper.Process.Kill()
		t.Fatal("fake ffmpeg never started")
	}

	_ = helper.Process.Kill()
	_ = helper.Wait()

	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return // ffmpeg is gone
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Error("ffmpeg kept running after goscribe was killed")
}
