package application

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/domain"
)

// loadReloadFixture writes yaml to root/.godev.yaml and runs it
// through the exact same config.Load+Merge pipeline Reload() itself
// uses, so a test's "initial" services are built with the same
// defaults (AutoStart, AutoRestart, ...) Reload would compute for an
// unchanged entry - otherwise a hand-built domain.Service literal
// would spuriously look "changed" against Reload's own output.
func loadReloadFixture(t *testing.T, root, yaml string) []domain.Service {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, config.FileName), []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing %s: %v", config.FileName, err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	services, err := config.Merge(root, nil, cfg)
	if err != nil {
		t.Fatalf("config.Merge: %v", err)
	}
	return services
}

func TestReloadAddsNewServiceWithoutStarting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	root := t.TempDir()
	services := loadReloadFixture(t, root, `
services:
  api:
    command: ["/bin/sh", "-c", "sleep 5"]
`)
	sup, err := NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := os.WriteFile(filepath.Join(root, config.FileName), []byte(`
services:
  api:
    command: ["/bin/sh", "-c", "sleep 5"]
  worker:
    command: ["/bin/sh", "-c", "sleep 5"]
`), 0o644); err != nil {
		t.Fatalf("writing %s: %v", config.FileName, err)
	}

	if err := sup.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := sup.Services()
	if len(got) != 2 {
		t.Fatalf("Services() after reload = %d services, want 2 (%+v)", len(got), got)
	}
	rt, ok := sup.Runtime("worker")
	if !ok {
		t.Fatal("worker not registered after reload")
	}
	if rt.State != domain.StateDiscovered {
		t.Errorf("worker.State = %s, want %s (reload must not auto-start a new service)", rt.State, domain.StateDiscovered)
	}
	if rt.PID != 0 {
		t.Errorf("worker.PID = %d, want 0 (not started)", rt.PID)
	}
}

func TestReloadRestartsRunningChangedService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	root := t.TempDir()
	services := loadReloadFixture(t, root, `
services:
  web:
    command: ["/bin/sh", "-c", "echo v1 tick; sleep 5"]
`)
	sup, err := NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := sup.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	before := waitForState(t, sup, "web", domain.StateRunning, 2*time.Second)

	if err := os.WriteFile(filepath.Join(root, config.FileName), []byte(`
services:
  web:
    command: ["/bin/sh", "-c", "echo v2 tick; sleep 5"]
`), 0o644); err != nil {
		t.Fatalf("writing %s: %v", config.FileName, err)
	}

	if err := sup.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var after domain.ServiceRuntime
	for time.Now().Before(deadline) {
		after, _ = sup.Runtime("web")
		if after.State == domain.StateRunning && after.PID != 0 && after.PID != before.PID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after.State != domain.StateRunning || after.PID == before.PID {
		t.Fatalf("web did not restart with a new PID after reload: before=%+v after=%+v", before, after)
	}

	svc, ok := findServiceByName(sup.Services(), "web")
	if !ok {
		t.Fatal("web missing from Services() after reload")
	}
	if got := lastArg(svc.Command); got != "echo v2 tick; sleep 5" {
		t.Errorf("web.Command = %v, want the v2 command to have taken effect", svc.Command)
	}
}

func TestReloadUpdatesStoppedServiceWithoutStartingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	root := t.TempDir()
	services := loadReloadFixture(t, root, `
services:
  worker:
    command: ["/bin/sh", "-c", "echo v1; sleep 5"]
`)
	sup, err := NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)
	// worker is never started.

	if err := os.WriteFile(filepath.Join(root, config.FileName), []byte(`
services:
  worker:
    command: ["/bin/sh", "-c", "echo v2; sleep 5"]
`), 0o644); err != nil {
		t.Fatalf("writing %s: %v", config.FileName, err)
	}

	if err := sup.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// Give any (wrongly) triggered restart a chance to happen before
	// asserting it didn't.
	time.Sleep(200 * time.Millisecond)
	rt, _ := sup.Runtime("worker")
	if rt.State != domain.StateDiscovered {
		t.Errorf("worker.State = %s, want %s (a service that isn't running must not be started by reload)", rt.State, domain.StateDiscovered)
	}

	svc, ok := findServiceByName(sup.Services(), "worker")
	if !ok {
		t.Fatal("worker missing from Services() after reload")
	}
	if got := lastArg(svc.Command); got != "echo v2; sleep 5" {
		t.Errorf("worker.Command = %v, want the v2 command to have been applied even though it never restarted", svc.Command)
	}
}

func TestReloadLeavesUnchangedRunningServiceAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	root := t.TempDir()
	yaml := `
services:
  web:
    command: ["/bin/sh", "-c", "sleep 5"]
`
	services := loadReloadFixture(t, root, yaml)
	sup, err := NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	if err := sup.Start("web"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	before := waitForState(t, sup, "web", domain.StateRunning, 2*time.Second)

	if err := os.WriteFile(filepath.Join(root, config.FileName), []byte(yaml), 0o644); err != nil {
		t.Fatalf("rewriting %s: %v", config.FileName, err)
	}
	if err := sup.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	after, _ := sup.Runtime("web")
	if after.PID != before.PID {
		t.Errorf("web.PID changed from %d to %d - an unchanged service must not be restarted by reload", before.PID, after.PID)
	}
	if after.State != domain.StateRunning {
		t.Errorf("web.State = %s, want %s", after.State, domain.StateRunning)
	}
}

func TestReloadErrorsWhenConfigFileMissing(t *testing.T) {
	root := t.TempDir()
	sup, err := NewSupervisor(root, []domain.Service{{Name: "api"}})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)
	// No .godev.yaml written to root at all.

	if err := sup.Reload(); err == nil {
		t.Fatal("expected an error reloading with no .godev.yaml present")
	}
}

func findServiceByName(services []domain.Service, name string) (domain.Service, bool) {
	for _, s := range services {
		if s.Name == name {
			return s, true
		}
	}
	return domain.Service{}, false
}

// lastArg returns a command slice's final element (the shell -c
// script), for asserting which version of a changed command actually
// took effect.
func lastArg(command []string) string {
	if len(command) == 0 {
		return ""
	}
	return command[len(command)-1]
}
