package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/abtinokhovat/godev/internal/domain"
)

func isUp(rt domain.ServiceRuntime) bool {
	switch rt.State {
	case domain.StateRunning, domain.StateStarting, domain.StateBuilding, domain.StateRestarting:
		return true
	default:
		return false
	}
}

func uptime(rt domain.ServiceRuntime) string {
	if rt.State != domain.StateRunning || rt.StartedAt.IsZero() {
		return "--"
	}
	return fmtDuration(time.Since(rt.StartedAt))
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

// padRight pads s with spaces to width visible columns, measured with
// lipgloss.Width so ANSI styling embedded in s doesn't throw off the
// column math. It never truncates - callers are expected to keep field
// content within width.
func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// truncate trims plain (unstyled) text to at most width visible
// runes, appending an ellipsis if it had to cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

// joinColumns lays two pre-rendered blocks of lines side by side,
// padding the left column to leftWidth and separating them with a
// vertical rule, for exactly `height` rows (short blocks are
// blank-padded, long ones are cut off - callers control scrolling
// upstream of this).
func joinColumns(left, right []string, leftWidth, height int) string {
	var b strings.Builder
	for i := 0; i < height; i++ {
		l := ""
		if i < len(left) {
			l = left[i]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		b.WriteString(padRight(l, leftWidth))
		b.WriteString(styleDivider.Render(" │ "))
		b.WriteString(r)
		if i < height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// window returns the slice of lines that should be visible given a
// "scrolled up from the bottom" offset and a viewport height, i.e. tail
// -height..end when scroll==0.
func window(lines []string, scroll, height int) []string {
	if height <= 0 {
		return nil
	}
	end := len(lines) - scroll
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
	return lines[start:end]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
