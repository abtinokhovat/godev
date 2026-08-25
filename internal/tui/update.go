package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
)

// buildSettleDelay is how long the Build view stays up after a build
// finishes before auto-returning to whatever view was showing before,
// so the user has a moment to see the result.
const buildSettleDelay = 1200 * time.Millisecond

// hScrollStep is how many columns left/right (or a horizontal wheel
// tick) shifts the content pane per press.
const hScrollStep = 10

// vWheelStep/hWheelStep are smaller than the key-driven steps since a
// single wheel "click" should feel like a nudge, not a page jump.
const vWheelStep = 3
const hWheelStep = 5

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollSidebarToSelection()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(tea.MouseEvent(msg))

	case eventMsg:
		return m.handleEvent(msg)

	case logMsg:
		m.logLines = append(m.logLines, logLine{
			service: msg.Service, stream: msg.Stream, time: msg.Time, text: msg.Message,
		})
		if len(m.logLines) > m.maxLogLines {
			m.logLines = m.logLines[len(m.logLines)-m.maxLogLines:]
		}
		return m, listenLogs(m.logsCh)

	case tickMsg:
		return m, tickCmd()

	case returnFromBuildMsg:
		if m.autoBuildView && m.view == ViewBuild && time.Now().After(m.returnAt) {
			m.view = m.autoReturnView
			m.autoBuildView = false
		}
		return m, nil
	}
	return m, nil
}

// handleEvent must always keep listening for further events - every
// return path has to include listenEvents(m.eventsCh), or the whole
// event stream (and with it every sidebar status update) stalls
// permanently the moment this function returns without it.
func (m Model) handleEvent(e eventMsg) (tea.Model, tea.Cmd) {
	if e.Type == application.EventServiceDiscovered || e.Type == application.EventServiceConfigChanged {
		// A reload (ctrl+r) can add services, or change an existing
		// one's definition (group, command, ...), after the TUI already
		// started - the cached slice from New() needs picking up again.
		// New services are only ever appended, so m.selected stays valid.
		m.services = m.sup.Services()
	}

	for _, svc := range m.services {
		if rt, ok := m.sup.Runtime(svc.Name); ok {
			m.runtimes[svc.Name] = rt
		}
	}

	svc, hasSelection := m.selectedService()
	isSelected := hasSelection && e.Service == svc.Name

	cmds := []tea.Cmd{listenEvents(m.eventsCh)}

	switch e.Type {
	case application.EventBuildStarted:
		if isSelected && m.view != ViewBuild {
			m.autoBuildView = true
			m.autoReturnView = m.view
			m.view = ViewBuild
		}
	case application.EventBuildSucceeded, application.EventBuildFailed:
		if isSelected && m.autoBuildView {
			m.returnAt = time.Now().Add(buildSettleDelay)
			cmds = append(cmds, tea.Tick(buildSettleDelay, func(time.Time) tea.Msg { return returnFromBuildMsg{} }))
		}
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if prev, ok := m.adjacentSelection(-1); ok {
			m.selected = prev
			m.scroll = 0
			m.hScroll = 0
			m.scrollSidebarToSelection()
		}
		return m, nil

	case "down", "j":
		if next, ok := m.adjacentSelection(1); ok {
			m.selected = next
			m.scroll = 0
			m.hScroll = 0
			m.scrollSidebarToSelection()
		}
		return m, nil

	case "enter":
		if svc, ok := m.selectedService(); ok {
			m.logScope = svc.Name
			m.view = ViewLogs
			m.scroll = 0
			m.hScroll = 0
		}
		return m, nil

	case "a", "esc":
		m.logScope = ""
		m.view = ViewLogs
		m.scroll = 0
		m.hScroll = 0
		return m, nil

	case "left":
		m.hScroll -= hScrollStep
		if m.hScroll < 0 {
			m.hScroll = 0
		}
		return m, nil

	case "right":
		m.hScroll += hScrollStep
		return m, nil

	case "tab":
		m.expanded = !m.expanded
		m.scrollSidebarToSelection()
		return m, nil

	case "1", "f1":
		m.view = ViewLogs
		m.autoBuildView = false
		return m, nil

	case "2", "f2":
		m.view = ViewBuild
		m.autoBuildView = false
		return m, nil

	case "3", "f3":
		m.view = ViewProblems
		m.autoBuildView = false
		return m, nil

	case "4", "f4":
		m.view = ViewDebugger
		m.autoBuildView = false
		return m, nil

	case "r":
		if svc, ok := m.selectedService(); ok {
			go m.sup.Restart(svc.Name)
		}
		return m, nil

	case "s":
		if svc, ok := m.selectedService(); ok {
			rt, _ := m.sup.Runtime(svc.Name)
			go func() {
				if isUp(rt) {
					m.sup.Stop(svc.Name)
				} else {
					m.sup.Start(svc.Name)
				}
			}()
		}
		return m, nil

	case "d":
		if svc, ok := m.selectedService(); ok {
			rt, _ := m.sup.Runtime(svc.Name)
			debugging := rt.Debug != nil
			go func() {
				if debugging {
					m.sup.StopDebug(svc.Name)
				} else {
					m.sup.StartDebug(svc.Name)
				}
			}()
			if !debugging {
				m.view = ViewDebugger
				m.autoBuildView = false
			}
		}
		return m, nil

	case "pgup":
		m.scroll += 10
		return m, nil

	case "pgdown":
		m.scroll -= 10
		if m.scroll < 0 {
			m.scroll = 0
		}
		return m, nil

	case "c":
		if m.view == ViewLogs {
			m.sup.ClearLogs()
			m.logLines = nil
		}
		return m, nil

	case "ctrl+r":
		go m.sup.Reload()
		return m, nil
	}
	return m, nil
}

