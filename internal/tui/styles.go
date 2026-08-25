package tui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"

	"github.com/abtinokhovat/godev/internal/domain"
)

var (
	// No Padding/Width here deliberately: header.go builds the line to
	// the exact terminal width itself (including its own space padding)
	// before styling it, so lipgloss never reflows it onto a second row.
	styleHeader      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	styleHeaderMuted = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("62"))
	styleSection     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	styleSelected    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("39"))
	styleFooter      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleFooterKey   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	styleStatus      = lipgloss.NewStyle().Foreground(lipgloss.Color("227"))
	styleService     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	styleSystem      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleStderr      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	styleTimestamp   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleDivider     = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleTabActive   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62")).Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	styleOK          = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styleErr         = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleWarn        = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	styleDim         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleLabel       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styleGroupHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("111"))
)

const (
	sidebarWidth         = 22
	sidebarWidthExpanded = 34
)

// groupPalette gives each group a consistent, distinct color in the
// sidebar tree - picked by hashing the group name, so "core" always
// renders in the same color across runs without any config.
var groupPalette = []lipgloss.Color{
	lipgloss.Color("111"), // blue
	lipgloss.Color("212"), // pink
	lipgloss.Color("150"), // green
	lipgloss.Color("215"), // orange
	lipgloss.Color("183"), // purple
	lipgloss.Color("222"), // yellow
	lipgloss.Color("80"),  // cyan
	lipgloss.Color("203"), // red
}

// groupColor deterministically maps a group name to one of
// groupPalette's colors via FNV-1a.
func groupColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return groupPalette[h.Sum32()%uint32(len(groupPalette))]
}

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
	switch s {
	case domain.StateRunning, domain.StateStarting, domain.StateBuilding, domain.StateRestarting:
		return "●"
	case domain.StateCrashed, domain.StateBuildFailed:
		return "✗"
	default:
		return "○"
	}
}

func viewTitle(v ViewMode) string {
	switch v {
	case ViewLogs:
		return "LOGS"
	case ViewBuild:
		return "BUILD"
	case ViewProblems:
		return "PROBLEMS"
	case ViewDebugger:
		return "DEBUG"
	default:
		return ""
	}
}
