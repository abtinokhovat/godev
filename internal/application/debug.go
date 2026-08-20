package application

import (
	"fmt"
	"time"

	"github.com/abtinokhovat/godev/internal/builder"
	"github.com/abtinokhovat/godev/internal/debugger"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/process"
)

// StartDebug stops the service's normal process (if any), builds a
// debug binary, and launches headless Delve, per section 36's lifecycle.
// Only one of {normal process, debug session} owns the binary at a time
// (section 40): starting debug always stops the normal run first.
func (s *Supervisor) StartDebug(name string) error {
	e, ok := s.entry(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	if err := debugger.CheckInstalled(); err != nil {
		s.events.Publish(Event{Type: EventDebuggerFailed, Service: name, Err: err})
		s.log(name, logs.StreamSystem, err.Error())
		return err
	}
	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.RLock()
	alreadyDebugging := e.debug != nil && e.debug.State == domain.DebugRunning
	s.mu.RUnlock()
	if alreadyDebugging {
		return nil
	}

	// Stop any normal run first (section 40: modes don't fight over the binary).
	if e.handle != nil {
		s.stopLocked(e, name)
	}

	s.setState(e, name, domain.StateBuilding)
	res, err := s.builder.Build(e.svc, builder.ModeDebug)
	if err != nil {
		s.recordBuild(e, builder.Result{Success: false, Output: err.Error()})
		s.events.Publish(Event{Type: EventDebuggerFailed, Service: name, Err: err})
		return err
	}
	s.recordBuild(e, res)
	if !res.Success {
		s.mu.Lock()
		e.runtime.State = domain.StateBuildFailed
		e.runtime.LastError = res.Output
		s.mu.Unlock()
		s.events.Publish(Event{Type: EventDebuggerFailed, Service: name, Message: res.Output})
		return fmt.Errorf("debug build failed")
	}

	port, err := debugger.FindAvailablePort(2345)
	if err != nil {
		s.events.Publish(Event{Type: EventDebuggerFailed, Service: name, Err: err})
		return err
	}

	s.events.Publish(Event{Type: EventDebuggerStarting, Service: name,
		Message: fmt.Sprintf("127.0.0.1:%d", port)})
	s.log(name, logs.StreamSystem, fmt.Sprintf("starting delve on 127.0.0.1:%d", port))

	sess, err := debugger.Start(name, "127.0.0.1", port, res.BinaryPath, e.svc.Args, e.svc.Directory,
		process.BuildEnv(e.svc.Env))
	if err != nil {
		s.events.Publish(Event{Type: EventDebuggerFailed, Service: name, Err: err})
		return err
	}

	if err := sess.WaitListening(10 * time.Second); err != nil {
		sess.Stop()
		s.events.Publish(Event{Type: EventDebuggerFailed, Service: name, Err: err})
		return err
	}

	s.mu.Lock()
	e.debug = sess
	e.runtime.State = domain.StateRunning
	e.runtime.PID = sess.DelvePID
	e.runtime.BinaryPath = res.BinaryPath
	e.runtime.StartedAt = time.Now()
	e.runtime.Debug = sess.DebugSession
	s.mu.Unlock()

	s.events.Publish(Event{Type: EventDebuggerStarted, Service: name,
		Message: fmt.Sprintf("127.0.0.1:%d", port)})
	s.log(name, logs.StreamSystem, "debugger ready: "+sess.VSCodeInstructions())
	s.log(name, logs.StreamSystem, "debugger ready: "+sess.GoLandInstructions())

	go func() {
		_ = sess.Wait()
		s.mu.Lock()
		if e.debug == sess {
			e.runtime.Debug = nil
			e.debug = nil
		}
		s.mu.Unlock()
		s.events.Publish(Event{Type: EventDebuggerStopped, Service: name})
	}()

	return nil
}

// StopDebug stops a running debug session, if any.
func (s *Supervisor) StopDebug(name string) error {
	e, ok := s.entry(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.Lock()
	sess := e.debug
	s.mu.Unlock()
	if sess == nil {
		return nil
	}
	sess.Stop()
	s.mu.Lock()
	e.debug = nil
	e.runtime.Debug = nil
	s.mu.Unlock()
	s.events.Publish(Event{Type: EventDebuggerStopped, Service: name})
	return nil
}

// stopLocked stops a service's normal process; caller must already hold
// e.opLock.
func (s *Supervisor) stopLocked(e *serviceEntry, name string) {
	s.mu.Lock()
	handle := e.handle
	if e.runtime.State == domain.StateRunning {
		e.runtime.State = domain.StateStopping
	}
	e.generation++ // see Stop()'s comment: invalidates any pending crash-restart
	s.mu.Unlock()
	if handle == nil {
		return
	}
	s.events.Publish(Event{Type: EventServiceStopping, Service: name})
	handle.Stop(stopTimeout)
}
