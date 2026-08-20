package tui

import (
	"testing"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
)

func groupedTestModel(t *testing.T) Model {
	t.Helper()
	sup, err := application.NewSupervisor(t.TempDir(), []domain.Service{
		{Name: "scheduler"}, // ungrouped
		{Name: "api", Group: []string{"core"}},
		{Name: "worker", Group: []string{"core"}},
		{Name: "web", Group: []string{"frontend"}},
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	return New(sup, "proj")
}

func TestGroupedRowsUngroupedFirstThenGroupsInFirstSeenOrder(t *testing.T) {
	m := groupedTestModel(t)
	rows := m.groupedRows()

	var got []string
	for _, r := range rows {
		if r.IsHeader {
			got = append(got, "H:"+r.Header)
		} else {
			got = append(got, "S:"+m.services[r.ServiceIndex].Name)
		}
	}

	want := []string{"S:scheduler", "H:core", "S:api", "S:worker", "H:frontend", "S:web"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestInitialSelectionIsFirstVisibleRow(t *testing.T) {
	sup, err := application.NewSupervisor(t.TempDir(), []domain.Service{
		{Name: "api", Group: []string{"core"}}, // index 0, but grouped
		{Name: "scheduler"},                    // index 1, ungrouped - should render first
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	m := New(sup, "proj")
	svc, ok := m.selectedService()
	if !ok || svc.Name != "scheduler" {
		t.Fatalf("initial selection = %+v, want scheduler (first visually-rendered row)", svc)
	}
}

func TestAdjacentSelectionSkipsGroupHeaders(t *testing.T) {
	m := groupedTestModel(t)
	// Order per TestGroupedRowsUngroupedFirstThenGroupsInFirstSeenOrder:
	// scheduler, [core] api, worker, [frontend] web
	m.selected = indexByName(m, "scheduler")

	next, ok := m.adjacentSelection(1)
	if !ok || m.services[next].Name != "api" {
		t.Fatalf("down from scheduler = %v, want api", nameOrNil(m, next, ok))
	}

	m.selected = next
	next, ok = m.adjacentSelection(1)
	if !ok || m.services[next].Name != "worker" {
		t.Fatalf("down from api = %v, want worker", nameOrNil(m, next, ok))
	}

	m.selected = next
	next, ok = m.adjacentSelection(1)
	if !ok || m.services[next].Name != "web" {
		t.Fatalf("down from worker = %v, want web (skipping the frontend header)", nameOrNil(m, next, ok))
	}

	m.selected = next
	if _, ok := m.adjacentSelection(1); ok {
		t.Fatalf("down from the last row should stay put (ok=false), got a valid next index")
	}

	m.selected = indexByName(m, "scheduler")
	if _, ok := m.adjacentSelection(-1); ok {
		t.Fatalf("up from the first row should stay put (ok=false), got a valid next index")
	}
}

func indexByName(m Model, name string) int {
	for i, s := range m.services {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func nameOrNil(m Model, idx int, ok bool) string {
	if !ok {
		return "<none>"
	}
	return m.services[idx].Name
}
