package recorder

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Event is a silence change on one recorded side.
type Event struct {
	Source string  // "mic" or "system"
	Silent bool    // true: silence started; false: sound is back
	At     float64 // seconds into the recording
}

// ReadEvents parses ffmpeg ametadata print lines (lavfi.silence_start=<s>,
// lavfi.silence_end=<s>) for one source until r ends.
func ReadEvents(r io.Reader, source string, out chan<- Event) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		at, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		switch key {
		case "lavfi.silence_start":
			out <- Event{Source: source, Silent: true, At: at}
		case "lavfi.silence_end":
			out <- Event{Source: source, Silent: false, At: at}
		}
	}
}

func (e Event) Message(after time.Duration) string {
	if e.Silent {
		return fmt.Sprintf("⚠ no sound from %s for %s (muted? wrong device?)", e.Source, after)
	}
	return fmt.Sprintf("✓ %s sound is back", e.Source)
}
