package assistant

import (
	"fmt"
	"io"
	"strings"
)

// plainView writes the feed as lines and, on a terminal, keeps one status
// line under it. It has no dashboard: the notes file holds it.
type plainView struct {
	out         io.Writer
	interactive bool
	muteHint    string
	recapKey    string
	last        *Status // redrawn under each new line
}

// NewPlainView shows the feed on out; interactive adds the status line.
func NewPlainView(out io.Writer, interactive bool, muteHint, recapKey string) View {
	return &plainView{out: out, interactive: interactive, muteHint: muteHint, recapKey: recapKey}
}

func (v *plainView) Show(it Item) {
	v.clear()
	fmt.Fprintln(v.out, it.Plain)
	if v.last != nil {
		v.draw(*v.last)
	}
}

// Status redraws the status line. An empty State only clears it: the meeting
// is over, and the next line brings back the last status.
func (v *plainView) Status(st Status) {
	if !v.interactive {
		return
	}
	if st.State == "" {
		v.clear()
		return
	}
	v.last = &st
	v.draw(st)
}

func (v *plainView) Board(Board) {}

func (v *plainView) clear() {
	if v.interactive {
		fmt.Fprint(v.out, "\r\033[K")
	}
}

func (v *plainView) draw(st Status) {
	if !v.interactive {
		return
	}
	line := "● ASSIST " + clock(st.Elapsed)
	for _, side := range []string{"me", "others"} {
		if st.Muted[side] {
			line += "  " + side + " (muted)"
		}
	}
	if st.Partial != "" {
		line += fmt.Sprintf("  %s ▸ %q", st.PartialSpeaker, lastRunes(st.Partial, 60))
	}
	switch st.State {
	case "recapping", "thinking":
		line += fmt.Sprintf("  · %s %ds (%s)", st.State, int(st.Busy.Seconds()), st.Engine)
	default:
		line += "  · listening"
	}
	var keys []string
	if v.muteHint != "" {
		keys = append(keys, v.muteHint+": mute")
	}
	if v.recapKey != "" {
		keys = append(keys, v.recapKey+": recap")
	}
	hint := "   (Ctrl+C to stop)"
	if len(keys) > 0 {
		hint = "   (" + strings.Join(append(keys, "Ctrl+C: stop"), " · ") + ")"
	}
	fmt.Fprint(v.out, "\r\033[K"+line+hint)
}