// handleMouse supports the mouse gestures that map cleanly onto
// existing keyboard actions: the scroll wheel (any direction) moves
// the content pane the same way pgup/pgdown/left/right do, and a left
// click on a sidebar service row selects it and focuses its logs, the
// same as navigating there with up/down and then pressing enter.
func (m Model) handleMouse(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	switch ev.Button {
	case tea.MouseButtonWheelUp:
		m.scroll += vWheelStep
		return m, nil

	case tea.MouseButtonWheelDown:
		m.scroll -= vWheelStep
		if m.scroll < 0 {
			m.scroll = 0
		}
		return m, nil

	case tea.MouseButtonWheelLeft:
		m.hScroll -= hWheelStep
		if m.hScroll < 0 {
			m.hScroll = 0
		}
		return m, nil

	case tea.MouseButtonWheelRight:
		m.hScroll += hWheelStep
		return m, nil

	case tea.MouseButtonLeft:
		if ev.Action != tea.MouseActionPress {
			return m, nil
		}
		if idx, ok := m.serviceAtScreenPos(ev.X, ev.Y); ok {
			m.selected = idx
			m.scroll = 0
			m.hScroll = 0
			m.scrollSidebarToSelection()
			if svc, ok := m.selectedService(); ok {
				m.logScope = svc.Name
				m.view = ViewLogs
			}
		}
		return m, nil
	}
	return m, nil
}

// serviceAtScreenPos maps a screen coordinate to the service index of
// the sidebar row it falls on, if any. Mirrors the layout View()
// actually renders: row 0 is the header, then the body (sidebar rows
// interleaved 2 lines each, via sidebarVisibleWindow), then the
// footer - a click outside the sidebar's own column, or not on an
// actual service row (the "SERVICES" title, a group header, blank
// padding), is simply not a hit.
func (m Model) serviceAtScreenPos(x, y int) (int, bool) {
	if x >= m.sidebarWidth() {
		return 0, false
	}
	bodyRow := y - 1 // header occupies screen row 0
	if bodyRow < 2 {
		return 0, false // "SERVICES" title / blank line
	}
	rows, start, end := m.sidebarVisibleWindow()
	idx := (bodyRow - 2) / 2
	if idx < 0 || start+idx >= end {
		return 0, false
	}
	row := rows[start+idx]
	if row.IsHeader {
		return 0, false
	}
	return row.ServiceIndex, true
}
