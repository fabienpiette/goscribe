package recorder

import (
	"strings"
	"testing"
)

const pactlSources = "60\talsa_output.pci-0000_00_1f.3.analog-stereo.monitor\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" +
	"61\talsa_input.pci-0000_00_1f.3.analog-stereo\tPipeWire\ts32le 2ch 48000Hz\tSUSPENDED\n" +
	"62\talsa_input.usb-046d_HD_Pro_Webcam_C920-02.analog-stereo\tPipeWire\ts16le 2ch 32000Hz\tRUNNING\n"

// fakePactl answers pactl calls from a map keyed by the joined args.
func fakePactl(t *testing.T, answers map[string]string) {
	t.Helper()
	orig := Runner
	Runner = func(args ...string) (string, error) {
		return answers[strings.Join(args, " ")], nil
	}
	t.Cleanup(func() { Runner = orig })
}

func defaultAnswers() map[string]string {
	return map[string]string{
		"list short sources": pactlSources,
		"get-default-source": "alsa_input.usb-046d_HD_Pro_Webcam_C920-02.analog-stereo\n",
		"get-default-sink":   "alsa_output.pci-0000_00_1f.3.analog-stereo\n",
	}
}

func TestListSources(t *testing.T) {
	fakePactl(t, defaultAnswers())
	got, err := ListSources()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || !got[0].Monitor || got[1].Monitor || got[2].Name != "alsa_input.usb-046d_HD_Pro_Webcam_C920-02.analog-stereo" {
		t.Errorf("sources = %+v", got)
	}
}

func TestResolveDefaults(t *testing.T) {
	fakePactl(t, defaultAnswers())
	in, err := Resolve("both", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Mic != "alsa_input.usb-046d_HD_Pro_Webcam_C920-02.analog-stereo" ||
		in.System != "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor" {
		t.Errorf("inputs = %+v", in)
	}

	in, _ = Resolve("mic", "alsa_input.pci-0000_00_1f.3.analog-stereo", "")
	if in.Mic != "alsa_input.pci-0000_00_1f.3.analog-stereo" || in.System != "" {
		t.Errorf("mic only = %+v", in)
	}
	in, _ = Resolve("system", "", "")
	if in.Mic != "" || in.System == "" {
		t.Errorf("system only = %+v", in)
	}
}

func TestResolveErrors(t *testing.T) {
	fakePactl(t, defaultAnswers())
	cases := []struct {
		source, mic, system, want string
	}{
		{"loud", "", "", "invalid source"},
		{"mic", "nope", "", "unknown input"},
		{"mic", "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor", "", "is computer sound"},
		{"system", "", "alsa_input.pci-0000_00_1f.3.analog-stereo", "is a mic"},
	}
	for _, c := range cases {
		_, err := Resolve(c.source, c.mic, c.system)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Resolve(%q, %q, %q) error = %v, want %q", c.source, c.mic, c.system, err, c.want)
		}
	}
}

// Review Focus 1: no default device must be a clear error.
func TestResolveNoDefaultMic(t *testing.T) {
	answers := defaultAnswers()
	answers["get-default-source"] = "\n"
	fakePactl(t, answers)
	_, err := Resolve("mic", "", "")
	if err == nil || !strings.Contains(err.Error(), "no default input") {
		t.Errorf("error = %v, want no default input", err)
	}
}
