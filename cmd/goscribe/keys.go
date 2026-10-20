package main

import (
	"maps"
	"unicode"

	"goscribe/internal/recorder"
)

// recapKey asks assist for a recap of the last minutes ("what did I miss").
const recapKey = 'w'

// keyLabel maps a key press to its label in keys, ignoring case.
func keyLabel(keys map[rune]string, b byte) (string, bool) {
	label, ok := keys[unicode.ToLower(rune(b))]
	return label, ok
}

// assistKeys are the mute keys of record plus the recap key.
func assistKeys() map[rune]string {
	keys := maps.Clone(recorder.MuteKeys)
	keys[recapKey] = "recap"
	return keys
}
