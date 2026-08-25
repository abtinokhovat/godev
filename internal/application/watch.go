package application

import (
	"path/filepath"
	"time"

	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/watcher"
)

// WatchAndReload starts a project-wide filesystem watcher and, for
// every debounced change batch, rebuilds+restarts only the Go
// services the change could actually affect (see depindex.go) -
// everyone else, running or not, is left untouched.
//
// It runs until the supervisor's watcher is closed; call the returned
// stop func on shutdown.
func (s *Supervisor) WatchAndReload(debounceMs int) (func(), error) {
	w, err := watcher.New(s.ProjectRoot, time.Duration(debounceMs)*time.Millisecond)
	if err != nil {
		return nil, err
	}
	s.SetWatchActive(true)
	go s.rebuildDepIndex() // best-effort warm-up; a change arriving before this finishes just reloads everyone this once

	go func() {
		for change := range w.Changes() {
			s.events.Publish(Event{Type: EventFileChanged,
				Message: firstOrCount(change.Paths)})
			s.reloadHotReloadServices(change.Paths)
			// Refresh in the background so the next change's scoping
			// reflects any import changes from this one - go list is
			// cheap relative to the builds it's meant to save.
			go s.rebuildDepIndex()
		}
	}()

	return func() { s.SetWatchActive(false); w.Close() }, nil
}

// reloadHotReloadServices restarts every hot-reload-enabled Go service
// whose build actually depends on one of paths - or every one of them
// if the index isn't ready yet, or a path is go.mod/go.sum (a
// module-level change can affect anything, so it's not worth trying
// to scope). Command-based services never rebuild from Go source
// changes at all: they have no Go package to depend on anything.
func (s *Supervisor) reloadHotReloadServices(paths []string) {
	affected, scoped := s.affectedByPaths(paths)

	for _, name := range s.order {
		e, ok := s.entry(name)
		if !ok || !e.svc.HotReload || e.svc.IsCommand() {
			continue
		}
		if scoped && !affected[name] {
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

// affectedByPaths resolves paths to the set of service names whose
// dependency graph includes at least one of them. scoped is false
// (meaning "affects everyone") when the index isn't ready yet, or any
// path is go.mod/go.sum.
func (s *Supervisor) affectedByPaths(paths []string) (affected map[string]bool, scoped bool) {
	affected = map[string]bool{}
	for _, p := range paths {
		base := filepath.Base(p)
		if base == "go.mod" || base == "go.sum" {
			return nil, false
		}
		names, ok := s.deps.servicesForDir(filepath.Dir(p))
		if !ok {
			return nil, false
		}
		for _, n := range names {
			affected[n] = true
		}
	}
	return affected, true
}

func firstOrCount(paths []string) string {
	if len(paths) == 1 {
		return paths[0] + " changed"
	}
	return "multiple files changed"
}
