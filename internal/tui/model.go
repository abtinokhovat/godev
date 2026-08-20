// Package tui implements godev's terminal UI: a narrow, always-visible
// sidebar for service control/status plus a log-dominant content area
// that switches between four views (Logs, Build, Problems, Debug). It
// only ever calls into application.Supervisor's public methods - it
// never touches processes, builds, or Delve directly.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// ViewMode selects what the content pane (right of the sidebar) shows.
// The sidebar itself never changes shape with this - it's always
// services + runtime + debugger status, per the "left = control and
// state, right = what is happening" split.
type ViewMode int

const (
	ViewLogs ViewMode = iota
	ViewBuild
	ViewProblems
	ViewDebugger
)

type logLine struct {
	service string
	stream  logs.Stream
	time    time.Time
	text    string
}

// Model is the Bubble Tea model driving the whole TUI.
type Model struct {
	sup     Source
	project string

	services []domain.Service
	runtimes map[string]domain.ServiceRuntime

	selected int
	view     ViewMode
	logScope string // "" = all services; otherwise a service name
	expanded bool   // Tab: sidebar shows an extra detail section for the selection
	scroll   int    // lines scrolled up from the bottom of the content pane; 0 = follow tail

	logLines    []logLine
	maxLogLines int

	// autoBuildView remembers that we switched to the Build view
	// automatically (because the selected service started building) so
	// we know to switch back once the build settles, per the "shown
	// automatically during build ... then automatically return to the
	// logs" behavior.
	autoBuildView  bool
	autoReturnView ViewMode
	returnAt       time.Time

	width, height int
	status        string

	eventsCh <-chan application.Event
	logsCh   <-chan logs.Event

	quitting bool
}

// New builds the initial Model for a Source already populated with
// discovered/configured services.
func New(sup Source, project string) Model {
	services := sup.Services()
	runtimes := make(map[string]domain.ServiceRuntime, len(services))
	for _, svc := range services {
		if rt, ok := sup.Runtime(svc.Name); ok {
			runtimes[svc.Name] = rt
		}
	}
	eventsCh, _ := sup.SubscribeEvents(64)
	logsCh, _ := sup.SubscribeLogs(256)

	m := Model{
		sup:         sup,
		project:     project,
		services:    services,
		runtimes:    runtimes,
		maxLogLines: 2000,
		eventsCh:    eventsCh,
		logsCh:      logsCh,
	}
	// Ungrouped services always render first (see groupedRows), so
	// index 0 is usually already the top row - but if service 0 happens
	// to belong to a group, start the selection on whatever the sidebar
	// actually shows first instead of a row that isn't visually at top.
	if order := m.selectableOrder(); len(order) > 0 {
		m.selected = order[0]
	}
	return m
}

type eventMsg application.Event
type logMsg logs.Event
type tickMsg time.Time
type returnFromBuildMsg struct{}

func listenEvents(ch <-chan application.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg(e)
	}
}

func listenLogs(ch <-chan logs.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return logMsg(e)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(listenEvents(m.eventsCh), listenLogs(m.logsCh), tickCmd())
}

func (m Model) selectedService() (domain.Service, bool) {
	if m.selected < 0 || m.selected >= len(m.services) {
		return domain.Service{}, false
	}
	return m.services[m.selected], true
}

// groupRow is one row of the sidebar's grouped service tree: either a
// non-selectable group header (Header set, ServiceIndex ignored) or a
// selectable service row (ServiceIndex into m.services).
type groupRow struct {
	Header       string
	ServiceIndex int
	IsHeader     bool
}

// groupedRows lays out m.services for the sidebar: ungrouped services
// first (today's flat list, unchanged for projects with no groups),
// then each group's services under a header, groups in order of first
// appearance (not sorted - so a project's own service order still
// drives what the user sees, per how discovery/config naturally order
// things).
func (m Model) groupedRows() []groupRow {
	var rows []groupRow
	for i, svc := range m.services {
		if len(svc.Group) == 0 {
			rows = append(rows, groupRow{ServiceIndex: i})
		}
	}

	var groupOrder []string
	groupIndices := map[string][]int{}
	for i, svc := range m.services {
		if len(svc.Group) == 0 {
			continue
		}
		g := svc.Group[0]
		if _, ok := groupIndices[g]; !ok {
			groupOrder = append(groupOrder, g)
		}
		groupIndices[g] = append(groupIndices[g], i)
	}
	for _, g := range groupOrder {
		rows = append(rows, groupRow{Header: g, IsHeader: true})
		for _, i := range groupIndices[g] {
			rows = append(rows, groupRow{ServiceIndex: i})
		}
	}
	return rows
}

// selectableOrder returns groupedRows' service indices only (headers
// excluded), in sidebar order - what up/down navigation moves through.
func (m Model) selectableOrder() []int {
	rows := m.groupedRows()
	order := make([]int, 0, len(rows))
	for _, r := range rows {
		if !r.IsHeader {
			order = append(order, r.ServiceIndex)
		}
	}
	return order
}

// adjacentSelection returns the service index delta steps away from
// the current selection in sidebar (grouped) order, e.g. delta=1 for
// "down", delta=-1 for "up". ok is false at either end of the list.
func (m Model) adjacentSelection(delta int) (int, bool) {
	order := m.selectableOrder()
	pos := -1
	for i, idx := range order {
		if idx == m.selected {
			pos = i
			break
		}
	}
	if pos == -1 {
		if len(order) == 0 {
			return 0, false
		}
		return order[0], true
	}
	newPos := pos + delta
	if newPos < 0 || newPos >= len(order) {
		return 0, false
	}
	return order[newPos], true
}
