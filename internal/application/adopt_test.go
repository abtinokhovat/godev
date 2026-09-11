package application

import (
	"runtime"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/process"
)

// TestAdoptRunningReclaimsProcessLeftByACrashedInstance is the core
// promise this whole mechanism exists for: if godev crashes (or is
// killed) without a clean shutdown, a fresh instance for the same
// project recognizes and takes back over its still-running processes
// instead of either starting a duplicate alongside them or losing
// track of them.
func TestAdoptRunningReclaimsProcessLeftByACrashedInstance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group adoption isn't supported on Windows yet")
	}
	projectRoot := t.TempDir()
	svc := domain.Service{
		Name:      "web",
		Command:   []string{"/bin/sh", "-c", "sleep 30"},
		Directory: t.TempDir(),
	}

	first, err := NewSupervisor(projectRoot, []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor (first instance): %v", err)
	}
	if err := first.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rt := waitForState(t, first, "web", domain.StateRunning, 2*time.Second)
	originalPID := rt.PID
	if originalPID == 0 {
		t.Fatal("expected a nonzero PID")
	}
	// Deliberately no Shutdown()/Stop() here - this is meant to
	// simulate `first` crashing while "web" keeps running, the exact
	// scenario adoptRunning exists to recover from. The process is
	// cleaned up at the end of the test via the second Supervisor.

	second, err := NewSupervisor(projectRoot, []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor (second instance): %v", err)
	}
	t.Cleanup(second.Shutdown)

	rt2, ok := second.Runtime("web")
	if !ok {
		t.Fatal("expected \"web\" to exist in the second Supervisor")
	}
	if rt2.State != domain.StateRunning {
		t.Fatalf("state after adoption = %v, want Running (a fresh instance should recognize the still-running process, not leave it Discovered)", rt2.State)
	}
	if rt2.PID != originalPID {
		t.Fatalf("adopted PID = %d, want the original %d (a different PID here would mean a duplicate got started instead of adopting)", rt2.PID, originalPID)
	}
	if !process.IsAlive(originalPID) {
		t.Fatal("the original process should still be the one running - adoption must not have restarted it")
	}

	// The registry entry survives being read by the second instance
	// (adoptRunning doesn't consume/clear it); stopping through the
	// second instance actually terminates the real process and cleans
	// up its own registry entry, proving adoption produces a fully
	// working handle, not just a cosmetic state label.
	if err := second.Stop("web"); err != nil {
		t.Fatalf("Stop (via second instance): %v", err)
	}
	waitForState(t, second, "web", domain.StateStopped, 3*time.Second)
	if process.IsAlive(originalPID) {
		t.Error("expected the original process to actually be dead after Stop() via the adopting instance")
	}
}

// TestAdoptRunningIgnoresStaleEntryAfterPIDReuse guards against the
// unsafe case: a registry entry whose PID is alive, but now belongs to
// a completely different process (the OS reused the number after the
// original service exited) - argv[0] no longer matching the service
// name is what has to catch this, since liveness alone can't.
func TestAdoptRunningIgnoresStaleEntryAfterPIDReuse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group adoption isn't supported on Windows yet")
	}
	projectRoot := t.TempDir()
	svc := domain.Service{Name: "web", Command: []string{"/bin/sh", "-c", "sleep 30"}, Directory: t.TempDir()}

	// A real, currently-alive process whose argv[0] is NOT "web" -
	// standing in for "some unrelated process the OS happened to
	// assign the recorded PID to after the original exited".
	unrelated, err := process.Start(process.StartOptions{Binary: "/bin/sh", Args: []string{"-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("starting the unrelated process: %v", err)
	}
	t.Cleanup(unrelated.Kill)

	sup, err := NewSupervisor(projectRoot, []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)
	// Plant a stale registry entry directly, bypassing a real start -
	// this is what adoptRunning would find if "web" had actually run
	// as this PID in some earlier instance, and the OS has since
	// reused the number for something else entirely.
	sup.reg.set("web", registryEntry{PID: unrelated.PID, StartedAt: time.Now()})

	sup2, err := NewSupervisor(projectRoot, []domain.Service{svc})
	if err != nil {
		t.Fatalf("NewSupervisor (adopting instance): %v", err)
	}
	t.Cleanup(sup2.Shutdown)

	rt, ok := sup2.Runtime("web")
	if !ok {
		t.Fatal("expected \"web\" to exist")
	}
	if rt.State == domain.StateRunning {
		t.Fatalf("adopted an unrelated process (PID %d) just because it happened to be alive - argv[0] fingerprint check should have refused it", rt.PID)
	}
}
