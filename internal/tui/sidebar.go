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

// renderSidebar lays out the sidebar as a fixed-height viewport: the
// SERVICES list scrolls (windowed to whatever m.sidebarScroll/
// sidebarMaxVisibleRows currently allow - see model.go), while
// RUNTIME/DEBUGGER/detail always render in full below it, so overall
// status stays visible no matter how many services there are.
func (m Model) renderSidebar() []string {
	w := m.sidebarWidth()
	footer := m.renderSidebarFooter(w)
	rows, start, end := m.sidebarVisibleWindow()
	maxVisible := m.sidebarMaxVisibleRows()

	title := "SERVICES"
	if len(rows) > maxVisible {
		title = fmt.Sprintf("SERVICES %d-%d/%d", start+1, end, len(rows))
	}
	lines := []string{styleSection.Render(title), ""}
	for _, row := range rows[start:end] {
		lines = append(lines, m.renderSidebarRow(row, w)...)
	}

	return append(lines, footer...)
}

// renderSidebarRow renders one groupedRows() entry as its two lines:
// a group header ("" + group name), or a service's name+status pair.
func (m Model) renderSidebarRow(row groupRow, w int) []string {
	if row.IsHeader {
		return []string{"", styleGroupHeader.Render(truncate(row.Header, w))}
	}

	svc := m.services[row.ServiceIndex]
	rt := m.runtimes[svc.Name]
	selected := row.ServiceIndex == m.selected
	indent := ""
	if len(svc.Group) > 0 {
		indent = " "
	}

	nameLine := fmt.Sprintf("%s%s %-*s", indent, stateDot(rt.State), w-3-len(indent), truncate(svc.Name, w-4-len(indent)))
	statusLine := indent + "  " + rt.State.String()
	if rt.PID != 0 {
		statusLine = fmt.Sprintf("%s  %s · PID %d", indent, rt.State.String(), rt.PID)
	}
	statusLine = truncate(statusLine, w)

	if selected {
		return []string{styleSelected.Render(padRight(nameLine, w)), styleSelected.Render(padRight(statusLine, w))}
	}
	dot := lipgloss.NewStyle().Foreground(stateColor(rt.State)).Render(stateDot(rt.State))
	return []string{indent + dot + " " + truncate(svc.Name, w-4-len(indent)), styleDim.Render(statusLine)}
}

// renderSidebarFooter renders everything below the scrollable
// services list: RUNTIME, DEBUGGER, and (when expanded) the selected
// service's detail section. Always shown in full, never scrolled.
func (m Model) renderSidebarFooter(w int) []string {
	var lines []string
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
	if svc.IsCommand() {
		lines = append(lines, styleLabel.Render("Command  ")+truncate(strings.Join(svc.Command, " "), w-9))
	} else {
		lines = append(lines, styleLabel.Render("Package  ")+truncate(svc.Package, w-9))
	}
	if len(svc.Group) > 0 {
		lines = append(lines, styleLabel.Render("Group    ")+truncate(strings.Join(svc.Group, "/"), w-9))
	}
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
