package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/abtinokhovat/godev/internal/builder"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/process"
)

// build compiles the service in normal mode, publishing build events.
// A failed build leaves any previously built binary untouched - the
// builder itself guarantees that via atomic rename.
//
// Command-based services (domain.Service.IsCommand(), i.e. manual
// .godev.yaml entries or imported run configurations for non-Go
// services) have no build step at all - godev execs their Command
// directly - so this returns instantly with a synthetic success.
func (s *Supervisor) build(e *serviceEntry, name string) (builder.Result, error) {
	s.setState(e, name, domain.StateBuilding)
	s.events.Publish(Event{Type: EventBuildStarted, Service: name, Message: "building"})

	if e.svc.IsCommand() {
		res := builder.Result{Success: true, Output: "no build step (command-based service)"}
		s.recordBuild(e, res)
		s.events.Publish(Event{Type: EventBuildSucceeded, Service: name})
		return res, nil
	}

	s.log(name, logs.StreamSystem, "building...")

	res, err := s.builder.Build(e.svc, builder.ModeNormal)
	if err != nil {
		s.recordBuild(e, builder.Result{Success: false, Output: err.Error()})
		s.events.Publish(Event{Type: EventBuildFailed, Service: name, Err: err})
		return res, err
	}
	s.recordBuild(e, res)
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

	return s.startProcess(e, name, runCommand(e.svc, res))
}

// runCommand resolves what to actually exec for a service: its
// explicit Command for command-based (non-Go) services, or the
// freshly-built binary for Go services.
func runCommand(svc domain.Service, res builder.Result) []string {
	if svc.IsCommand() {
		return svc.Command
	}
	return []string{res.BinaryPath}
}

func (s *Supervisor) startProcess(e *serviceEntry, name string, command []string) error {
	s.setState(e, name, domain.StateStarting)
	s.events.Publish(Event{Type: EventServiceStarting, Service: name})

	out := make(chan process.OutputLine, 256)
	handle, err := process.Start(process.StartOptions{
		Binary: command[0],
		Args:   append(append([]string{}, command[1:]...), e.svc.Args...),
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
	e.runtime.BinaryPath = strings.Join(command, " ")
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

// monitor waits for a process to exit and reacts: a deliberate stop
// gets finalized to STOPPED (see finalizeStopped), anything else is
// treated as a crash and (if auto-restart is enabled) triggers a
// backoff-guarded restart, per section 24-25.
func (s *Supervisor) monitor(e *serviceEntry, name string, handle *process.Handle, gen int) {
	err := handle.Wait()

	s.mu.Lock()
	wasStopping := e.runtime.State == domain.StateStopping
	stale := e.generation != gen
	s.mu.Unlock()

	if wasStopping {
		// Stop() may already have finalized this itself (see
		// finalizeStopped) by the time this goroutine gets scheduled -
		// that's fine, the check-and-set inside is idempotent.
		s.finalizeStopped(e, name)
		return
	}

	if stale {
		return // superseded by a newer start/restart
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
		go s.crashRestart(e, name)
	}
}

// crashRestart applies exponential backoff (section 25) before
// rebuilding and restarting a crashed service. It re-checks the
// service's state after waking from its backoff sleep (under opLock,
// so nothing else can be mid-transition) rather than relying on the
// generation counter: opLock serialization guarantees state can only
// have moved away from Crashed if something else - a user-initiated
// Stop(), or another Start()/Restart() that raced ahead while this
// goroutine slept - already handled it, in which case this restart is
// no longer wanted and must not revive the service.
func (s *Supervisor) crashRestart(e *serviceEntry, name string) {
	delay := e.backoff.next()

	s.events.Publish(Event{Type: EventServiceRestarting, Service: name,
		Message: fmt.Sprintf("restarting in %s", delay)})
	s.log(name, logs.StreamSystem, fmt.Sprintf("restarting in %s...", delay))
	time.Sleep(delay)

	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.RLock()
	stillCrashed := e.runtime.State == domain.StateCrashed
	s.mu.RUnlock()
	if !stillCrashed {
		return
	}

	s.setState(e, name, domain.StateRestarting)
	res, err := s.build(e, name)
	if err != nil {
		return
	}
	_ = s.startProcess(e, name, runCommand(e.svc, res))

	// Reset backoff once the service has stayed up long enough (section 25).
	go e.backoff.watchStability(func() bool {
		rt, ok := s.Runtime(name)
		return ok && rt.State == domain.StateRunning
	})
}

// finalizeStopped transitions e to Stopped exactly once per Stopping
// episode: it's a check-and-set, so whichever of Stop() (synchronously,
// right after confirming the process is dead) or monitor()
// (asynchronously, on separately noticing the same handle exit) gets
// here first does the actual work - the other call is a no-op, since by
// then the state is no longer Stopping. This matters because both
// Stop()'s handle.Stop() and monitor()'s handle.Wait() unblock from the
// same closed channel with no ordering guarantee between them: without
// this, a caller of Stop() could see it return while the state was
// still Stopping, racing monitor()'s own finalization. Calling this
// synchronously from Stop() closes that race - Runtime(name).State is
// guaranteed to already be Stopped by the time Stop() returns.
func (s *Supervisor) finalizeStopped(e *serviceEntry, name string) {
	s.mu.Lock()
	if e.runtime.State != domain.StateStopping {
		s.mu.Unlock()
		return
	}
	e.runtime.State = domain.StateStopped
	e.runtime.PID = 0
	s.mu.Unlock()
	s.events.Publish(Event{Type: EventServiceStopped, Service: name})
	s.log(name, logs.StreamSystem, "stopped")
}

// Stop gracefully stops a running service. When it returns, the
// service's state is guaranteed to already be Stopped (see
// finalizeStopped) - callers never need to poll for that themselves.
func (s *Supervisor) Stop(name string) error {
	e, ok := s.entry(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	e.opLock.Lock()
	defer e.opLock.Unlock()

	s.mu.Lock()
	handle := e.handle
	live := e.runtime.State == domain.StateRunning
	if live {
		e.runtime.State = domain.StateStopping
	} else if e.runtime.State != domain.StateStopped {
		// No live process to signal (crashed, build-failed, never
		// started, etc). This also opLock-serializes against
		// crashRestart(), which re-checks state == Crashed after its
		// backoff sleep - setting Stopped here is what makes a
		// crash-restart that's currently sleeping abort instead of
		// reviving a service the caller just stopped.
		e.runtime.State = domain.StateStopped
	}
	s.mu.Unlock()

	if !live {
		return nil
	}

	s.events.Publish(Event{Type: EventServiceStopping, Service: name})
	s.log(name, logs.StreamSystem, "stopping...")
	handle.Stop(stopTimeout)
	// handle.Stop() only returns once the process is confirmed dead (it
	// always blocks on the process's exit, escalating to SIGKILL past
	// the timeout), so it's safe to finalize here rather than leaving it
	// entirely to monitor() racing on the same handle.
	s.finalizeStopped(e, name)
	return nil
}

// Restart stops (if running) then starts a service again.
func (s *Supervisor) Restart(name string) error {
	if err := s.Stop(name); err != nil {
		return err
	}
	return s.Start(name)
}
