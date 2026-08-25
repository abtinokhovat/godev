package tui

import "github.com/charmbracelet/x/ansi"

func (m Model) View() string {
	if m.quitting {
		return "shutting down services...\n"
	}
	if m.width == 0 || m.height == 0 {
		return "loading...\n"
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	contentWidth := m.width - m.sidebarWidth() - 3 // " │ " separator
	if contentWidth < 10 {
		contentWidth = 10
	}
	bodyHeight := m.height - 2 // header + footer
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	sidebarLines := m.renderSidebar()

	title := styleSection.Render(viewTitle(m.view)+" ") + styleDim.Render("· "+m.scopeLabel())
	contentAll := m.renderContent(contentWidth)
	contentHeight := bodyHeight - 2
	if contentHeight < 0 {
		contentHeight = 0
	}
	visible := window(contentAll, m.scroll, contentHeight)
	// Cut each line to the horizontal viewport - ANSI-aware, so a
	// scrolled-right styled log line doesn't clip mid-escape-sequence
	// and bleed color into the rest of the row. This also doubles as
	// safe truncation at hScroll=0: a long line no longer overflows
	// past contentWidth and wraps the terminal, breaking the sidebar's
	// column alignment.
	for i, line := range visible {
		visible[i] = ansi.Cut(line, m.hScroll, m.hScroll+contentWidth)
	}
	contentLines := append([]string{title, ""}, visible...)

	body := joinColumns(sidebarLines, contentLines, m.sidebarWidth(), bodyHeight)

	return header + "\n" + body + "\n" + footer
}

func (m Model) scopeLabel() string {
	switch m.view {
	case ViewLogs:
		if m.logScope == "" {
			return "all services"
		}
		return m.logScope
	default:
		if svc, ok := m.selectedService(); ok {
			return svc.Name
		}
		return ""
	}
}
