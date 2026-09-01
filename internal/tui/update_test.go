package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/logs"
)

// scrollableTestModel is groupedTestModel sized and populated with
// enough log lines that content scroll has real room to move -
// distinguishing "scrolled by N" from "clamped to whatever tiny
// maxScroll a 0x0 model happens to have" needs actual content.
func scrollableTestModel(t *testing.T) Model {
	t.Helper()
	m := groupedTestModel(t)
	m.width, m.height = 100, 30
	for i := 0; i < 40; i++ {
		m.logLines = append(m.logLines, logLine{service: "api", stream: logs.StreamStdout, time: time.Now(), text: "line"})
	}
	return m
}

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
	m := scrollableTestModel(t)
	overContent := m.sidebarWidth() + 5

	next, _ := m.handleMouse(tea.MouseEvent{X: overContent, Button: tea.MouseButtonWheelUp})
	got := next.(Model)
	if got.scroll != vWheelStep {
		t.Fatalf("scroll after wheel up = %d, want %d", got.scroll, vWheelStep)
	}

	next, _ = got.handleMouse(tea.MouseEvent{X: overContent, Button: tea.MouseButtonWheelDown})
	got = next.(Model)
	if got.scroll != 0 {
		t.Fatalf("scroll after wheel down = %d, want 0", got.scroll)
	}

	next, _ = m.handleMouse(tea.MouseEvent{X: overContent, Button: tea.MouseButtonWheelRight})
	got = next.(Model)
	if got.hScroll != hWheelStep {
		t.Fatalf("hScroll after wheel right = %d, want %d", got.hScroll, hWheelStep)
	}
}

// TestMouseWheelScrollsWhicheverPaneItsOver is the actual feature
// this session added: wheeling over the sidebar's column moves the
// sidebar, wheeling over the content pane moves the content - each
// leaving the other's scroll position untouched, regardless of which
// pane happens to be "selected" or focused.
func TestMouseWheelScrollsWhicheverPaneItsOver(t *testing.T) {
	m := scrollableTestModel(t)
	// Enough sidebar rows to actually have somewhere to scroll to.
	for i := 0; i < 20; i++ {
		m.services = append(m.services, m.services[0])
	}
	overSidebar := m.sidebarWidth() - 1
	overContent := m.sidebarWidth() + 5

	next, _ := m.handleMouse(tea.MouseEvent{X: overSidebar, Button: tea.MouseButtonWheelDown})
	got := next.(Model)
	if got.sidebarScroll == 0 {
		t.Fatalf("wheel down over the sidebar should move sidebarScroll, got %d", got.sidebarScroll)
	}
	if got.scroll != 0 {
		t.Errorf("wheel down over the sidebar should not touch the content pane's scroll, got %d", got.scroll)
	}

	next, _ = m.handleMouse(tea.MouseEvent{X: overContent, Button: tea.MouseButtonWheelUp})
	got = next.(Model)
	if got.scroll == 0 {
		t.Fatalf("wheel up over the content pane should move scroll, got %d", got.scroll)
	}
	if got.sidebarScroll != 0 {
		t.Errorf("wheel up over the content pane should not touch the sidebar's scroll, got %d", got.sidebarScroll)
	}
}

// TestScrollClampsToContentLengthAndUnwindsImmediately guards the
// actual bug report: pgup/wheel-up used to have no upper bound, so
// enough of them scrolled past the earliest log line into blank
// space, and - since only rendering clamped, not the stored m.scroll
// itself - a single wheel-down back barely moved the view because it
// was unwinding a huge, invisible overshoot rather than the visible
// scroll position.
func TestScrollClampsToContentLengthAndUnwindsImmediately(t *testing.T) {
	m := scrollableTestModel(t)
	overContent := m.sidebarWidth() + 5

	for i := 0; i < 50; i++ {
		next, _ := m.handleMouse(tea.MouseEvent{X: overContent, Button: tea.MouseButtonWheelUp})
		m = next.(Model)
	}
	max := m.maxScroll()
	if m.scroll != max {
		t.Fatalf("scroll after over-scrolling = %d, want clamped to maxScroll = %d", m.scroll, max)
	}

	next, _ := m.handleMouse(tea.MouseEvent{X: overContent, Button: tea.MouseButtonWheelDown})
	got := next.(Model)
	if want := max - vWheelStep; got.scroll != want {
		t.Fatalf("scroll after one wheel down from the clamped top = %d, want %d (should respond immediately, not unwind an invisible overshoot)", got.scroll, want)
	}
}

func TestCopyLogsKeyAppendsConfirmation(t *testing.T) {
	m := groupedTestModel(t)
	m.view = ViewLogs
	m.logLines = []logLine{
		{service: "api", stream: logs.StreamStdout, time: time.Now(), text: "hello"},
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	got := next.(Model)

	if len(got.logLines) != 2 {
		t.Fatalf("logLines after y = %d, want 2 (original + confirmation)", len(got.logLines))
	}
	last := got.logLines[len(got.logLines)-1]
	if last.stream != logs.StreamSystem || !strings.Contains(last.text, "copied") {
		t.Errorf("expected a system confirmation line mentioning \"copied\", got %+v", last)
	}
}

func TestPlainLogTextRespectsScope(t *testing.T) {
	m := groupedTestModel(t)
	m.logLines = []logLine{
		{service: "api", stream: logs.StreamStdout, time: time.Now(), text: "api line"},
		{service: "worker", stream: logs.StreamStdout, time: time.Now(), text: "worker line"},
	}

	text, n := m.plainLogText()
	if n != 2 || !strings.Contains(text, "[api]") || !strings.Contains(text, "[worker]") {
		t.Fatalf("unscoped plainLogText = %q (n=%d), want both lines with [service] tags", text, n)
	}

	m.logScope = "worker"
	text, n = m.plainLogText()
	if n != 1 || strings.Contains(text, "api line") || !strings.Contains(text, "worker line") {
		t.Fatalf("scoped plainLogText = %q (n=%d), want only worker's line", text, n)
	}
	if strings.Contains(text, "[worker]") {
		t.Errorf("scoped plainLogText should not repeat the [service] tag, got %q", text)
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
