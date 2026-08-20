package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/abtinokhovat/godev/internal/domain"
)

func (m Model) sidebarWidth() int {
	if m.expanded {
		return sidebarWidthExpanded
	}
	return sidebarWidth
}

func (m Model) renderSidebar() []string {
	w := m.sidebarWidth()
	var lines []string

	lines = append(lines, styleSection.Render("SERVICES"), "")
	for i, svc := range m.services {
		rt := m.runtimes[svc.Name]
		selected := i == m.selected

		nameLine := fmt.Sprintf("%s %-*s", stateDot(rt.State), w-3, truncate(svc.Name, w-4))
		statusLine := "  " + rt.State.String()
		if rt.PID != 0 {
			statusLine = fmt.Sprintf("  %s · PID %d", rt.State.String(), rt.PID)
		}
		statusLine = truncate(statusLine, w)

		if selected {
			lines = append(lines, styleSelected.Render(padRight(nameLine, w)))
			lines = append(lines, styleSelected.Render(padRight(statusLine, w)))
		} else {
			dot := lipgloss.NewStyle().Foreground(stateColor(rt.State)).Render(stateDot(rt.State))
			lines = append(lines, dot+" "+truncate(svc.Name, w-4))
			lines = append(lines, styleDim.Render(statusLine))
		}
	}

	lines = append(lines, "", styleDivider.Render(strings.Repeat("─", w)))
	lines = append(lines, styleSection.Render("RUNTIME"))
	lines = append(lines, reloadStatusLine(m.sup.WatchActive()))
	lines = append(lines, autoRestartStatusLine(m.services))

	lines = append(lines, "", styleDivider.Render(strings.Repeat("─", w)))
	lines = append(lines, styleSection.Render("DEBUGGER"))
	lines = append(lines, debuggerStatusLines(m.runtimes)...)

	if m.expanded {
		if svc, ok := m.selectedService(); ok {
			lines = append(lines, "", styleDivider.Render(strings.Repeat("─", w)))
			lines = append(lines, m.renderDetail(svc, w)...)
		}
	}

	return lines
}

func reloadStatusLine(active bool) string {
	if active {
		return styleOK.Render("✓") + " Hot reload"
	}
	return styleDim.Render("✗") + " Hot reload"
}

func autoRestartStatusLine(services []domain.Service) string {
	if len(services) == 0 {
		return styleDim.Render("✗") + " Auto restart"
	}
	all, none := true, true
	for _, s := range services {
		if s.AutoRestart {
			none = false
		} else {
			all = false
		}
	}
	switch {
	case all:
		return styleOK.Render("✓") + " Auto restart"
	case none:
		return styleDim.Render("✗") + " Auto restart"
	default:
		return styleWarn.Render("~") + " Auto restart (mixed)"
	}
}

func debuggerStatusLines(runtimes map[string]domain.ServiceRuntime) []string {
	for name, rt := range runtimes {
		if rt.Debug != nil {
			return []string{
				styleOK.Render("●") + " " + name,
				styleDim.Render(fmt.Sprintf("  %s:%d", rt.Debug.Host, rt.Debug.Port)),
			}
		}
	}
	return []string{styleDim.Render("○ None")}
}

func (m Model) renderDetail(svc domain.Service, w int) []string {
	rt := m.runtimes[svc.Name]
	bi, _ := m.sup.BuildInfo(svc.Name)

	var lines []string
	lines = append(lines, styleSection.Render("DETAIL: "+truncate(svc.Name, w-8)))
	lines = append(lines, styleLabel.Render("Package  ")+truncate(svc.Package, w-9))
	lines = append(lines, styleLabel.Render("Uptime   ")+uptime(rt))
	buildFlag := styleDim.Render("--")
	if bi.Attempted {
		if bi.Success {
			buildFlag = styleOK.Render("✓")
		} else {
			buildFlag = styleErr.Render("✗")
		}
	}
	lines = append(lines, styleLabel.Render("Build    ")+buildFlag)
	lines = append(lines, styleLabel.Render("Reload   ")+onOff(svc.HotReload))
	lines = append(lines, styleLabel.Render("Restart  ")+onOff(svc.AutoRestart))

	lines = append(lines, "", styleLabel.Render("Arguments"))
	if len(svc.Args) == 0 {
		lines = append(lines, styleDim.Render("  (none)"))
	} else {
		for _, a := range svc.Args {
			lines = append(lines, "  "+truncate(a, w-2))
		}
	}

	if len(svc.Env) > 0 {
		lines = append(lines, "", styleLabel.Render("Environment"))
		for k, v := range svc.Env {
			lines = append(lines, truncate("  "+k+"="+v, w))
		}
	}

	return lines
}
