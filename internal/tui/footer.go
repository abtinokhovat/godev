package tui

import "strings"

type hint struct {
	key   string
	label string
}

func (m Model) footerHints() []hint {
	common := []hint{
		{"↑↓", "select"},
		{"1-4", "views"},
		{"q", "quit"},
	}
	switch m.view {
	case ViewLogs:
		return append([]hint{
			{"enter", "focus"}, {"a", "all"}, {"tab", "detail"},
			{"r", "restart"}, {"s", "start/stop"}, {"d", "debug"}, {"c", "clear"},
		}, common...)
	case ViewBuild:
		return append([]hint{{"tab", "detail"}, {"r", "restart"}}, common...)
	case ViewProblems:
		return append([]hint{{"enter", "view logs"}, {"r", "restart"}}, common...)
	case ViewDebugger:
		return append([]hint{{"d", "start/stop debugger"}, {"tab", "detail"}}, common...)
	default:
		return common
	}
}

// renderFooter builds the footer as plain text first (truncating that,
// not styled output, so ANSI escapes never get cut mid-sequence) and
// applies color to the whole line at the end.
func (m Model) renderFooter() string {
	if m.status != "" {
		return styleFooter.Render(truncate(m.status, m.width))
	}
	var parts []string
	for _, h := range m.footerHints() {
		parts = append(parts, h.key+" "+h.label)
	}
	line := truncate(strings.Join(parts, "   "), m.width)
	return styleFooter.Render(line)
}
