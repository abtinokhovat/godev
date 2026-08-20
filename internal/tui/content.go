package tui

import (
	"fmt"
	"strings"

	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// renderContent produces the full, unwindowed line list for whatever
// view is active; view.go slices it down to the viewport with scroll.
func (m Model) renderContent(width int) []string {
	switch m.view {
	case ViewBuild:
		return m.renderBuildContent(width)
	case ViewProblems:
		return m.renderProblemsContent(width)
	case ViewDebugger:
		return m.renderDebugContent(width)
	default:
		return m.renderLogsContent(width)
	}
}

func (m Model) contentTitle() string {
	switch m.view {
	case ViewBuild:
		if svc, ok := m.selectedService(); ok {
			return "BUILD · " + svc.Name
		}
		return "BUILD"
	case ViewProblems:
		return "PROBLEMS"
	case ViewDebugger:
		if svc, ok := m.selectedService(); ok {
			return "DEBUG · " + svc.Name
		}
		return "DEBUG"
	default:
		if m.logScope == "" {
			return "LOGS · all"
		}
		return "LOGS · " + m.logScope
	}
}

func (m Model) renderLogsContent(width int) []string {
	var out []string
	for _, l := range m.logLines {
		if m.logScope != "" && l.service != m.logScope {
			continue
		}
		ts := styleTimestamp.Render(l.time.Format("15:04:05"))
		text := l.text
		switch l.stream {
		case logs.StreamStderr:
			text = styleStderr.Render(text)
		case logs.StreamSystem:
			text = styleSystem.Render(text)
		}
		var line string
		if m.logScope == "" {
			line = ts + " " + styleService.Render(fmt.Sprintf("[%s]", l.service)) + " " + text
		} else {
			line = ts + "  " + text
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		out = []string{styleDim.Render("(no logs yet)")}
	}
	return out
}

func (m Model) renderBuildContent(width int) []string {
	svc, ok := m.selectedService()
	if !ok {
		return []string{styleDim.Render("no service selected")}
	}
	bi, _ := m.sup.BuildInfo(svc.Name)
	rt := m.runtimes[svc.Name]

	var out []string
	if rt.State == domain.StateBuilding {
		out = append(out, styleWarn.Render("Building "+svc.Name+"..."))
		out = append(out, styleDim.Render(fmt.Sprintf("$ go build -o <cache>/%s ./... %s", svc.Name, svc.Package)))
		out = append(out, "")
	}
	if !bi.Attempted {
		out = append(out, styleDim.Render("No build attempted yet."))
		return out
	}

	if bi.Success {
		out = append(out, styleOK.Render(fmt.Sprintf("✓ Build succeeded (%s)", bi.At.Format("15:04:05"))))
	} else {
		out = append(out, styleErr.Render(fmt.Sprintf("✗ Build failed (%s)", bi.At.Format("15:04:05"))))
	}
	if strings.TrimSpace(bi.Output) != "" {
		out = append(out, "")
		for _, line := range strings.Split(strings.TrimRight(bi.Output, "\n"), "\n") {
			if !bi.Success {
				out = append(out, styleErr.Render(line))
			} else {
				out = append(out, line)
			}
		}
	}
	return out
}

func (m Model) renderProblemsContent(width int) []string {
	var out []string
	count := 0
	for _, svc := range m.services {
		rt := m.runtimes[svc.Name]
		if rt.State != domain.StateCrashed && rt.State != domain.StateBuildFailed {
			continue
		}
		count++
		label := "CRASHED"
		if rt.State == domain.StateBuildFailed {
			label = "BUILD FAILED"
		}
		out = append(out, styleErr.Render("✗ ["+svc.Name+"] "+label))
		if rt.LastError != "" {
			for _, line := range strings.Split(strings.TrimRight(rt.LastError, "\n"), "\n") {
				out = append(out, "  "+truncate(line, max(width-2, 10)))
			}
		}
		out = append(out, "")
	}
	if count == 0 {
		return []string{styleOK.Render("No problems."), "", styleDim.Render("Every service is running or intentionally stopped.")}
	}
	return out
}

func (m Model) renderDebugContent(width int) []string {
	svc, ok := m.selectedService()
	if !ok {
		return []string{styleDim.Render("no service selected")}
	}
	rt := m.runtimes[svc.Name]

	if rt.Debug == nil {
		return []string{
			styleDim.Render("Debugger inactive for " + svc.Name + "."),
			"",
			styleDim.Render("Press d to build a debug binary and start headless Delve."),
		}
	}

	d := rt.Debug
	var out []string
	out = append(out, styleOK.Render("● Delve "+d.State.String()))
	out = append(out, fmt.Sprintf("  PID       %d", d.DelvePID))
	out = append(out, fmt.Sprintf("  Address   %s", d.Host))
	out = append(out, fmt.Sprintf("  Port      %d", d.Port))
	out = append(out, "")
	out = append(out, styleLabel.Render("VS Code"))
	out = append(out, fmt.Sprintf("  Attach (remote) -> %s:%d", d.Host, d.Port))
	out = append(out, "")
	out = append(out, styleLabel.Render("GoLand"))
	out = append(out, "  Run > Edit Configurations > Go Remote")
	out = append(out, fmt.Sprintf("  Host: %s   Port: %d", d.Host, d.Port))
	if d.Error != "" {
		out = append(out, "", styleErr.Render(d.Error))
	}
	return out
}
