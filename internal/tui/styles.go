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
)

const (
	sidebarWidth         = 22
	sidebarWidthExpanded = 34
)

// groupBadgePalette and groupTextPalette give each group a
// consistent, distinct color across the TUI - picked by hashing the
// group name, so "core" always renders in the same color family
// across runs without any config, and index-for-index the same hue
// family in both palettes (badge and text) so a service's log-line
// color and its sidebar group badge are recognizably "the same
// color", not just coincidentally similar.
//
// They're tuned differently for how each is used: groupBadgePalette
// backs a badge's background behind bold white text, so every entry
// is dark/saturated enough to keep that text readable - a pale color
// there would wash the label out. groupTextPalette colors plain
// foreground text (the unified log view's "[service]" prefix) against
// the terminal's own background, so it needs to be bright rather than
// dark to actually read.
var groupBadgePalette = []lipgloss.Color{
	lipgloss.Color("25"),  // blue
	lipgloss.Color("130"), // orange
	lipgloss.Color("28"),  // green
	lipgloss.Color("125"), // magenta
	lipgloss.Color("94"),  // brown
	lipgloss.Color("30"),  // teal
	lipgloss.Color("160"), // red
	lipgloss.Color("55"),  // purple
}

var groupTextPalette = []lipgloss.Color{
	lipgloss.Color("111"), // blue
	lipgloss.Color("215"), // orange
	lipgloss.Color("150"), // green
	lipgloss.Color("212"), // magenta
	lipgloss.Color("180"), // brown/tan
	lipgloss.Color("80"),  // teal
	lipgloss.Color("203"), // red
	lipgloss.Color("183"), // purple
}

// groupPaletteIndex hashes a group name into the index shared by
// groupBadgePalette and groupTextPalette via FNV-1a.
func groupPaletteIndex(name string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return int(h.Sum32() % uint32(len(groupBadgePalette)))
}

// groupColor is a group header badge's background color.
func groupColor(name string) lipgloss.Color {
	return groupBadgePalette[groupPaletteIndex(name)]
}

// groupTextColor is a group's color as plain foreground text, same
// hue family as groupColor but bright enough to read as text.
func groupTextColor(name string) lipgloss.Color {
	return groupTextPalette[groupPaletteIndex(name)]
}

// serviceLabelStyle picks the color for a service's name as it
// appears in the unified log view ("[service] some log line"):
// services in the same group render in that group's color (the same
// hue as its sidebar badge), so a burst of interleaved log lines from
// different services still reads as "these came from the same group"
// at a glance. An ungrouped service falls back to the plain
// styleService color, since there's no group to color it by.
func serviceLabelStyle(group string) lipgloss.Style {
	if group == "" {
		return styleService
	}
	return lipgloss.NewStyle().Bold(true).Foreground(groupTextColor(group))
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
