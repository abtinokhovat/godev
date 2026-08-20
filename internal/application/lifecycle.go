package application

import (
	"fmt"
	"time"

	"github.com/abtinokhovat/godev/internal/builder"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/process"
)

// build compiles the service in normal mode, publishing build events.
// Per section 14, a failed build leaves any previously built binary
// untouched - the builder itself guarantees that via atomic rename.
func (s *Supervisor) build(e *serviceEntry, name string) (builder.Result, error) {
	s.setState(e, name, domain.StateBuilding)
	s.events.Publish(Event{Type: EventBuildStarted, Service: name, Message: "building"})
	s.log(name, logs.StreamSystem, "building...")

	res, err := s.builder.Build(e.svc, builder.ModeNormal)
	if err != nil {
		s.events.Publish(Event{Type: EventBuildFailed, Service: name, Err: err})
		return res, err
	}
	if !res.Success {
		s.mu.Lock()
		e.runtime.State = domain.StateBuildFailed
		e.runtime.LastError = res.Output
		s.mu.Unlock()
		s.events.Publish(Event{Type: EventBuildFailed, Service: name, Message: res.Output})
		s.log(name, logs.StreamSystem, "build failed:\n"+res.Output)
		return res, fmt.Errorf("build failed")
	}

	s.events.Publish(Event{Type: EventBuildSucceeded, Service: name})
	s.log(name, logs.StreamSystem, "build succeeded")
	return res, nil
}

// Start builds (if needed) and starts a service. It is a no-op if the
// service is already running.
func (s *Supervisor) Start(name string) error {
	e, ok := s.entry(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.RLock()
	alreadyRunning := e.runtime.State == domain.StateRunning || e.runtime.State == domain.StateStarting
	s.mu.RUnlock()
	if alreadyRunning {
		return nil
	}

	res, err := s.build(e, name)
	if err != nil {
		return err
	}

	return s.startBinary(e, name, res.BinaryPath)
}

func (s *Supervisor) startBinary(e *serviceEntry, name, binaryPath string) error {
	s.setState(e, name, domain.StateStarting)
	s.events.Publish(Event{Type: EventServiceStarting, Service: name})

	out := make(chan process.OutputLine, 256)
	handle, err := process.Start(process.StartOptions{
		Binary: binaryPath,
		Args:   e.svc.Args,
		Dir:    e.svc.Directory,
		Env:    process.BuildEnv(e.svc.Env),
		Output: out,
	})
	if err != nil {
		s.mu.Lock()
		e.runtime.State = domain.StateCrashed
		e.runtime.LastError = err.Error()
		s.mu.Unlock()
		s.events.Publish(Event{Type: EventServiceCrashed, Service: name, Err: err})
		return err
	}

	s.mu.Lock()
	e.handle = handle
	e.generation++
	gen := e.generation
	e.runtime.State = domain.StateRunning
	e.runtime.PID = handle.PID
	e.runtime.BinaryPath = binaryPath
	e.runtime.StartedAt = time.Now()
	e.runtime.LastError = ""
	s.mu.Unlock()

	s.events.Publish(Event{Type: EventServiceStarted, Service: name, Message: fmt.Sprintf("pid %d", handle.PID)})
	s.log(name, logs.StreamSystem, fmt.Sprintf("started (pid %d)", handle.PID))

	go s.pumpOutput(name, out)
	go s.monitor(e, name, handle, gen)
	return nil
}

func (s *Supervisor) pumpOutput(name string, out <-chan process.OutputLine) {
	for line := range out {
		stream := logs.StreamStdout
		if line.Stream == process.StreamStderr {
			stream = logs.StreamStderr
		}
		s.log(name, stream, line.Text)
	}
}

// monitor waits for a process to exit and reacts: a deliberate stop just
// records STOPPED, anything else is treated as a crash and (if
// auto-restart is enabled) triggers a backoff-guarded restart, per
// section 24-25.
func (s *Supervisor) monitor(e *serviceEntry, name string, handle *process.Handle, gen int) {
	err := handle.Wait()

	s.mu.Lock()
	stale := e.generation != gen
	wasStopping := e.runtime.State == domain.StateStopping
	s.mu.Unlock()
	if stale {
		return // superseded by a newer start/restart
	}

	if wasStopping {
		s.mu.Lock()
		e.runtime.State = domain.StateStopped
		e.runtime.PID = 0
		s.mu.Unlock()
		s.events.Publish(Event{Type: EventServiceStopped, Service: name})
		s.log(name, logs.StreamSystem, "stopped")
		return
	}

	// Unexpected exit -> crash.
	exitMsg := "process exited"
	if err != nil {
		exitMsg = err.Error()
	}
	s.mu.Lock()
	e.runtime.State = domain.StateCrashed
	e.runtime.PID = 0
	e.runtime.LastError = exitMsg
	s.mu.Unlock()
	s.events.Publish(Event{Type: EventServiceCrashed, Service: name, Message: exitMsg})
	s.log(name, logs.StreamSystem, "crashed: "+exitMsg)

	if e.svc.AutoRestart {
		go s.crashRestart(e, name, gen)
	}
}

// crashRestart applies exponential backoff (section 25) before
// rebuilding and restarting a crashed service.
func (s *Supervisor) crashRestart(e *serviceEntry, name string, gen int) {
	delay := e.backoff.next()

	s.events.Publish(Event{Type: EventServiceRestarting, Service: name,
		Message: fmt.Sprintf("restarting in %s", delay)})
	s.log(name, logs.StreamSystem, fmt.Sprintf("restarting in %s...", delay))
	time.Sleep(delay)

	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.RLock()
	supersededOrStopped := e.generation != gen
	s.mu.RUnlock()
	if supersededOrStopped {
		return
	}

	s.setState(e, name, domain.StateRestarting)
	res, err := s.build(e, name)
	if err != nil {
		return
	}
	_ = s.startBinary(e, name, res.BinaryPath)

	// Reset backoff once the service has stayed up long enough (section 25).
	go e.backoff.watchStability(func() bool {
		rt, ok := s.Runtime(name)
		return ok && rt.State == domain.StateRunning
	})
}

// Stop gracefully stops a running service.
func (s *Supervisor) Stop(name string) error {
	e, ok := s.entry(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.Lock()
	handle := e.handle
	running := e.runtime.State == domain.StateRunning || e.runtime.State == domain.StateCrashed
	if e.runtime.State == domain.StateRunning {
		e.runtime.State = domain.StateStopping
	}
	// Bump the generation so any crash-restart backoff still sleeping for
	// this service (tied to the old generation) sees itself as superseded
	// and aborts instead of reviving a service the caller just stopped.
	e.generation++
	s.mu.Unlock()

	if handle == nil || !running {
		s.mu.Lock()
		if e.runtime.State != domain.StateStopped {
			e.runtime.State = domain.StateStopped
		}
		s.mu.Unlock()
		return nil
	}

	s.events.Publish(Event{Type: EventServiceStopping, Service: name})
	s.log(name, logs.StreamSystem, "stopping...")
	handle.Stop(stopTimeout)
	return nil
}

// Restart stops (if running) then starts a service again.
func (s *Supervisor) Restart(name string) error {
	if err := s.Stop(name); err != nil {
		return err
	}
	return s.Start(name)
}
