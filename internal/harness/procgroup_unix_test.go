//go:build unix

package harness

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// With OwnProcessGroup (set by assist), a CLI runs in its own process group,
// out of reach of a terminal Ctrl+C.
func TestRunOwnProcessGroup(t *testing.T) {
	dir := fakeHarness(t, `ps -o pgid= -p $$ | tr -d ' ' > "$FAKE_DIR/pgid"; echo answer`, "")
	pgid := func() int {
		data, _ := os.ReadFile(dir + "/pgid")
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return n
	}
	if _, err := Run("claude", "x"); err != nil {
		t.Fatal(err)
	}
	if pgid() != syscall.Getpgrp() {
		t.Errorf("by default the CLI shares goscribe's process group")
	}
	OwnProcessGroup = true
	t.Cleanup(func() { OwnProcessGroup = false })
	if _, err := Run("claude", "x"); err != nil {
		t.Fatal(err)
	}
	if g := pgid(); g == 0 || g == syscall.Getpgrp() {
		t.Errorf("process group = %d, want its own", g)
	}
}
