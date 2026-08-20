package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case eventMsg:
		return m.handleEvent(msg)

	case logMsg:
		m.logLines = append(m.logLines, logLine{service: msg.Service, stream: msg.Stream, text: msg.Message})
		if len(m.logLines) > m.maxLogLines {
			m.logLines = m.logLines[len(m.logLines)-m.maxLogLines:]
		}
		return m, listenLogs(m.logsCh)

	case tickMsg:
		return m, tickCmd()
	}
	return m, nil
}

func (m Model) handleEvent(e eventMsg) (tea.Model, tea.Cmd) {
	// Refresh every known service's runtime snapshot; cheap for MVP scale.
	for _, svc := range m.services {
		if rt, ok := m.sup.Runtime(svc.Name); ok {
			m.runtimes[svc.Name] = rt
		}
	}
	if e.Message != "" {
		m.status = string(e.Type) + ": " + e.Message
	} else if e.Err != nil {
		m.status = string(e.Type) + ": " + e.Err.Error()
	} else {
		m.status = string(e.Type)
	}
	return m, listenEvents(m.eventsCh)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil

	case "down", "j":
		if m.selected < len(m.services)-1 {
			m.selected++
		}
		return m, nil

	case "enter":
		m.detail = !m.detail
		return m, nil

	case "esc":
		m.detail = false
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
			go func() {
				if rt.Debug != nil {
					m.sup.StopDebug(svc.Name)
				} else {
					m.sup.StartDebug(svc.Name)
				}
			}()
		}
		return m, nil

	case "pgup":
		m.logScroll += 10
		return m, nil

	case "pgdown":
		m.logScroll -= 10
		if m.logScroll < 0 {
			m.logScroll = 0
		}
		return m, nil

	case "c":
		m.sup.Logs().Clear()
		m.logLines = nil
		return m, nil
	}
	return m, nil
}
