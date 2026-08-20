package daemon

import (
	"errors"
	"time"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// frame is the one message type exchanged over the connection, tagged
// by Kind; exactly one of the pointer fields is set per Kind. Encoded
// one-per-call via encoding/json's Encoder/Decoder, which already
// frames successive values on a stream (no manual delimiter needed).
type frame struct {
	Kind     string       `json:"kind"`
	Snapshot *snapshot    `json:"snapshot,omitempty"`
	Event    *eventFrame  `json:"event,omitempty"`
	Log      *logs.Event  `json:"log,omitempty"`
	Action   *actionFrame `json:"action,omitempty"`
}

const (
	kindSnapshot = "snapshot"
	kindEvent    = "event"
	kindLog      = "log"
	kindAction   = "action"
	kindShutdown = "shutdown"
)

// snapshot is sent once, immediately after a client connects: enough
// state to seed a tui.Source replica (RemoteSource) without waiting for
// events to trickle in for services that aren't currently changing.
type snapshot struct {
	Services    []domain.Service
	Runtimes    map[string]domain.ServiceRuntime
	BuildInfos  map[string]application.BuildInfo
	WatchActive bool
	RecentLogs  []logs.Event
}

// eventFrame carries an application.Event plus the affected service's
// runtime snapshot at forward time, so the client can update its local
// replica directly from the stream instead of pulling a fresh Runtime()
// over the wire for every event the way the in-process TUI pulls it
// in-memory.
type eventFrame struct {
	Type       string
	Time       time.Time
	Service    string
	Message    string
	Err        string // application.Event.Err, flattened to a string - error values don't round-trip through JSON
	Runtime    domain.ServiceRuntime
	HasRuntime bool // false if Service doesn't name a currently-known service
}

func toEventFrame(e application.Event, rt domain.ServiceRuntime, hasRuntime bool) eventFrame {
	f := eventFrame{
		Type: string(e.Type), Time: e.Time, Service: e.Service, Message: e.Message,
		Runtime: rt, HasRuntime: hasRuntime,
	}
	if e.Err != nil {
		f.Err = e.Err.Error()
	}
	return f
}

func (f eventFrame) toEvent() application.Event {
	e := application.Event{
		Type: application.EventType(f.Type), Time: f.Time, Service: f.Service, Message: f.Message,
	}
	if f.Err != "" {
		e.Err = errors.New(f.Err)
	}
	return e
}

// actionFrame is the only client->server message: a fire-and-forget
// request to perform one Supervisor operation. The server doesn't
// reply directly - the result (success or failure) shows up the normal
// way, as events and log lines flowing back through the stream, the
// same as the in-process TUI's own `go m.sup.X(name)` dispatch already
// works.
type actionFrame struct {
	Action  string // "start" | "stop" | "restart" | "start_debug" | "stop_debug"
	Service string
}

const (
	actionStart      = "start"
	actionStop       = "stop"
	actionRestart    = "restart"
	actionStartDebug = "start_debug"
	actionStopDebug  = "stop_debug"
)
