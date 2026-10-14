//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// guardSignals keeps the test process alive when it signals itself.
func guardSignals(t *testing.T) chan os.Signal {
	t.Helper()
	guard := make(chan os.Signal, 8)
	signal.Notify(guard, syscall.SIGINT, syscall.SIGHUP)
	t.Cleanup(func() { signal.Stop(guard) })
	return guard
}

func cancelled(ctxDone <-chan struct{}) bool {
	select {
	case <-ctxDone:
		return true
	case <-time.After(500 * time.Millisecond):
		return false
	}
}

// Closing the terminal (SIGHUP) must stop the recording cleanly, not leave
// goscribe dead and the recording unfinished.
func TestWatchSignalsHangupStops(t *testing.T) {
	guardSignals(t)
	ctx, _, stop := watchSignals()
	defer stop()

	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	if !cancelled(ctx.Done()) {
		t.Error("SIGHUP did not stop the recording")
	}
}

// Once the recording is over (e.g. during -transcribe), Ctrl+C must go back to
// its default behavior instead of being swallowed by the recorder.
func TestWatchSignalsStopReleasesCtrlC(t *testing.T) {
	guard := guardSignals(t)
	_, force, stop := watchSignals()
	stop()

	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case <-guard:
	case <-time.After(time.Second):
		t.Fatal("SIGINT never arrived")
	}
	select {
	case <-force:
		t.Error("recorder still reacted to Ctrl+C after stop")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestWatchSignalsTwoStageCtrlC(t *testing.T) {
	guardSignals(t)
	ctx, force, stop := watchSignals()
	defer stop()

	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	if !cancelled(ctx.Done()) {
		t.Fatal("first Ctrl+C did not stop")
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	select {
	case <-force:
	case <-time.After(time.Second):
		t.Error("second Ctrl+C did not force")
	}
}
