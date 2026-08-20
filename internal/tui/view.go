package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

var (
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62")).Padding(0, 1)
	styleSection  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("39"))
	styleFooter   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("227"))
	styleService  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	styleSystem   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleStderr   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func stateColor(s domain.State) lipgloss.Color {
	switch s {
	case domain.StateRunning:
		return lipgloss.Color("42") // green
	case domain.StateCrashed, domain.StateBuildFailed:
		return lipgloss.Color("196") // red
	case domain.StateBuilding, domain.StateStarting, domain.StateRestarting, domain.StateStopping:
		return lipgloss.Color("220") // yellow
	default:
		return lipgloss.Color("245") // gray
	}
}

func stateDot(s domain.State) string {
	if s == domain.StateRunning {
		return "●"
	}
	return "○"
}

func (m Model) View() string {
	if m.quitting {
		return "shutting down services...\n"
	}
	if m.width == 0 {
		return "loading...\n"
	}

	header := styleHeader.Width(m.width).Render(fmt.Sprintf("godev%s%s", strings.Repeat(" ", max(1, m.width-6-len(m.project)-6)), m.project))

	if m.detail {
		return header + "\n" + m.renderDetail() + "\n" + m.renderFooter()
	}

	servicesHeight := len(m.services) + 3
	logsHeight := m.height - servicesHeight - 4
	if logsHeight < 3 {
		logsHeight = 3
	}

	body := header + "\n" + m.renderServices() + "\n" + m.renderLogs(logsHeight) + "\n" + m.renderFooter()
	return body
}

func (m Model) renderServices() string {
	var b strings.Builder
	b.WriteString(styleSection.Render("SERVICES"))
	b.WriteString("\n\n")

	for i, svc := range m.services {
		rt := m.runtimes[svc.Name]
		dot := lipgloss.NewStyle().Foreground(stateColor(rt.State)).Render(stateDot(rt.State))
		debugFlag := "DEBUG OFF"
		if rt.Debug != nil {
			debugFlag = fmt.Sprintf("DEBUG :%d", rt.Debug.Port)
		}
		pid := "--"
		if rt.PID != 0 {
			pid = fmt.Sprintf("PID %d", rt.PID)
		}
		line := fmt.Sprintf("%s %-14s %-12s %-10s %-8s %s",
			dot, svc.Name, rt.State.String(), pid, uptime(rt), debugFlag)

		if i == m.selected {
			b.WriteString(styleSelected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderLogs(height int) string {
	var b strings.Builder
	b.WriteString(styleSection.Render("LOGS"))
	b.WriteString("\n\n")

	lines := m.logLines
	end := len(lines) - m.logScroll
	if end > len(lines) {
		end = len(lines)
	}
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	visible := lines[start:end]

	for _, l := range visible {
		prefix := styleService.Render("[" + l.service + "]")
		text := l.text
		switch l.stream {
		case logs.StreamStderr:
			text = styleStderr.Render(text)
		case logs.StreamSystem:
			text = styleSystem.Render(text)
		}
		b.WriteString(prefix + " " + text + "\n")
	}
	for i := len(visible); i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderDetail() string {
	svc, ok := m.selectedService()
	if !ok {
		return "no service selected"
	}
	rt := m.runtimes[svc.Name]

	var b strings.Builder
	b.WriteString(styleSection.Render(strings.ToUpper(svc.Name)))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Package:       %s\n", svc.Package)
	fmt.Fprintf(&b, "Directory:     %s\n", svc.Directory)
	fmt.Fprintf(&b, "PID:           %d\n", rt.PID)
	fmt.Fprintf(&b, "State:         %s\n", rt.State.String())
	fmt.Fprintf(&b, "Uptime:        %s\n", uptime(rt))
	if rt.LastError != "" {
		fmt.Fprintf(&b, "Last Error:    %s\n", rt.LastError)
	}
	b.WriteString("\nArguments:\n")
	if len(svc.Args) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range svc.Args {
		fmt.Fprintf(&b, "  %s\n", a)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Hot Reload:    %s\n", onOff(svc.HotReload))
	fmt.Fprintf(&b, "Auto Restart:  %s\n", onOff(svc.AutoRestart))

	if rt.Debug != nil {
		b.WriteString("\nDEBUGGING\n")
		fmt.Fprintf(&b, "  Delve PID: %d\n", rt.Debug.DelvePID)
		fmt.Fprintf(&b, "  Endpoint:  %s:%d\n", rt.Debug.Host, rt.Debug.Port)
		b.WriteString("  VS Code:   Go: Attach (remote) -> above host/port\n")
		b.WriteString("  GoLand:    Run > Edit Configurations > Go Remote -> above host/port\n")
	} else {
		b.WriteString("\nDebugger:      OFF\n")
	}

	b.WriteString("\n[r] Restart  [s] Start/Stop  [d] Debug  [esc] Back\n")
	return b.String()
}

func (m Model) renderFooter() string {
	help := "↑↓ select   enter details   r restart   s start/stop   d debug   c clear logs   pgup/pgdn scroll   q quit"
	line := help
	if m.status != "" {
		line = styleStatus.Render(m.status)
	}
	return styleFooter.Render(line)
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
