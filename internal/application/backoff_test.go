package application

import (
	"testing"
	"time"
)

func TestBackoffDoublesAndCaps(t *testing.T) {
	b := newBackoff()
	want := []time.Duration{1, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		got := b.next()
		if got != w*time.Second {
			t.Fatalf("next() #%d = %s, want %ds", i, got, w)
		}
	}
}

func TestBackoffResetRestartsFromInitial(t *testing.T) {
	b := newBackoff()
	b.next()
	b.next() // now at 2s
	b.reset()
	if got := b.next(); got != backoffInitial {
		t.Fatalf("after reset, next() = %s, want %s", got, backoffInitial)
	}
}

func TestWatchStabilityResetsAfterSustainedRun(t *testing.T) {
	origWindow, origGap := stabilityWindow, stabilityPollGap
	stabilityWindow = 20 * time.Millisecond
	stabilityPollGap = 5 * time.Millisecond
	defer func() { stabilityWindow, stabilityPollGap = origWindow, origGap }()

	b := &backoffState{delay: 8 * time.Second}
	isRunning := func() bool { return true }

	done := make(chan struct{})
	go func() {
		b.watchStability(isRunning)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchStability did not return in time")
	}
	if got := b.next(); got != backoffInitial {
		t.Fatalf("after stability window, next() = %s, want %s (reset)", got, backoffInitial)
	}
}
