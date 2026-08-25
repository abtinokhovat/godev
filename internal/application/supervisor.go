// Package application is the core: the Supervisor drives each service's
// lifecycle (build, start, stop, restart, debug, crash/restart-on-change)
// and publishes Events + log lines for the TUI/CLI to consume. Per
// section 29, it is "the heart of the application"; the TUI never
// touches processes directly.
package application

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/abtinokhovat/godev/internal/builder"
	"github.com/abtinokhovat/godev/internal/debugger"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/process"
)

const stopTimeout = 5 * time.Second

// serviceEntry bundles a service's config, runtime snapshot, and the
// per-service lock that serializes its lifecycle operations (section 30).
type serviceEntry struct {
	svc     domain.Service
	runtime domain.ServiceRuntime
	opLock  sync.Mutex // serializes build/start/stop/restart for this service

	handle    *process.Handle
	debug     *debugger.Session
	backoff   *backoffState
	lastBuild BuildInfo

	// generation guards against a stale monitor goroutine acting on a
	// service after it has already moved on (e.g. stopped then restarted).
	generation int
}

// BuildInfo is the outcome of the most recent build attempt for a
// service, kept around so the TUI's Build view has something to show
// even between builds (and so a successful build after a failure still
// has the old failing output available for a moment).
type BuildInfo struct {
	Output    string
	Success   bool
	Attempted bool
	At        time.Time
}

type Supervisor struct {
	ProjectRoot string

	mu      sync.RWMutex
	entries map[string]*serviceEntry
	order   []string

	builder *builder.Builder
	logsMgr *logs.Manager
	events  *EventBus

	// buildSem bounds how many `go build` invocations run at once
	// across every service this Supervisor manages. Without it, a
	// crash-loop or hot-reload correlated across many services (a
	// shared package edit, a group start) fires one build per service
	// concurrently and unboundedly, oversubscribing the CPU and making
	// every single build slower than it would be run one at a time -
	// this caps concurrent compilation at GOMAXPROCS regardless of how
	// many services need rebuilding at once.
	buildSem chan struct{}

	// deps scopes hot-reload restarts to only the services a changed
	// file could actually affect - see depindex.go.
	deps *depIndex

	watchActive bool
}

func NewSupervisor(projectRoot string, services []domain.Service) (*Supervisor, error) {
	b, err := builder.New(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("initializing builder: %w", err)
	}
	s := &Supervisor{
		ProjectRoot: projectRoot,
		entries:     make(map[string]*serviceEntry, len(services)),
		builder:     b,
		logsMgr:     logs.NewManager(5000),
		events:      NewEventBus(),
		buildSem:    make(chan struct{}, runtime.GOMAXPROCS(0)),
		deps:        newDepIndex(),
	}
	for _, svc := range services {
		s.entries[svc.Name] = &serviceEntry{
			svc:     svc,
			runtime: domain.ServiceRuntime{State: domain.StateDiscovered},
			backoff: newBackoff(),
		}
		s.order = append(s.order, svc.Name)
		s.events.Publish(Event{Type: EventServiceDiscovered, Service: svc.Name,
			Message: fmt.Sprintf("discovered %s (%s)", svc.Name, svc.Package)})
	}
	return s, nil
}

func (s *Supervisor) Logs() *logs.Manager { return s.logsMgr }
func (s *Supervisor) Events() *EventBus   { return s.events }

// SubscribeEvents and SubscribeLogs flatten Events().Subscribe()/
// Logs().Subscribe() into direct Supervisor methods, and ClearLogs
// flattens Logs().Clear() - together these are exactly what
// tui.Source needs, so both the in-process TUI and a remote
// (attached) client can be written against the same small interface
// instead of the full Supervisor/EventBus/logs.Manager surface.
func (s *Supervisor) SubscribeEvents(buf int) (<-chan Event, func()) {
	return s.events.Subscribe(buf)
}

func (s *Supervisor) SubscribeLogs(buf int) (<-chan logs.Event, func()) {
	return s.logsMgr.Subscribe(buf)
}

func (s *Supervisor) ClearLogs() {
	s.logsMgr.Clear()
}

// Services returns the static config for every known service, in
// discovery order.
func (s *Supervisor) Services() []domain.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Service, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.entries[name].svc)
	}
	return out
}

// Runtime returns a snapshot of a service's current runtime state.
func (s *Supervisor) Runtime(name string) (domain.ServiceRuntime, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[name]
	if !ok {
		return domain.ServiceRuntime{}, false
	}
	return e.runtime, true
}

// BuildInfo returns the outcome of a service's most recent build
// attempt, for the TUI's Build view.
func (s *Supervisor) BuildInfo(name string) (BuildInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[name]
	if !ok {
		return BuildInfo{}, false
	}
	return e.lastBuild, true
}

func (s *Supervisor) recordBuild(e *serviceEntry, res builder.Result) {
	s.mu.Lock()
	e.lastBuild = BuildInfo{Output: res.Output, Success: res.Success, Attempted: true, At: time.Now()}
	s.mu.Unlock()
}

// SetWatchActive records whether the project-wide hot-reload watcher is
// currently running, for the TUI's sidebar/header status.
func (s *Supervisor) SetWatchActive(active bool) {
	s.mu.Lock()
	s.watchActive = active
	s.mu.Unlock()
}

// WatchActive reports whether hot reload is currently active.
func (s *Supervisor) WatchActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watchActive
}

func (s *Supervisor) entry(name string) (*serviceEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[name]
	return e, ok
}

func (s *Supervisor) setState(e *serviceEntry, name string, state domain.State) {
	s.mu.Lock()
	e.runtime.State = state
	s.mu.Unlock()
}

func (s *Supervisor) log(name string, stream logs.Stream, msg string) {
	s.logsMgr.Publish(logs.Event{Service: name, Stream: stream, Message: msg})
}

// StartAll starts every service configured with AutoStart. This is
// for a bare invocation with nothing named explicitly (plain `godev`,
// or `godev --detach` with no target) - what runs is entirely up to
// each service's own auto_start setting, which defaults to false, so
// a bare invocation with no configured auto-starters starts nothing.
func (s *Supervisor) StartAll() {
	for _, name := range s.order {
		e, _ := s.entry(name)
		if e.svc.AutoStart {
			go func(n string) {
				if err := s.Start(n); err != nil {
					s.log(n, logs.StreamSystem, "start failed: "+err.Error())
				}
			}(name)
		}
	}
}

// StartServices starts exactly the named services, regardless of each
// one's AutoStart setting - for when the caller named what to run
// explicitly (`godev run <target>...`), where being asked for by name
// is itself the start signal, distinct from StartAll's "whatever's
// configured to start on a bare invocation".
func (s *Supervisor) StartServices(names []string) {
	for _, name := range names {
		go func(n string) {
			if err := s.Start(n); err != nil {
				s.log(n, logs.StreamSystem, "start failed: "+err.Error())
			}
		}(name)
	}
}

// Shutdown stops every running service and debugger, per section 50's
// graceful-shutdown sequence.
func (s *Supervisor) Shutdown() {
	var wg sync.WaitGroup
	for _, name := range s.order {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			_ = s.StopDebug(n)
			_ = s.Stop(n)
		}(name)
	}
	wg.Wait()
}
