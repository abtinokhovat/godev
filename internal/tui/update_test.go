package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEscActsLikeABackToAllLogs(t *testing.T) {
	m := groupedTestModel(t)
	m.logScope = "api"
	m.view = ViewBuild
	m.scroll = 5
	m.hScroll = 5

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(Model)

	if got.logScope != "" {
		t.Errorf("logScope = %q, want empty (esc should scope back to all logs)", got.logScope)
	}
	if got.view != ViewLogs {
		t.Errorf("view = %v, want ViewLogs", got.view)
	}
	if got.scroll != 0 || got.hScroll != 0 {
		t.Errorf("scroll/hScroll = %d/%d, want 0/0", got.scroll, got.hScroll)
	}
}

func TestLeftRightScrollHorizontally(t *testing.T) {
	m := groupedTestModel(t)

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	got := next.(Model)
	if got.hScroll != hScrollStep {
		t.Fatalf("hScroll after right = %d, want %d", got.hScroll, hScrollStep)
	}

	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	got = next.(Model)
	if got.hScroll != 0 {
		t.Fatalf("hScroll after left = %d, want 0", got.hScroll)
	}

	// Can't scroll past the left edge.
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	got = next.(Model)
	if got.hScroll != 0 {
		t.Fatalf("hScroll after left at 0 = %d, want clamped to 0", got.hScroll)
	}
}

func TestMouseWheelScrolls(t *testing.T) {
	m := groupedTestModel(t)

	next, _ := m.handleMouse(tea.MouseEvent{Button: tea.MouseButtonWheelUp})
	got := next.(Model)
	if got.scroll != vWheelStep {
		t.Fatalf("scroll after wheel up = %d, want %d", got.scroll, vWheelStep)
	}

	next, _ = got.handleMouse(tea.MouseEvent{Button: tea.MouseButtonWheelDown})
	got = next.(Model)
	if got.scroll != 0 {
		t.Fatalf("scroll after wheel down = %d, want 0", got.scroll)
	}

	next, _ = m.handleMouse(tea.MouseEvent{Button: tea.MouseButtonWheelRight})
	got = next.(Model)
	if got.hScroll != hWheelStep {
		t.Fatalf("hScroll after wheel right = %d, want %d", got.hScroll, hWheelStep)
	}
}

func TestMouseClickSelectsSidebarService(t *testing.T) {
	m := groupedTestModel(t)
	m.width, m.height = 100, 30

	// Screen layout: y=0 is the top project header bar; the sidebar
	// body starts at y=1 with "SERVICES" (y=1) and a blank line (y=2),
	// then each groupedRows() row occupies 2 screen lines. Order per
	// TestGroupedRowsUngroupedFirstThenGroupsInFirstSeenOrder:
	// scheduler (y=3-4), [core] header (y=5-6), api (y=7-8), worker (y=9-10).
	svc, ok := m.serviceAtScreenPos(0, 3) // scheduler's name line
	if !ok || m.services[svc].Name != "scheduler" {
		t.Fatalf("serviceAtScreenPos(0,3) = %v,%v, want scheduler", svc, ok)
	}
	svc, ok = m.serviceAtScreenPos(0, 4) // scheduler's status line - still a hit
	if !ok || m.services[svc].Name != "scheduler" {
		t.Fatalf("serviceAtScreenPos(0,4) = %v,%v, want scheduler", svc, ok)
	}
	svc, ok = m.serviceAtScreenPos(0, 7) // api's name line
	if !ok || m.services[svc].Name != "api" {
		t.Fatalf("serviceAtScreenPos(0,7) = %v,%v, want api", svc, ok)
	}

	// The "core" group header (y=5-6) isn't a selectable service.
	rows := m.groupedRows()
	if !rows[1].IsHeader {
		t.Fatalf("test setup assumption broken: rows[1] should be the core header")
	}
	if _, ok := m.serviceAtScreenPos(0, 5); ok {
		t.Fatalf("clicking a group header row should not select a service")
	}

	if _, ok := m.serviceAtScreenPos(0, 0); ok {
		t.Fatalf("clicking the top project header bar should never hit a service")
	}
	if _, ok := m.serviceAtScreenPos(m.sidebarWidth()+5, 3); ok {
		t.Fatalf("clicking in the content pane should never hit a sidebar service")
	}

	next, _ := m.handleMouse(tea.MouseEvent{X: 0, Y: 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := next.(Model)
	if m.services[got.selected].Name != "api" {
		t.Fatalf("selected after click = %q, want api", m.services[got.selected].Name)
	}
	if got.logScope != "api" || got.view != ViewLogs {
		t.Fatalf("click on a service should focus its logs like enter: logScope=%q view=%v, want logScope=api view=ViewLogs", got.logScope, got.view)
	}
}
