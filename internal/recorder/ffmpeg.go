package recorder

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultSilenceAfter is how long a source must be silent before a warning.
const DefaultSilenceAfter = 30 * time.Second

// MuteKeys maps the keys that mute/unmute a side during a recording to its label.
var MuteKeys = map[rune]string{'m': "mic", 's': "system"}

// MuteHint lists the mute keys of the recorded sides, e.g. "m/s".
func (c Config) MuteHint() string {
	var keys []string
	for _, l := range c.Labels() {
		for k, kl := range MuteKeys {
			if kl == l {
				keys = append(keys, string(k))
			}
		}
	}
	return strings.Join(keys, "/")
}

type Config struct {
	Inputs       Inputs
	Quality      string        // "voice" or "music"
	Duration     time.Duration // 0 = until stopped
	Output       string
	SilenceAfter time.Duration // 0 = DefaultSilenceAfter
}

// Labels returns the recorded sides in ffmpeg input order. The silence events
// of label i are written to fd 3+i.
func (c Config) Labels() []string {
	var labels []string
	if c.Inputs.Mic != "" {
		labels = append(labels, "mic")
	}
	if c.Inputs.System != "" {
		labels = append(labels, "system")
	}
	return labels
}

func (c Config) Device(label string) string {
	if label == "mic" {
		return c.Inputs.Mic
	}
	return c.Inputs.System
}

func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// Args builds the ffmpeg command line (without the binary).
func Args(c Config) ([]string, error) {
	labels := c.Labels()
	if len(labels) == 0 {
		return nil, fmt.Errorf("nothing to record: no input selected")
	}
	var quality []string
	switch c.Quality {
	case "voice":
		quality = []string{"-ac", "1", "-c:a", "libmp3lame", "-b:a", "64k"}
	case "music":
		quality = []string{"-ac", "2", "-c:a", "libmp3lame", "-q:a", "2"}
	default:
		return nil, fmt.Errorf("invalid quality %q (use voice or music)", c.Quality)
	}
	silence := c.SilenceAfter
	if silence <= 0 {
		silence = DefaultSilenceAfter
	}

	args := []string{"-hide_banner", "-nostats", "-loglevel", "error"}
	for _, l := range labels {
		args = append(args, "-f", "pulse", "-thread_queue_size", "1024", "-i", c.Device(l))
	}

	// volume@<label> comes after silencedetect, so muting a side never looks like
	// a dead source. Record mutes it with an ffmpeg command on stdin.
	chain := func(i int) string {
		return fmt.Sprintf("[%d:a]aresample=async=1000,silencedetect=n=-50dB:d=%s,ametadata=mode=print:file=/dev/fd/%d,volume@%s=volume=1",
			i, seconds(silence), 3+i, labels[i])
	}
	graph := chain(0)
	if len(labels) == 2 {
		graph = chain(0) + "[a0];" + chain(1) + "[a1];[a0][a1]amix=inputs=2:duration=longest:normalize=0"
	}
	args = append(args, "-filter_complex", graph)

	if c.Duration > 0 {
		args = append(args, "-t", seconds(c.Duration))
	}
	args = append(args, quality...)
	// -n: never overwrite, even if the file appeared after our own check.
	args = append(args, "-n", c.Output)
	return args, nil
}

// String is a readable "mic+system" summary for the status line.
func (c Config) String() string {
	return strings.Join(c.Labels(), "+")
}
