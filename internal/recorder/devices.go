// Package recorder records the mic and/or computer sound to an mp3 with ffmpeg
// on PipeWire/PulseAudio.
package recorder

import (
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs pactl and returns its stdout. Overridable for tests.
var Runner = func(args ...string) (string, error) {
	out, err := exec.Command("pactl", args...).Output()
	if err != nil {
		return "", fmt.Errorf("pactl %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// Source is a PulseAudio source. Monitors carry what an output plays (computer sound).
type Source struct {
	Name    string
	Monitor bool
}

func ListSources() ([]Source, error) {
	out, err := Runner("list", "short", "sources")
	if err != nil {
		return nil, err
	}
	var sources []Source
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || fields[1] == "" {
			continue
		}
		sources = append(sources, Source{Name: fields[1], Monitor: strings.HasSuffix(fields[1], ".monitor")})
	}
	return sources, nil
}

func DefaultMic() (string, error) {
	out, err := Runner("get-default-source")
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(out)
	if name == "" || strings.HasSuffix(name, ".monitor") {
		return "", fmt.Errorf("no default input (mic) found; plug one in or pass -mic (see -list-sources)")
	}
	return name, nil
}

func DefaultMonitor() (string, error) {
	out, err := Runner("get-default-sink")
	if err != nil {
		return "", err
	}
	sink := strings.TrimSpace(out)
	if sink == "" {
		return "", fmt.Errorf("no default output found; pass -system (see -list-sources)")
	}
	return sink + ".monitor", nil
}

// Inputs are the resolved device names; an empty field means that side isn't recorded.
type Inputs struct {
	Mic    string
	System string
}

// Resolve picks the devices for source ("mic", "system" or "both"), using mic and
// system when given and the defaults otherwise, and checks each exists and has
// the right kind.
func Resolve(source, mic, system string) (Inputs, error) {
	if source != "mic" && source != "system" && source != "both" {
		return Inputs{}, fmt.Errorf("invalid source %q (use mic, system or both)", source)
	}
	sources, err := ListSources()
	if err != nil {
		return Inputs{}, err
	}
	find := func(name string) (Source, bool) {
		for _, s := range sources {
			if s.Name == name {
				return s, true
			}
		}
		return Source{}, false
	}

	var in Inputs
	if source != "system" {
		if mic == "" {
			if mic, err = DefaultMic(); err != nil {
				return Inputs{}, err
			}
		}
		s, ok := find(mic)
		if !ok {
			return Inputs{}, fmt.Errorf("unknown input %q (see -list-sources)", mic)
		}
		if s.Monitor {
			return Inputs{}, fmt.Errorf("%q is computer sound (a monitor), not a mic; use -system for it", mic)
		}
		in.Mic = mic
	}
	if source != "mic" {
		if system == "" {
			if system, err = DefaultMonitor(); err != nil {
				return Inputs{}, err
			}
		}
		s, ok := find(system)
		if !ok {
			return Inputs{}, fmt.Errorf("unknown computer sound source %q (see -list-sources)", system)
		}
		if !s.Monitor {
			return Inputs{}, fmt.Errorf("%q is a mic, not computer sound; use -mic for it", system)
		}
		in.System = system
	}
	return in, nil
}
