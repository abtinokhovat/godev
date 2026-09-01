package tui

import "github.com/charmbracelet/x/ansi"

// contentWidth is the content pane's usable width, matching what
// View() actually lays out (total width minus the sidebar and its " │
// " separator) - shared with maxScroll so scroll clamping never
// disagrees with what's actually rendered.
func (m Model) contentWidth() int {
	w := m.width - m.sidebarWidth() - 3
	if w < 10 {
		w = 10
	}
	return w
}

// contentHeight is the content pane's usable height in rows, below
// its 2-line title.
func (m Model) contentHeight() int {
	bodyHeight := m.height - 2 // header + footer
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	h := bodyHeight - 2 // title + blank line
	if h < 0 {
		h = 0
	}
	return h
}

// maxScroll is the highest m.scroll can usefully be: enough to bring
// the content pane's very first line to the top of the viewport, and
// not one line further - scrolling past that would only reveal blank
// space above the actual content.
func (m Model) maxScroll() int {
	n := len(m.renderContent(m.contentWidth()))
	h := m.contentHeight()
	if n <= h {
		return 0
	}
	return n - h
}

// clampScroll bounds m.scroll to maxScroll, so pgup/wheel-up can't
// scroll past the earliest line - called right after any increment,
// not just at render time, so scrolling back down afterward responds
// immediately instead of having to "unwind" however far past the top
// a burst of pgup/wheel-up went.
func (m *Model) clampScroll() {
	if max := m.maxScroll(); m.scroll > max {
		m.scroll = max
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m Model) View() string {
	if m.quitting {
		return "shutting down services...\n"
	}
	if m.width == 0 || m.height == 0 {
		return "loading...\n"
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	contentWidth := m.contentWidth()
	bodyHeight := m.height - 2 // header + footer
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	sidebarLines := m.renderSidebar()

	title := styleSection.Render(viewTitle(m.view)+" ") + styleDim.Render("· "+m.scopeLabel())
	contentAll := m.renderContent(contentWidth)
	contentHeight := m.contentHeight()
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
