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

// collectStartOrder subscribes to sup's events and returns the order
// services actually finished starting in (EventServiceStarted or
// EventServiceCrashed), failing the test the instant a second
// service's EventServiceStarting arrives while an earlier one is
// still mid-launch (hasn't yet published its own started/crashed) -
// that overlap is exactly what a sequential start must never allow.
func collectStartOrder(t *testing.T, eventsCh <-chan Event, want int, timeout time.Duration) []string {
	t.Helper()
	var order []string
	pending := map[string]bool{}
	deadline := time.Now().Add(timeout)
	for len(order) < want {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for %d services to start; got order=%v", want, order)
		}
		select {
		case e := <-eventsCh:
			switch e.Type {
			case EventServiceStarting:
				if len(pending) > 0 {
					t.Fatalf("service %q began starting while another service was still mid-launch (pending=%v) - starts must be sequential, not overlapping", e.Service, pending)
				}
				pending[e.Service] = true
			case EventServiceStarted, EventServiceCrashed:
				delete(pending, e.Service)
				order = append(order, e.Service)
			}
		case <-time.After(remaining):
			t.Fatalf("timed out waiting for %d services to start; got order=%v", want, order)
		}
	}
	return order
}

func TestStartServicesRunsSequentially(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	root := t.TempDir()
	names := []string{"c", "a", "b"} // deliberately not alphabetical
	var services []domain.Service
	for _, n := range names {
		services = append(services, domain.Service{
			Name:      n,
			Command:   []string{"/bin/sh", "-c", "sleep 5"},
			Directory: root,
		})
	}
	sup, err := NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	eventsCh, cancel := sup.SubscribeEvents(64)
	defer cancel()

	sup.StartServices(names)

	order := collectStartOrder(t, eventsCh, len(names), 5*time.Second)
	for i, n := range names {
		if order[i] != n {
			t.Errorf("start order[%d] = %q, want %q (must follow the given order): %v", i, order[i], n, order)
		}
	}
}

func TestStartAllStartsAutoStartServicesInDeclaredOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	root := t.TempDir()
	// zebra before apple: proves order tracks the file, not a sorted
	// or alphabetical fallback.
	yaml := `
services:
  zebra:
    command: ["/bin/sh", "-c", "sleep 5"]
  apple:
    command: ["/bin/sh", "-c", "sleep 5"]
  mango:
    command: ["/bin/sh", "-c", "sleep 5"]
`
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

	sup, err := NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	eventsCh, cancel := sup.SubscribeEvents(64)
	defer cancel()

	sup.StartAll()

	order := collectStartOrder(t, eventsCh, len(services), 5*time.Second)
	want := []string{"zebra", "apple", "mango"}
	for i, n := range want {
		if order[i] != n {
			t.Errorf("start order[%d] = %q, want %q (.godev.yaml's declared order): %v", i, order[i], n, order)
		}
	}
}
