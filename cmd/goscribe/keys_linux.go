//go:build linux

package main

import (
	"os"
	"sync"

	"goscribe/internal/recorder"

	"golang.org/x/sys/unix"
)

// startMuteKeys reads the mute keys of record (recorder.MuteKeys).
func startMuteKeys(in *os.File) (toggle <-chan string, restore func()) {
	return startKeys(in, recorder.MuteKeys)
}

// startKeys reads single key presses from in and sends the label of each key
// in keys on toggle. The terminal leaves line mode and stops echoing, but
// keeps signals, so Ctrl+C still stops the command. restore puts the
// terminal back and stops reading; it is safe to call more than once. When in
// is not a terminal, toggle is nil.
func startKeys(in *os.File, keys map[rune]string) (toggle <-chan string, restore func()) {
	fd := int(in.Fd())
	orig, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, func() {}
	}
	raw := *orig
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, func() {}
	}

	out := make(chan string)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		readKeys(fd, keys, out, done)
	}()

	var once sync.Once
	return out, func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			_ = unix.IoctlSetTermios(fd, unix.TCSETS, orig)
		})
	}
}

// readKeys polls instead of blocking in read, so it notices done within
// 100 ms and never keeps a read pending on stdin after the recording.
func readKeys(fd int, keys map[rune]string, out chan<- string, done <-chan struct{}) {
	buf := make([]byte, 16)
	for {
		select {
		case <-done:
			return
		default:
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if err == unix.EINTR || n == 0 {
			continue
		}
		if err != nil || fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return
		}
		n, err = unix.Read(fd, buf)
		if err != nil || n == 0 {
			return
		}
		for _, b := range buf[:n] {
			label, ok := keyLabel(keys, b)
			if !ok {
				continue
			}
			select {
			case out <- label:
			case <-done:
				return
			}
		}
	}
}
