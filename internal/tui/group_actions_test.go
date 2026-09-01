package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// fakeSource is a minimal tui.Source test double that records calls
// instead of touching any real Supervisor/process - the "S"/"R" group
// keybinds only need to be checked for *which* names they resolve and
// dispatch, not for actual process behavior (that's
// application.Supervisor's own, already-tested job).
type fakeSource struct {
	services []domain.Service
	runtimes map[string]domain.ServiceRuntime

	started   [][]string
	stopped   [][]string
	restarted [][]string
}

func (f *fakeSource) Services() []domain.Service { return f.services }
func (f *fakeSource) Runtime(name string) (domain.ServiceRuntime, bool) {
	rt, ok := f.runtimes[name]
	return rt, ok
}
func (f *fakeSource) BuildInfo(string) (application.BuildInfo, bool) {
	return application.BuildInfo{}, false
}
func (f *fakeSource) WatchActive() bool { return false }
func (f *fakeSource) SubscribeEvents(int) (<-chan application.Event, func()) {
	return make(chan application.Event), func() {}
}
func (f *fakeSource) SubscribeLogs(int) (<-chan logs.Event, func()) {
	return make(chan logs.Event), func() {}
}
func (f *fakeSource) ClearLogs()              {}
func (f *fakeSource) Start(string) error      { return nil }
func (f *fakeSource) Stop(string) error       { return nil }
func (f *fakeSource) Restart(string) error    { return nil }
func (f *fakeSource) StartDebug(string) error { return nil }
func (f *fakeSource) StopDebug(string) error  { return nil }
func (f *fakeSource) Reload() error           { return nil }

func (f *fakeSource) StartServices(names []string)   { f.started = append(f.started, names) }
func (f *fakeSource) StopServices(names []string)    { f.stopped = append(f.stopped, names) }
func (f *fakeSource) RestartServices(names []string) { f.restarted = append(f.restarted, names) }

func newFakeGroupModel() (Model, *fakeSource) {
	src := &fakeSource{
		services: []domain.Service{
			{Name: "scheduler"}, // ungrouped
			{Name: "api", Group: []string{"core"}},
			{Name: "worker", Group: []string{"core"}},
			{Name: "web", Group: []string{"frontend"}},
		},
		runtimes: map[string]domain.ServiceRuntime{
			"api":    {State: domain.StateRunning},
			"worker": {State: domain.StateStopped},
		},
	}
	return New(src, "proj"), src
}

func TestSelectedGroupMembersReturnsWholeGroup(t *testing.T) {
	m, _ := newFakeGroupModel()

	m.selected = 1 // api
	names, ok := m.selectedGroupMembers()
	if !ok || len(names) != 2 || names[0] != "api" || names[1] != "worker" {
		t.Errorf("selectedGroupMembers(api) = %v, ok=%v, want [api worker], true", names, ok)
	}

	m.selected = 0 // scheduler, ungrouped
	names, ok = m.selectedGroupMembers()
	if !ok || len(names) != 1 || names[0] != "scheduler" {
		t.Errorf("selectedGroupMembers(scheduler) = %v, ok=%v, want [scheduler], true", names, ok)
	}
}

func TestGroupToggleKeyStopsWholeGroupWhenAnyMemberRunning(t *testing.T) {
	m, src := newFakeGroupModel()
	m.selected = 1 // api is Running, worker (same group) is Stopped

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	_ = next.(Model)

	if len(src.stopped) != 1 {
		t.Fatalf("StopServices calls = %d, want 1 (any member running -> stop the whole group)", len(src.stopped))
	}
	if len(src.started) != 0 {
		t.Errorf("StartServices should not have been called, got %v", src.started)
	}
	got := src.stopped[0]
	if len(got) != 2 || got[0] != "api" || got[1] != "worker" {
		t.Errorf("StopServices names = %v, want [api worker]", got)
	}
}

func TestGroupToggleKeyStartsWholeGroupWhenNoneRunning(t *testing.T) {
	src := &fakeSource{
		services: []domain.Service{
			{Name: "api", Group: []string{"core"}},
			{Name: "worker", Group: []string{"core"}},
		},
		runtimes: map[string]domain.ServiceRuntime{
			"api":    {State: domain.StateStopped},
			"worker": {State: domain.StateDiscovered},
		},
	}
	m := New(src, "proj")
	m.selected = 0

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	_ = next.(Model)

	if len(src.started) != 1 {
		t.Fatalf("StartServices calls = %d, want 1", len(src.started))
	}
	if len(src.stopped) != 0 {
		t.Errorf("StopServices should not have been called, got %v", src.stopped)
	}
}

func TestGroupRestartKeyRestartsWholeGroup(t *testing.T) {
	m, src := newFakeGroupModel()
	m.selected = 2 // worker, same group as api

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	_ = next.(Model)

	if len(src.restarted) != 1 {
		t.Fatalf("RestartServices calls = %d, want 1", len(src.restarted))
	}
	got := src.restarted[0]
	if len(got) != 2 || got[0] != "api" || got[1] != "worker" {
		t.Errorf("RestartServices names = %v, want [api worker]", got)
	}
}
