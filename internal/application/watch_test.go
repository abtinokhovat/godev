package application

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/domain"
)

// collectSequentialRestarts subscribes to sup's events and returns
// the order services actually finished restarting in (first reaching
// Running or Crashed) - not just finished building - so the caller
// can safely stop listening once it returns: the background goroutine
// driving the restarts is guaranteed to have moved past every one of
// them by then, unlike if this returned as soon as builds alone
// finished, while a process launch might still be in flight and about
// to publish into a channel the test has already unsubscribed from.
// Only the *first* Started/Crashed per service counts: a service that
// already completed can still occasionally report a second, spurious
// crash from an old monitor goroutine racing a fast, immediately-
// following restart of the same service (a pre-existing, narrow
// timing edge unrelated to what this test actually checks) - without
// this dedup, that stray event could be mistaken for a *different*
// service's completion and both corrupt the reported order and, if it
// arrives after this function has already returned, race the
// subscription's own teardown. Along the way it fails the test the
// instant a second service's EventBuildStarted arrives while an
// earlier one's build hasn't finished yet - that overlap is exactly
// what sequential rebuilding must never allow.
func collectSequentialRestarts(t *testing.T, eventsCh <-chan Event, want int, timeout time.Duration) []string {
	t.Helper()
	var order []string
	seen := map[string]bool{}
	buildPending := map[string]bool{}
	deadline := time.Now().Add(timeout)
	for len(order) < want {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for %d restarts; got order=%v", want, order)
		}
		select {
		case e := <-eventsCh:
			switch e.Type {
			case EventBuildStarted:
				if len(buildPending) > 0 {
					t.Fatalf("service %q started building while another service's build was still in flight (pending=%v) - rebuilds must be sequential, not overlapping", e.Service, buildPending)
				}
				buildPending[e.Service] = true
			case EventBuildSucceeded, EventBuildFailed:
				delete(buildPending, e.Service)
			case EventServiceStarted, EventServiceCrashed:
				if !seen[e.Service] {
					seen[e.Service] = true
					order = append(order, e.Service)
				}
			}
		case <-time.After(remaining):
			t.Fatalf("timed out waiting for %d restarts; got order=%v", want, order)
		}
	}
	return order
}

func TestReloadHotReloadServicesRestartsOneAtATime(t *testing.T) {
	root, api, worker := writeDepFixture(t)
	// worker declared first, to prove build order follows registration
	// order rather than, say, alphabetical or map order.
	sup, err := NewSupervisor(root, []domain.Service{worker, api})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	for _, name := range []string{"worker", "api"} {
		if err := sup.Start(name); err != nil {
			t.Fatalf("Start(%s): %v", name, err)
		}
		waitForState(t, sup, name, domain.StateRunning, 10*time.Second)
	}

	eventsCh, cancel := sup.SubscribeEvents(64)
	defer cancel()

	// Deliberately not calling rebuildDepIndex: an unready dep index
	// makes affectedByPaths treat the change as affecting everyone,
	// which is exactly what's needed here - both hot-reload services
	// must be candidates for this one reload.
	sup.reloadHotReloadServices([]string{filepath.Join(root, "cmd", "worker", "main.go")})

	order := collectSequentialRestarts(t, eventsCh, 2, 15*time.Second)
	want := []string{"worker", "api"}
	for i, n := range want {
		if order[i] != n {
			t.Errorf("build order[%d] = %q, want %q (registration order): %v", i, order[i], n, order)
		}
	}
}
