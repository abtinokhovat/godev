package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

func testModel(t *testing.T) Model {
	t.Helper()
	sup, err := application.NewSupervisor(t.TempDir(), []domain.Service{
		{Name: "api", Package: "./cmd/api", Directory: t.TempDir(), Args: []string{"--port", "8080"},
			Env: map[string]string{"LOG_LEVEL": "debug"}, AutoRestart: true, HotReload: true, Group: []string{"core"}},
		{Name: "worker", Package: "./cmd/worker", Directory: t.TempDir(), AutoRestart: true, Group: []string{"core"}},
		{Name: "scheduler", Package: "./cmd/scheduler", Directory: t.TempDir()},
		{Name: "web", Command: []string{"npm", "run", "dev"}, Directory: t.TempDir(), Group: []string{"frontend"}},
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	m := New(sup, "my-project")

	m.runtimes["api"] = domain.ServiceRuntime{State: domain.StateRunning, PID: 111, StartedAt: time.Now()}
	m.runtimes["worker"] = domain.ServiceRuntime{State: domain.StateCrashed, LastError: "exit status 1\nstack trace line"}
	m.runtimes["scheduler"] = domain.ServiceRuntime{State: domain.StateBuildFailed, LastError: "cmd/scheduler/main.go:10: undefined: Foo"}

	m.logLines = []logLine{
		{service: "api", stream: logs.StreamStdout, time: time.Now(), text: "GET /users 200"},
		{service: "worker", stream: logs.StreamStderr, time: time.Now(), text: "job failed"},
		{service: "api", stream: logs.StreamSystem, time: time.Now(), text: "restarting..."},
	}
	return m
}

func TestViewRendersWithoutPanicAcrossModesAndSizes(t *testing.T) {
	sizes := []struct{ w, h int }{
		{20, 5}, {40, 10}, {80, 24}, {120, 40}, {8, 3},
	}
	views := []ViewMode{ViewLogs, ViewBuild, ViewProblems, ViewDebugger}

	for _, sz := range sizes {
		for _, v := range views {
			for _, expanded := range []bool{false, true} {
				m := testModel(t)
				m.width, m.height = sz.w, sz.h
				m.view = v
				m.expanded = expanded

				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("View() panicked at size=%dx%d view=%v expanded=%v: %v", sz.w, sz.h, v, expanded, r)
						}
					}()
					out := m.View()
					if out == "" {
						t.Fatalf("View() returned empty string at size=%dx%d view=%v", sz.w, sz.h, v)
					}
				}()
			}
		}
	}
}

func TestViewLogsScopedToSelectedService(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	m.view = ViewLogs
	m.logScope = "worker"

	out := m.View()
	if !strings.Contains(out, "job failed") {
		t.Fatalf("expected worker's log line in scoped view, got:\n%s", out)
	}
	if strings.Contains(out, "GET /users 200") {
		t.Fatalf("api's log line should be filtered out when scoped to worker, got:\n%s", out)
	}
}

func TestProblemsViewListsCrashedAndBuildFailed(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 100, 30
	m.view = ViewProblems

	out := m.View()
	if !strings.Contains(out, "worker") || !strings.Contains(out, "scheduler") {
		t.Fatalf("expected both crashed and build-failed services listed, got:\n%s", out)
	}
}

func TestZeroSizeDoesNotPanic(t *testing.T) {
	m := testModel(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View() panicked at zero size: %v", r)
		}
	}()
	_ = m.View()
}
