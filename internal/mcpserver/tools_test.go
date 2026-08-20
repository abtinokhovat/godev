package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

func writeMainPkg(t *testing.T, root string) domain.Service {
	t.Helper()
	dir := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "package main\nimport(\"fmt\";\"time\")\nfunc main(){for{fmt.Println(\"tick\");time.Sleep(time.Hour)}}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return domain.Service{Name: "app", Package: "./cmd/app", Directory: dir, AutoRestart: true}
}

// newTestToolset builds a toolset over a fresh Supervisor rooted at
// root. Pass t.TempDir() directly when a test has no real project root
// of its own (list/get/log tests that never actually build or run
// anything); pass the same root writeMainPkg wrote into when a test
// needs a real, buildable fixture (lifecycle tests) - the Supervisor's
// builder always execs `go build` with this exact directory as its
// working directory, so it must match where go.mod actually lives.
func newTestToolset(t *testing.T, root string, services []domain.Service) *toolset {
	t.Helper()
	sup, err := application.NewSupervisor(root, services)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	t.Cleanup(sup.Shutdown)
	return &toolset{sup: sup}
}

func TestListServicesReportsKindAndGroup(t *testing.T) {
	ts := newTestToolset(t, t.TempDir(), []domain.Service{
		{Name: "api", Package: "./cmd/api", Group: []string{"core"}},
		{Name: "web", Command: []string{"npm", "run", "dev"}, Group: []string{"frontend"}},
	})

	_, out, err := ts.listServices(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("listServices: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(out), out)
	}

	byName := map[string]ServiceSummary{}
	for _, s := range out {
		byName[s.Name] = s
	}
	if byName["api"].Kind != "go" {
		t.Errorf("api.Kind = %q, want go", byName["api"].Kind)
	}
	if byName["web"].Kind != "command" {
		t.Errorf("web.Kind = %q, want command", byName["web"].Kind)
	}
	if len(byName["api"].Group) != 1 || byName["api"].Group[0] != "core" {
		t.Errorf("api.Group = %v, want [core]", byName["api"].Group)
	}
	if byName["api"].State != domain.StateDiscovered.String() {
		t.Errorf("api.State = %q, want %q (never started)", byName["api"].State, domain.StateDiscovered)
	}
}

func TestGetServiceUnknownErrors(t *testing.T) {
	ts := newTestToolset(t, t.TempDir(), []domain.Service{{Name: "api"}})
	_, _, err := ts.getService(context.Background(), nil, serviceNameInput{Name: "nope"})
	if err == nil {
		t.Fatal("expected an error for an unknown service")
	}
}

func TestGetServiceReportsCommandAndDebugAbsence(t *testing.T) {
	ts := newTestToolset(t, t.TempDir(), []domain.Service{
		{Name: "web", Command: []string{"npm", "run", "dev"}, Directory: "/proj/web"},
	})
	_, detail, err := ts.getService(context.Background(), nil, serviceNameInput{Name: "web"})
	if err != nil {
		t.Fatalf("getService: %v", err)
	}
	if detail.Command != "npm run dev" {
		t.Errorf("Command = %q, want %q", detail.Command, "npm run dev")
	}
	if detail.Directory != "/proj/web" {
		t.Errorf("Directory = %q, want /proj/web", detail.Directory)
	}
	if detail.Debug != nil {
		t.Errorf("Debug should be nil for a service that was never debugged, got %+v", detail.Debug)
	}
	if detail.BuildOK != nil {
		t.Errorf("BuildOK should be nil before any build attempt, got %v", *detail.BuildOK)
	}
}

func TestGetLogsFiltersByServiceAndRespectsLimit(t *testing.T) {
	ts := newTestToolset(t, t.TempDir(), []domain.Service{{Name: "api"}, {Name: "worker"}})
	for i := 0; i < 5; i++ {
		ts.sup.Logs().Publish(logs.Event{Service: "api", Message: "api line"})
		ts.sup.Logs().Publish(logs.Event{Service: "worker", Message: "worker line"})
	}

	_, all, err := ts.getLogs(context.Background(), nil, getLogsInput{})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if len(all) != 10 {
		t.Fatalf("got %d lines, want 10", len(all))
	}

	_, apiOnly, err := ts.getLogs(context.Background(), nil, getLogsInput{Name: "api"})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if len(apiOnly) != 5 {
		t.Fatalf("got %d api lines, want 5", len(apiOnly))
	}
	for _, l := range apiOnly {
		if l.Service != "api" {
			t.Errorf("unexpected service %q in api-filtered logs", l.Service)
		}
	}

	_, limited, err := ts.getLogs(context.Background(), nil, getLogsInput{Limit: 3})
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if len(limited) != 3 {
		t.Fatalf("got %d lines, want 3 (limit)", len(limited))
	}
}

func TestActionToolsUnknownServiceErrors(t *testing.T) {
	ts := newTestToolset(t, t.TempDir(), []domain.Service{{Name: "api"}})
	ctx := context.Background()
	const missing = "nope"

	if _, _, err := ts.startService(ctx, nil, serviceNameInput{Name: missing}); err == nil {
		t.Error("startService: expected error for unknown service")
	}
	if _, _, err := ts.stopService(ctx, nil, serviceNameInput{Name: missing}); err == nil {
		t.Error("stopService: expected error for unknown service")
	}
	if _, _, err := ts.restartService(ctx, nil, serviceNameInput{Name: missing}); err == nil {
		t.Error("restartService: expected error for unknown service")
	}
	if _, _, err := ts.startDebug(ctx, nil, serviceNameInput{Name: missing}); err == nil {
		t.Error("startDebug: expected error for unknown service")
	}
	if _, _, err := ts.stopDebug(ctx, nil, serviceNameInput{Name: missing}); err == nil {
		t.Error("stopDebug: expected error for unknown service")
	}
}

func TestStartDebugRejectsCommandBasedService(t *testing.T) {
	ts := newTestToolset(t, t.TempDir(), []domain.Service{
		{Name: "web", Command: []string{"node", "server.js"}},
	})
	_, _, err := ts.startDebug(context.Background(), nil, serviceNameInput{Name: "web"})
	if err == nil {
		t.Fatal("expected an error for debugging a command-based service")
	}
	if !strings.Contains(err.Error(), "command-based") {
		t.Errorf("error %q should explain the service is command-based", err.Error())
	}
}

func TestStartStopServiceLifecycle(t *testing.T) {
	root := t.TempDir()
	svc := writeMainPkg(t, root)
	ts := newTestToolset(t, root, []domain.Service{svc})

	_, started, err := ts.startService(context.Background(), nil, serviceNameInput{Name: "app"})
	if err != nil {
		t.Fatalf("startService: %v", err)
	}
	if started.State != domain.StateRunning.String() {
		t.Fatalf("State = %q after start, want %q", started.State, domain.StateRunning)
	}

	_, detail, err := ts.getService(context.Background(), nil, serviceNameInput{Name: "app"})
	if err != nil {
		t.Fatalf("getService: %v", err)
	}
	if detail.PID == 0 {
		t.Fatal("expected a nonzero PID for a running service")
	}
	if detail.BuildOK == nil || !*detail.BuildOK {
		t.Fatalf("expected a successful build recorded, got %+v", detail.BuildOK)
	}

	deadline := time.Now().Add(3 * time.Second)
	var uptimeSeen bool
	for time.Now().Before(deadline) {
		_, d, _ := ts.getService(context.Background(), nil, serviceNameInput{Name: "app"})
		if d.Uptime != "" {
			uptimeSeen = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !uptimeSeen {
		t.Error("expected Uptime to become non-empty for a running service")
	}

	_, stopped, err := ts.stopService(context.Background(), nil, serviceNameInput{Name: "app"})
	if err != nil {
		t.Fatalf("stopService: %v", err)
	}
	if stopped.State != domain.StateStopped.String() {
		t.Fatalf("State = %q after stop, want %q", stopped.State, domain.StateStopped)
	}
}
