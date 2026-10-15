package main

import (
	"unicode"

	"goscribe/internal/recorder"
)

// keyLabel maps a key press to the side it mutes, ignoring case.
func keyLabel(b byte) (string, bool) {
	label, ok := recorder.MuteKeys[unicode.ToLower(rune(b))]
	return label, ok
}
