package application

import (
	"sync"
	"time"
)

const (
	backoffInitial = 1 * time.Second
	backoffMax     = 30 * time.Second
)

// Package-level vars (not consts) so tests can shrink them; production
// code never reassigns these.
var (
	stabilityWindow  = 30 * time.Second
	stabilityPollGap = 2 * time.Second
)

// backoffState implements the exponential restart backoff from section
// 25: 1s, 2s, 4s, 8s, ... capped at backoffMax, reset once a service has
// stayed running for stabilityWindow.
type backoffState struct {
	mu    sync.Mutex
	delay time.Duration
}

func newBackoff() *backoffState {
	return &backoffState{delay: backoffInitial / 2} // so first next() returns backoffInitial
}

func (b *backoffState) next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.delay *= 2
	if b.delay > backoffMax {
		b.delay = backoffMax
	}
	if b.delay < backoffInitial {
		b.delay = backoffInitial
	}
	return b.delay
}

func (b *backoffState) reset() {
	b.mu.Lock()
	b.delay = backoffInitial / 2
	b.mu.Unlock()
}

// watchStability polls isRunning and resets the backoff once the
// service has been continuously running for stabilityWindow.
func (b *backoffState) watchStability(isRunning func() bool) {
	elapsed := time.Duration(0)
	for elapsed < stabilityWindow {
		time.Sleep(stabilityPollGap)
		if !isRunning() {
			return
		}
		elapsed += stabilityPollGap
	}
	b.reset()
}
