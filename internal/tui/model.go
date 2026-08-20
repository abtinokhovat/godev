// Package tui implements godev's terminal UI (section 41-43). It only
// ever calls into application.Supervisor's public methods - it never
// touches processes, builds, or Delve directly (see plan section 3's
// "TUI must never directly manage processes" rule).
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

type logLine struct {
	service string
	stream  logs.Stream
	text    string
}

// Model is the Bubble Tea model driving the whole TUI.
type Model struct {
	sup     *application.Supervisor
	project string

	services []domain.Service
	runtimes map[string]domain.ServiceRuntime

	selected int
	detail   bool

	logLines    []logLine
	maxLogLines int
	logScroll   int // 0 = follow tail; >0 = lines scrolled up from bottom

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
