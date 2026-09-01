package tui

import (
	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// Source is everything the TUI needs from a service supervisor. It
// exists so the TUI can run against either an in-process
// *application.Supervisor (the normal case) or a remote replica
// synced over a socket (attaching to a detached instance,
// internal/daemon.RemoteSource) without knowing which one it has -
// the TUI never touches processes, builds, or Delve directly either
// way.
type Source interface {
	Services() []domain.Service
	Runtime(name string) (domain.ServiceRuntime, bool)
	BuildInfo(name string) (application.BuildInfo, bool)
	WatchActive() bool

	SubscribeEvents(buf int) (<-chan application.Event, func())
	SubscribeLogs(buf int) (<-chan logs.Event, func())
	ClearLogs()

	Start(name string) error
	Stop(name string) error
	Restart(name string) error
	StartDebug(name string) error
	StopDebug(name string) error
	Reload() error

	// StartServices/StopServices/RestartServices act on several
	// services at once - a whole group, or an ad-hoc set of
	// names/groups typed in at the TUI's ":" prompt. Each is
	// fire-and-forget (failures surface as log lines/events, same as
	// the single-service methods above do once dispatched), and each
	// is handled sequentially or concurrently exactly the way
	// application.Supervisor's own method of the same name is - the
	// TUI never re-decides that policy, it only resolves which names a
	// group or ad-hoc target list means.
	StartServices(names []string)
	StopServices(names []string)
	RestartServices(names []string)
}
