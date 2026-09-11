package application

import (
	"fmt"

	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/process"
)

// adoptRunning checks the on-disk registry for services a previous
// instance of godev for this same project left running when it didn't
// shut down cleanly (a crash, a killed terminal, `kill -9`) - their
// processes survive as-is, in their own process group, independent of
// godev's own lifetime, so a fresh Supervisor that didn't know about
// them would otherwise either start a duplicate alongside them or
// simply lose track of them forever.
//
// A registry entry is only trusted if the PID is still alive AND its
// argv[0] still matches the service name godev itself always sets
// there (process.StartOptions.Name) - PIDs get reused by the OS, so
// liveness alone isn't proof it's the same process. Anything that
// fails either check is dropped from the registry rather than
// adopted, so a stale entry doesn't linger forever.
//
// An adopted service's output from before this instance existed is
// already available via seedHistory's disk-backed replay. Its output
// going forward generally is not: the process's original stdout/
// stderr were pipes to the now-dead previous instance, which this new
// one has no way to reconnect to - and the OS commonly kills a process
// with SIGPIPE the next time it tries to write to a pipe whose reader
// is gone, so there's frequently nothing left to reconnect to anyway.
// Restarting an adopted service gives it a fresh pipe under this
// instance's control, resuming live output normally.
func (s *Supervisor) adoptRunning() {
	for name, re := range s.reg.load() {
		e, ok := s.entry(name)
		if !ok {
			s.reg.remove(name)
			continue
		}
		if !process.IsAlive(re.PID) || !process.ArgvMatchesName(re.PID, name) {
			s.reg.remove(name)
			continue
		}

		handle := process.Adopt(re.PID)

		s.mu.Lock()
		e.handle = handle
		e.generation++
		gen := e.generation
		e.runtime.State = domain.StateRunning
		e.runtime.PID = re.PID
		e.runtime.StartedAt = re.StartedAt
		e.runtime.LastError = ""
		s.mu.Unlock()

		s.events.Publish(Event{Type: EventServiceStarted, Service: name,
			Message: fmt.Sprintf("adopted pid %d from a previous instance", re.PID)})
		s.log(name, logs.StreamSystem, fmt.Sprintf(
			"adopted already-running process (pid %d) left by a previous instance - restart it to resume live output", re.PID))

		go s.monitor(e, name, handle, gen)
		go s.pollPorts(e, name, re.PID, gen, handle.Done())
	}
}
