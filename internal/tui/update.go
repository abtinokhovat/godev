package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
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
		m.clampScroll()
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

// appendLocalLogLine appends a synthetic, TUI-local system log line -
// never sent through logs.Manager or seen by anything but this
// client - for one-off feedback (e.g. "copied N lines to clipboard")
// that doesn't warrant a whole Supervisor round trip. Tagged with the
// current log scope so it's visible from wherever the user actually
// is: the "all services" view when unscoped, or the scoped service's
// view otherwise.
func (m *Model) appendLocalLogLine(text string) {
	m.logLines = append(m.logLines, logLine{service: m.logScope, stream: logs.StreamSystem, time: time.Now(), text: text})
	if len(m.logLines) > m.maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-m.maxLogLines:]
	}
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
	if m.commandMode {
		return m.handleCommandInputKey(msg)
	}
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

	case "R":
		if names, ok := m.selectedGroupMembers(); ok {
			m.sup.RestartServices(names)
		}
		return m, nil

	case "S":
		if names, ok := m.selectedGroupMembers(); ok {
			anyUp := false
			for _, n := range names {
				if rt, ok := m.sup.Runtime(n); ok && isUp(rt) {
					anyUp = true
					break
				}
			}
			if anyUp {
				m.sup.StopServices(names)
			} else {
				m.sup.StartServices(names)
			}
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
		m.clampScroll()
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

	case "y":
		if m.view == ViewLogs {
			text, n := m.plainLogText()
			osc52Copy(text)
			m.appendLocalLogLine(fmt.Sprintf("copied %d log line(s) to clipboard", n))
		}
		return m, nil

	case "ctrl+r":
		go m.sup.Reload()
		return m, nil

	case ":":
		m.commandMode = true
		m.commandInput = ""
		return m, nil
	}
	return m, nil
}

// handleCommandInputKey handles every keypress while the ":" ad-hoc
// run prompt is open, bypassing handleKey's normal single-key dispatch
// entirely - typing "s" here must insert the letter, not toggle a
// service.
func (m Model) handleCommandInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.commandMode = false
		m.commandInput = ""
		return m, nil

	case tea.KeyEnter:
		m.commandMode = false
		targets := strings.Fields(m.commandInput)
		m.commandInput = ""
		if len(targets) == 0 {
			return m, nil
		}
		resolved, err := domain.ResolveTargets(m.services, targets)
		if err != nil {
			m.appendLocalLogLine("run " + strings.Join(targets, " ") + ": " + err.Error())
			return m, nil
		}
		names := make([]string, len(resolved))
		for i, s := range resolved {
			names[i] = s.Name
		}
		m.sup.StartServices(names)
		m.appendLocalLogLine("starting " + strings.Join(names, ", "))
		return m, nil

	case tea.KeyBackspace:
		if r := []rune(m.commandInput); len(r) > 0 {
			m.commandInput = string(r[:len(r)-1])
		}
		return m, nil

	case tea.KeySpace:
		m.commandInput += " "
		return m, nil

	case tea.KeyRunes:
		m.commandInput += string(msg.Runes)
		return m, nil
	}
	return m, nil
}

// handleMouse supports the mouse gestures that map cleanly onto
// existing keyboard actions: the scroll wheel moves whichever pane
// the cursor is currently over - the sidebar if it's over the
// sidebar's column, the content pane otherwise - the same way
// pgup/pgdown/left/right (content) or up/down (sidebar) do, and a left
// click on a sidebar service row selects it and focuses its logs, the
// same as navigating there with up/down and then pressing enter.
func (m Model) handleMouse(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	overSidebar := ev.X < m.sidebarWidth()

	switch ev.Button {
	case tea.MouseButtonWheelUp:
		if overSidebar {
			m.sidebarScroll--
			m.clampSidebarScroll()
		} else {
			m.scroll += vWheelStep
			m.clampScroll()
		}
		return m, nil

	case tea.MouseButtonWheelDown:
		if overSidebar {
			m.sidebarScroll++
			m.clampSidebarScroll()
		} else {
			m.scroll -= vWheelStep
			if m.scroll < 0 {
				m.scroll = 0
			}
		}
		return m, nil

	case tea.MouseButtonWheelLeft:
		if !overSidebar {
			m.hScroll -= hWheelStep
			if m.hScroll < 0 {
				m.hScroll = 0
			}
		}
		return m, nil

	case tea.MouseButtonWheelRight:
		if !overSidebar {
			m.hScroll += hWheelStep
		}
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
