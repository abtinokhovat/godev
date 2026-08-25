package tui

import "strings"

type hint struct {
	key   string
	label string
}

func (m Model) footerHints() []hint {
	common := []hint{
		{"↑↓", "select"},
		{"pgup/dn ←→", "scroll"},
		{"1-4", "views"},
		{"q", "quit"},
	}
	switch m.view {
	case ViewLogs:
		return append([]hint{
			{"enter", "focus"}, {"a/esc", "all"}, {"tab", "detail"},
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
// applies color to the whole line at the end. Always the available
// keybinds for the current view - never overwritten by a transient
// status message, which used to make the footer show whatever the
// last event happened to be instead of what you can actually press,
// almost all the time (events fire constantly - a build, a log line,
// a ports check - so "transient" in practice meant "always").
func (m Model) renderFooter() string {
	var parts []string
	for _, h := range m.footerHints() {
		parts = append(parts, h.key+" "+h.label)
	}
	line := truncate(strings.Join(parts, "   "), m.width)
	return styleFooter.Render(line)
}
