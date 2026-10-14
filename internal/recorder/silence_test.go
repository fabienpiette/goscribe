package recorder

import (
	"strings"
	"testing"
	"time"
)

func TestReadEvents(t *testing.T) {
	// Captured from ffmpeg 8.0 ametadata=mode=print output.
	input := "frame:46   pts:47104   pts_time:0.981333\n" +
		"lavfi.silence_start=12.5\n" +
		"frame:900  pts:921600  pts_time:19.2\n" +
		"lavfi.silence_end=44.1\n" +
		"lavfi.silence_duration=31.6\n" +
		"lavfi.silence_start=garbage\n"
	out := make(chan Event, 10)
	ReadEvents(strings.NewReader(input), "mic", out)
	close(out)

	var got []Event
	for e := range out {
		got = append(got, e)
	}
	if len(got) != 2 {
		t.Fatalf("events = %+v, want start and end only", got)
	}
	if got[0] != (Event{Source: "mic", Silent: true, At: 12.5}) || got[1] != (Event{Source: "mic", Silent: false, At: 44.1}) {
		t.Errorf("events = %+v", got)
	}
}

func TestEventMessage(t *testing.T) {
	if m := (Event{Source: "mic", Silent: true}).Message(30 * time.Second); m != "⚠ no sound from mic for 30s (muted? wrong device?)" {
		t.Errorf("silent message = %q", m)
	}
	if m := (Event{Source: "system"}).Message(30 * time.Second); m != "✓ system sound is back" {
		t.Errorf("back message = %q", m)
	}
}
