package application

import (
	"runtime"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/domain"
)

func waitForState(t *testing.T, s *Supervisor, name string, want domain.State, timeout time.Duration) domain.ServiceRuntime {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var rt domain.ServiceRuntime
	for time.Now().Before(deadline) {
		var ok bool
		rt, ok = s.Runtime(name)
		if ok && rt.State == want {
			return rt
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("service %q did not reach state %s within %s (last state: %s)", name, want, timeout, rt.State)
	return rt
}

func TestCommandServiceStartSkipsBuildAndRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	svc := domain.Service{
		Name:      "web",
		Command:   []string{"/bin/sh", "-c", "echo hello; sleep 5"},
		Directory: t.TempDir(),
	}
	sup, err := NewSupervisor(t.TempDir(), []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := sup.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rt := waitForState(t, sup, "web", domain.StateRunning, 2*time.Second)
	if rt.PID == 0 {
		t.Errorf("expected a nonzero PID for a running command-based service")
	}

	bi, ok := sup.BuildInfo("web")
	if !ok || !bi.Attempted || !bi.Success {
		t.Errorf("BuildInfo = %+v, want an instantly-successful synthetic build", bi)
	}

	if err := sup.Stop("web"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop() waits up to stopTimeout (5s) for a graceful SIGTERM exit
	// before escalating to SIGKILL, so give this more room than the
	// startup assertions above.
	waitForState(t, sup, "web", domain.StateStopped, 7*time.Second)
}

// TestStopOfRunningServiceReachesStopped is a regression test: an
// earlier fix for a different race (a crash-restart reviving a
// deliberately-stopped service) bumped e.generation inside Stop(),
// which made the process's own monitor() goroutine think itself
// superseded and skip its Stopped transition entirely - the service
// got stuck at Stopping forever. Stop() must always settle at Stopped.
func TestStopOfRunningServiceReachesStopped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	svc := domain.Service{
		Name:      "web",
		Command:   []string{"/bin/sh", "-c", "sleep 30"},
		Directory: t.TempDir(),
	}
	sup, err := NewSupervisor(t.TempDir(), []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := sup.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, sup, "web", domain.StateRunning, 2*time.Second)

	if err := sup.Stop("web"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitForState(t, sup, "web", domain.StateStopped, 2*time.Second)
}

// TestStopDuringCrashRestartBackoffAbortsRestart covers the race the
// generation-bump was originally meant to fix, now via a state check
// instead: stopping a service while its crash-restart backoff is still
// sleeping must prevent the restart from reviving it.
func TestStopDuringCrashRestartBackoffAbortsRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	svc := domain.Service{
		Name:        "web",
		Command:     []string{"/bin/sh", "-c", "exit 1"},
		Directory:   t.TempDir(),
		AutoRestart: true,
	}
	sup, err := NewSupervisor(t.TempDir(), []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := sup.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The process exits immediately (exit 1) and crashRestart() begins
	// its first backoff sleep (1s, per backoffInitial) - stop it well
	// within that window.
	waitForState(t, sup, "web", domain.StateCrashed, 2*time.Second)
	if err := sup.Stop("web"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Give the sleeping crashRestart goroutine time to wake up and
	// (incorrectly, if the bug regresses) revive the service.
	time.Sleep(1500 * time.Millisecond)

	rt, ok := sup.Runtime("web")
	if !ok {
		t.Fatal("service disappeared")
	}
	if rt.State != domain.StateStopped {
		t.Fatalf("state = %s, want STOPPED (crash-restart should have aborted after Stop)", rt.State)
	}
}

func TestStartDebugRejectsCommandBasedService(t *testing.T) {
	svc := domain.Service{Name: "web", Command: []string{"echo", "hi"}, Directory: t.TempDir()}
	sup, err := NewSupervisor(t.TempDir(), []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}

	err = sup.StartDebug("web")
	if err == nil {
		t.Fatal("expected StartDebug to reject a command-based service")
	}
}
