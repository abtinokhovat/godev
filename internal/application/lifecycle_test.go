package application

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
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
// TestBuildSemBoundsConcurrentCommandServiceStarts covers the gap a
// build-only semaphore would leave: command-based services skip the
// build step entirely, so if buildAndStart's semaphore only wrapped
// `go build`, starting a pile of them at once (a big `godev run
// <group>`) would be completely unbounded. Starts many concurrently
// and checks the semaphore never over-admits and always drains back
// to empty - a leaked slot would permanently under-provision every
// build/start after it.
func TestBuildSemBoundsConcurrentCommandServiceStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	const n = 20
	services := make([]domain.Service, n)
	for i := range services {
		services[i] = domain.Service{
			Name:      fmt.Sprintf("svc-%02d", i),
			Command:   []string{"/bin/sh", "-c", "sleep 2"},
			Directory: t.TempDir(),
		}
	}
	sup, err := NewSupervisor(t.TempDir(), services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	var wg sync.WaitGroup
	for _, svc := range services {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := sup.Start(name); err != nil {
				t.Errorf("Start(%s): %v", name, err)
			}
		}(svc.Name)
	}
	wg.Wait()

	for _, svc := range services {
		waitForState(t, sup, svc.Name, domain.StateRunning, 2*time.Second)
	}

	if len(sup.buildSem) != 0 {
		t.Fatalf("buildSem has %d slots still held after every Start returned, want 0 (a leaked slot)", len(sup.buildSem))
	}
}

// TestRunningServicePortIsDiscoveredAndClearedOnStop is an end-to-end
// check of the Supervisor <-> internal/ports wiring (see ports.go):
// a service that actually opens a TCP listener should have it show up
// in Runtime().Ports shortly after starting, and Ports should clear
// once the service is stopped - not linger as stale "last known"
// data for a service that isn't running anymore. Linux-only since
// internal/ports' /proc-based discovery is what's being exercised
// here (its own package covers the platform-specific mechanics).
func TestRunningServicePortIsDiscoveredAndClearedOnStop(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exercises internal/ports' /proc-based discovery")
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/go.mod", []byte("module listener\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `
package main

import ("fmt"; "net"; "time")

func main() {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { panic(err) }
	fmt.Println(l.Addr().(*net.TCPAddr).Port)
	time.Sleep(10 * time.Second)
}
`
	if err := os.WriteFile(dir+"/main.go", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := dir + "/listener"
	if out, err := exec.Command("go", "build", "-o", binPath, dir+"/main.go").CombinedOutput(); err != nil {
		t.Fatalf("building test listener: %v\n%s", err, out)
	}

	svc := domain.Service{Name: "web", Command: []string{binPath}, Directory: dir}
	sup, err := NewSupervisor(t.TempDir(), []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := sup.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForState(t, sup, "web", domain.StateRunning, 2*time.Second)

	deadline := time.Now().Add(3 * time.Second)
	var rt domain.ServiceRuntime
	for time.Now().Before(deadline) {
		rt, _ = sup.Runtime("web")
		if len(rt.Ports) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(rt.Ports) != 1 {
		t.Fatalf("Runtime(web).Ports = %v, want exactly 1 discovered port", rt.Ports)
	}

	if err := sup.Stop("web"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rt, _ = sup.Runtime("web")
	if len(rt.Ports) != 0 {
		t.Fatalf("Runtime(web).Ports after Stop = %v, want cleared (empty)", rt.Ports)
	}
}

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
