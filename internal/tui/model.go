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
	sup     *application.Supervisor
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

// New builds the initial Model for a supervisor already populated with
// discovered/configured services.
func New(sup *application.Supervisor, project string) Model {
	services := sup.Services()
	runtimes := make(map[string]domain.ServiceRuntime, len(services))
	for _, svc := range services {
		if rt, ok := sup.Runtime(svc.Name); ok {
			runtimes[svc.Name] = rt
		}
	}
	eventsCh, _ := sup.Events().Subscribe(64)
	logsCh, _ := sup.Logs().Subscribe(256)

	return Model{
		sup:         sup,
		project:     project,
		services:    services,
		runtimes:    runtimes,
		maxLogLines: 2000,
		eventsCh:    eventsCh,
		logsCh:      logsCh,
	}
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
