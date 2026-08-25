package application

import (
	"slices"
	"time"

	"github.com/abtinokhovat/godev/internal/ports"
)

// portPollInterval is how often a running service's listening ports
// are re-checked. Infrequent enough not to be its own source of load
// (see Supervisor.buildSem's concern about background work piling
// up), frequent enough that a port shows up shortly after the service
// actually binds it - an app can take a moment to get there, so this
// polls repeatedly rather than checking once right after start.
const portPollInterval = 2 * time.Second

// pollPorts checks pid's listening ports (see internal/ports) once
// immediately, then on portPollInterval, updating e.runtime.Ports and
// publishing EventPortsChanged whenever the observed set changes. It
// stops as soon as either done closes (the process exited) or gen no
// longer matches e.generation (superseded by a newer start/restart) -
// whichever comes first, so it never outlives the process it's
// watching or a stale one it no longer belongs to.
func (s *Supervisor) pollPorts(e *serviceEntry, name string, pid, gen int, done <-chan struct{}) {
	if !s.checkPortsOnce(e, name, pid, gen) {
		return
	}
	ticker := time.NewTicker(portPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if !s.checkPortsOnce(e, name, pid, gen) {
				return
			}
		}
	}
}

// checkPortsOnce reports false when this poller is stale and should
// stop - checked explicitly rather than relying solely on `done`
// closing, since a service can be stopped and immediately restarted
// (bumping the generation) faster than this goroutine's next tick.
func (s *Supervisor) checkPortsOnce(e *serviceEntry, name string, pid, gen int) bool {
	s.mu.RLock()
	stale := e.generation != gen
	s.mu.RUnlock()
	if stale {
		return false
	}

	found, err := ports.ForPID(pid)
	if err != nil {
		return true // best-effort: try again next tick
	}

	s.mu.Lock()
	changed := !slices.Equal(e.runtime.Ports, found)
	if changed {
		e.runtime.Ports = found
	}
	s.mu.Unlock()
	if changed {
		s.events.Publish(Event{Type: EventPortsChanged, Service: name})
	}
	return true
}
