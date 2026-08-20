package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/abtinokhovat/godev/internal/domain"
)

func (m Model) renderHeader() string {
	running := 0
	for _, svc := range m.services {
		if m.runtimes[svc.Name].State == domain.StateRunning {
			running++
		}
	}
	reload := "reload ✗"
	if m.sup.WatchActive() {
		reload = "reload ✓"
	}
	stats := fmt.Sprintf("%d service(s) · %d running · %s", len(m.services), running, reload)

	left := " " + m.project
	right := stats + " "

	if lipgloss.Width(left)+lipgloss.Width(right) >= m.width {
		combined := truncate(left+" "+right, m.width)
		return styleHeader.Render(padRight(combined, m.width))
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	line := left + strings.Repeat(" ", gap) + right
	return styleHeader.Render(line)
}
