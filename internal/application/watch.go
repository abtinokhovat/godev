package application

import (
	"time"

	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/watcher"
)

// WatchAndReload starts a project-wide filesystem watcher and, for every
// debounced change batch, rebuilds+restarts every hot-reload-enabled
// service. This is the MVP-scope "any relevant Go file changes -> rebuild
// service" behavior from section 23 (per-service dependency graphs are
// explicitly a future optimization, not required for MVP).
//
// It runs until the supervisor's watcher is closed; call the returned
// stop func on shutdown.
func (s *Supervisor) WatchAndReload(debounceMs int) (func(), error) {
	w, err := watcher.New(s.ProjectRoot, time.Duration(debounceMs)*time.Millisecond)
	if err != nil {
		return nil, err
	}

	go func() {
		for change := range w.Changes() {
			s.events.Publish(Event{Type: EventFileChanged,
				Message: firstOrCount(change.Paths)})
			s.reloadHotReloadServices()
		}
	}()

	return func() { w.Close() }, nil
}

func (s *Supervisor) reloadHotReloadServices() {
	for _, name := range s.order {
		e, ok := s.entry(name)
		if !ok || !e.svc.HotReload {
			continue
		}
		s.mu.RLock()
		state := e.runtime.State
		s.mu.RUnlock()
		// Only reload services that are meant to be up; leave deliberately
		// stopped services alone.
		if state != domain.StateRunning && state != domain.StateCrashed && state != domain.StateBuildFailed {
			continue
		}
		go func(n string) {
			s.log(n, logs.StreamSystem, "source changed, reloading...")
			if err := s.Restart(n); err != nil {
				s.log(n, logs.StreamSystem, "reload failed: "+err.Error())
			}
		}(name)
	}
}

func firstOrCount(paths []string) string {
	if len(paths) == 1 {
		return paths[0] + " changed"
	}
	return "multiple files changed"
}
