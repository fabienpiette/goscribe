//go:build linux

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPty returns the controlling (master) and terminal (slave) ends of a new
// pseudo-terminal.
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	fd := int(master.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	return master, slave
}

func TestMuteKeysOnTerminal(t *testing.T) {
	master, slave := openPty(t)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}

	toggle, restore := startMuteKeys(slave)
	defer restore()
	if toggle == nil {
		t.Fatal("expected mute keys on a terminal")
	}
	during, _ := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if during.Lflag&(unix.ICANON|unix.ECHO) != 0 {
		t.Error("terminal should read single keys without echo while recording")
	}
	if during.Lflag&unix.ISIG == 0 {
		t.Error("Ctrl+C must still send a signal while recording")
	}

	// No Enter needed; other keys are ignored; case doesn't matter.
	if _, err := master.Write([]byte("xMs")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mic", "system"} {
		select {
		case got := <-toggle:
			if got != want {
				t.Errorf("toggle = %q, want %q", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no toggle for %q", want)
		}
	}

	start := time.Now()
	restore()
	restore() // safe to call twice
	if time.Since(start) > time.Second {
		t.Error("restore should stop the key reader quickly")
	}
	after, _ := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if after.Lflag != before.Lflag {
		t.Errorf("terminal not restored: lflag %#x, want %#x", after.Lflag, before.Lflag)
	}
}

func TestMuteKeysOffWithoutTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	toggle, restore := startMuteKeys(r)
	restore()
	if toggle != nil {
		t.Error("stdin that is not a terminal must not enable mute keys")
	}
}
