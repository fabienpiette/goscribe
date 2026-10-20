//go:build !linux

package main

import "os"

// startMuteKeys is Linux only, like recording itself (PipeWire/PulseAudio).
func startMuteKeys(in *os.File) (toggle <-chan string, restore func()) {
	return nil, func() {}
}

// startKeys is Linux only, like startMuteKeys.
func startKeys(in *os.File, keys map[rune]string) (toggle <-chan string, restore func()) {
	return nil, func() {}
}
