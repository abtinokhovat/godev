package application

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"runtime"
	"sync"

	"github.com/abtinokhovat/godev/internal/domain"
)

// depIndex maps each Go service to the set of directories its build
// depends on (including its own), so a changed file can be scoped to
// only the services it could actually affect instead of restarting
// every hot-reload service on every change - a shared package used by
// dozens of services still correctly restarts all of them, but an
// edit to one service's own file no longer touches any other.
//
// It starts empty/not-ready; until the first background computation
// finishes, callers must treat every change as affecting everyone
// (the safe default: restarting a service that didn't need it is
// wasted work, but failing to restart one that did is a real bug -
// silently stale code).
type depIndex struct {
	mu     sync.RWMutex
	ready  bool
	dirsOf map[string]map[string]struct{} // service name -> its dependency dirs
}

func newDepIndex() *depIndex {
	return &depIndex{dirsOf: map[string]map[string]struct{}{}}
}

// servicesForDir returns the names of every service whose dependency
// set includes dir. ok is false if the index hasn't been computed yet.
func (d *depIndex) servicesForDir(dir string) (names []string, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if !d.ready {
		return nil, false
	}
	for name, dirs := range d.dirsOf {
		if _, hit := dirs[dir]; hit {
			names = append(names, name)
		}
	}
	return names, true
}

func (d *depIndex) set(dirsOf map[string]map[string]struct{}) {
	d.mu.Lock()
	d.dirsOf = dirsOf
	d.ready = true
	d.mu.Unlock()
}

// rebuildDepIndex recomputes the dependency index from this
// Supervisor's current Go services and installs it. Safe to call
// repeatedly (e.g. after every watched change, to stay accurate as
// imports change) - each call only reads package metadata via `go
// list`, never builds anything.
func (s *Supervisor) rebuildDepIndex() {
	s.mu.RLock()
	services := make([]domain.Service, 0, len(s.entries))
	for _, name := range s.order {
		services = append(services, s.entries[name].svc)
	}
	s.mu.RUnlock()
	s.deps.set(computeDepIndex(s.ProjectRoot, services))
}

// computeDepIndex runs `go list -json -deps <pkg>` for every Go
// (buildable) service concurrently, bounded to GOMAXPROCS since this
// runs on every file-change batch and shouldn't itself compete with
// builds for the whole machine. Command-based services are skipped -
// they have no Go package to depend on anything.
func computeDepIndex(projectRoot string, services []domain.Service) map[string]map[string]struct{} {
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := make(map[string]map[string]struct{}, len(services))

	for _, svc := range services {
		if svc.IsCommand() {
			continue
		}
		wg.Add(1)
		go func(svc domain.Service) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dirs := servicePackageDirs(projectRoot, svc.Package)
			mu.Lock()
			out[svc.Name] = dirs
			mu.Unlock()
		}(svc)
	}
	wg.Wait()
	return out
}

// servicePackageDirs returns the absolute directories of pkg and
// every package it transitively imports, per `go list -json -deps`.
// Best-effort: a failed or partial `go list` (a broken import, a
// toolchain hiccup) still yields whatever packages did resolve before
// the failure, rather than nothing - the caller falls back to "affects
// everyone" for the rare case that yields no directories at all
// (a service whose own directory doesn't even show up means something
// went wrong, not that it genuinely depends on nothing).
func servicePackageDirs(projectRoot, pkg string) map[string]struct{} {
	cmd := exec.Command("go", "list", "-json", "-deps", pkg)
	cmd.Dir = projectRoot
	out, _ := cmd.Output()

	dirs := map[string]struct{}{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var pk struct {
			Dir string
		}
		if err := dec.Decode(&pk); err != nil {
			break
		}
		if pk.Dir != "" {
			dirs[pk.Dir] = struct{}{}
		}
	}
	return dirs
}
