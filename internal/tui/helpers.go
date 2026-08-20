package tui

import (
	"fmt"
	"time"

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
