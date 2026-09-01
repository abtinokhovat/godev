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

	selected      int
	view          ViewMode
	logScope      string // "" = all services; otherwise a service name
	expanded      bool   // Tab: sidebar shows an extra detail section for the selection
	scroll        int    // lines scrolled up from the bottom of the content pane; 0 = follow tail
	hScroll       int    // columns scrolled right in the content pane; 0 = flush left
	sidebarScroll int    // index into groupedRows() of the first visible row; kept in view of m.selected

	logLines    []logLine
	maxLogLines int

	// commandMode/commandInput back the ":" prompt - typing space-
	// separated service/group names and pressing enter starts exactly
	// those (see domain.ResolveTargets), without needing to navigate
	// the sidebar to each one first. While commandMode is true,
	// handleKey routes every key to handleCommandInputKey instead of
	// its normal single-key dispatch.
	commandMode  bool
	commandInput string

	// autoBuildView remembers that we switched to the Build view
	// automatically (because the selected service started building) so
	// we know to switch back once the build settles, per the "shown
	// automatically during build ... then automatically return to the
	// logs" behavior.
	autoBuildView  bool
	autoReturnView ViewMode
	returnAt       time.Time

	width, height int

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

// selectedGroupMembers returns the names of every service the sidebar
// lists under the selected service's group header (its primary group,
// see domain.PrimaryGroups), in m.services order - exactly what a
// "start/stop/restart this whole group" keybind should act on. An
// ungrouped selected service has no such header, so it acts alone:
// the returned slice is just that one service's name.
func (m Model) selectedGroupMembers() ([]string, bool) {
	svc, ok := m.selectedService()
	if !ok {
		return nil, false
	}
	primary := domain.PrimaryGroups(m.services)
	group := primary[svc.Name]
	if group == "" {
		return []string{svc.Name}, true
	}
	var names []string
	for _, s := range m.services {
		if primary[s.Name] == group {
			names = append(names, s.Name)
		}
	}
	return names, true
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
// things). A service tagged into more than one group (`group: [core,
// test]`, for `godev run <target>...` convenience) is displayed under
// whichever is its smallest/most-specific one - see
// domain.PrimaryGroups - never duplicated across headers.
func (m Model) groupedRows() []groupRow {
	primary := domain.PrimaryGroups(m.services)

	var rows []groupRow
	for i, svc := range m.services {
		if primary[svc.Name] == "" {
			rows = append(rows, groupRow{ServiceIndex: i})
		}
	}

	var groupOrder []string
	groupIndices := map[string][]int{}
	for i, svc := range m.services {
		g, ok := primary[svc.Name]
		if !ok {
			continue
		}
		if _, seen := groupIndices[g]; !seen {
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

// sidebarRowIndex returns m.selected's position within rows, or -1 if
// not present (e.g. no services).
func (m Model) sidebarRowIndex(rows []groupRow) int {
	for i, r := range rows {
		if !r.IsHeader && r.ServiceIndex == m.selected {
			return i
		}
	}
	return -1
}

// sidebarMaxVisibleRows computes how many groupedRows() rows fit
// alongside the sidebar's fixed RUNTIME/DEBUGGER/detail footer, given
// the current terminal height. Shared by rendering (to pick the
// visible window) and key handling (to scroll that window to follow
// the selection) so the two can never disagree.
func (m Model) sidebarMaxVisibleRows() int {
	bodyHeight := m.height - 2 // header + footer
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	footer := m.renderSidebarFooter(m.sidebarWidth())
	available := bodyHeight - 2 - len(footer) // 2 = "SERVICES" title + blank line
	maxVisible := available / 2               // every row (service or group header) renders as 2 lines
	if maxVisible < 1 {
		maxVisible = 1
	}
	return maxVisible
}

// sidebarVisibleWindow returns groupedRows() along with the [start,
// end) slice currently visible, given m.sidebarScroll and the
// terminal's height - the single source of truth for "what's on
// screen" shared by rendering (sidebar.go) and mouse hit-testing
// (update.go), so the two can never disagree about which row a given
// screen line belongs to.
func (m Model) sidebarVisibleWindow() (rows []groupRow, start, end int) {
	rows = m.groupedRows()
	maxVisible := m.sidebarMaxVisibleRows()

	start = m.sidebarScroll
	if start < 0 {
		start = 0
	}
	if maxStart := len(rows) - maxVisible; start > maxStart && maxStart >= 0 {
		start = maxStart
	}
	end = start + maxVisible
	if end > len(rows) {
		end = len(rows)
	}
	return rows, start, end
}

// scrollSidebarToSelection clamps m.sidebarScroll so the current
// selection's row stays within the visible window - called whenever
// the selection changes or the available height might have (resize,
// expanding/collapsing the detail section).
func (m *Model) scrollSidebarToSelection() {
	rows := m.groupedRows()
	maxVisible := m.sidebarMaxVisibleRows()

	if idx := m.sidebarRowIndex(rows); idx >= 0 {
		if idx < m.sidebarScroll {
			m.sidebarScroll = idx
		} else if idx >= m.sidebarScroll+maxVisible {
			m.sidebarScroll = idx - maxVisible + 1
		}
	}
	m.clampSidebarScroll()
}

// clampSidebarScroll bounds m.sidebarScroll to the sidebar's actual
// row count - the tail half of scrollSidebarToSelection's own
// clamping, factored out so a direct wheel-driven scroll (which,
// unlike scrollSidebarToSelection, doesn't touch the current
// selection at all - looking around independently of it is the whole
// point) can clamp itself the same way without forcing the selection
// into view.
func (m *Model) clampSidebarScroll() {
	rows := m.groupedRows()
	maxVisible := m.sidebarMaxVisibleRows()
	maxStart := len(rows) - maxVisible
	if maxStart < 0 {
		maxStart = 0
	}
	if m.sidebarScroll > maxStart {
		m.sidebarScroll = maxStart
	}
	if m.sidebarScroll < 0 {
		m.sidebarScroll = 0
	}
}
