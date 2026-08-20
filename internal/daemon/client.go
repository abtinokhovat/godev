package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

// RemoteSource implements tui.Source (structurally - internal/tui
// imports neither this package nor application/domain/logs beyond what
// it already did) as a locally-synced replica of a detached instance's
// Supervisor, kept current by the event/log stream from Server. Reads
// (Services/Runtime/BuildInfo/WatchActive) are answered from the
// replica, no round trip; writes (Start/Stop/...) send a fire-and-forget
// action frame, same as the in-process TUI's async dispatch.
type RemoteSource struct {
	conn net.Conn
	enc  *json.Encoder

	mu          sync.RWMutex
	services    []domain.Service
	runtimes    map[string]domain.ServiceRuntime
	buildInfos  map[string]application.BuildInfo
	watchActive bool

	eventSubs *fanout[application.Event]
	logSubs   *fanout[logs.Event]

	closed chan struct{}
}

// Dial connects to projectRoot's detached instance and blocks until
// the initial snapshot arrives (or dialTimeout elapses), so a caller
// can immediately use Services()/Runtime() without a race against the
// first message.
func Dial(projectRoot string, dialTimeout time.Duration) (*RemoteSource, error) {
	paths, err := ResolvePaths(projectRoot)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", paths.Socket, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("no running detached instance for this project (start one with `godev run <target>... --detach`): %w", err)
	}

	r := &RemoteSource{
		conn:       conn,
		enc:        json.NewEncoder(conn),
		runtimes:   map[string]domain.ServiceRuntime{},
		buildInfos: map[string]application.BuildInfo{},
		eventSubs:  newFanout[application.Event](),
		logSubs:    newFanout[logs.Event](),
		closed:     make(chan struct{}),
	}

	dec := json.NewDecoder(conn)
	var first frame
	if err := dec.Decode(&first); err != nil {
		conn.Close()
		return nil, fmt.Errorf("reading initial snapshot: %w", err)
	}
	if first.Kind != kindSnapshot || first.Snapshot == nil {
		conn.Close()
		return nil, fmt.Errorf("unexpected first message from daemon (kind=%q)", first.Kind)
	}
	r.applySnapshot(*first.Snapshot)

	go r.readLoop(dec)
	return r, nil
}

func (r *RemoteSource) applySnapshot(s snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services = s.Services
	r.runtimes = s.Runtimes
	if r.runtimes == nil {
		r.runtimes = map[string]domain.ServiceRuntime{}
	}
	r.buildInfos = s.BuildInfos
	if r.buildInfos == nil {
		r.buildInfos = map[string]application.BuildInfo{}
	}
	r.watchActive = s.WatchActive
	for _, l := range s.RecentLogs {
		r.logSubs.publish(l)
	}
}

func (r *RemoteSource) readLoop(dec *json.Decoder) {
	defer close(r.closed)
	defer r.conn.Close()
	for {
		var f frame
		if err := dec.Decode(&f); err != nil {
			return
		}
		switch f.Kind {
		case kindEvent:
			if f.Event == nil {
				continue
			}
			if f.Event.HasRuntime {
				r.mu.Lock()
				r.runtimes[f.Event.Service] = f.Event.Runtime
				r.mu.Unlock()
			}
			r.eventSubs.publish(f.Event.toEvent())
		case kindLog:
			if f.Log != nil {
				r.logSubs.publish(*f.Log)
			}
		}
	}
}

// Closed reports a channel closed when the connection to the daemon
// ends (the daemon exited, was killed, or the socket otherwise broke),
// so a caller like the attached TUI can notice and quit instead of
// sitting on a silently-dead connection.
func (r *RemoteSource) Closed() <-chan struct{} { return r.closed }

func (r *RemoteSource) Close() error { return r.conn.Close() }

// RequestShutdown sends the kill message a detached instance's own
// Server listens for; it does not wait for the instance to actually
// exit - see the `godev kill` command for that.
func (r *RemoteSource) RequestShutdown() error {
	return r.enc.Encode(frame{Kind: kindShutdown})
}

func (r *RemoteSource) sendAction(action, service string) error {
	return r.enc.Encode(frame{Kind: kindAction, Action: &actionFrame{Action: action, Service: service}})
}

func (r *RemoteSource) Services() []domain.Service {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Service, len(r.services))
	copy(out, r.services)
	return out
}

func (r *RemoteSource) Runtime(name string) (domain.ServiceRuntime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rt, ok := r.runtimes[name]
	return rt, ok
}

func (r *RemoteSource) BuildInfo(name string) (application.BuildInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	bi, ok := r.buildInfos[name]
	return bi, ok
}

func (r *RemoteSource) WatchActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.watchActive
}

func (r *RemoteSource) SubscribeEvents(buf int) (<-chan application.Event, func()) {
	return r.eventSubs.subscribe(buf)
}

func (r *RemoteSource) SubscribeLogs(buf int) (<-chan logs.Event, func()) {
	return r.logSubs.subscribe(buf)
}

// ClearLogs is a local-only clear (mirrors the in-process TUI's own
// "c" key): it doesn't ask the daemon to clear its buffer, since other
// attached clients (or a future re-attach) would want it kept.
func (r *RemoteSource) ClearLogs() {}

func (r *RemoteSource) Start(name string) error      { return r.sendAction(actionStart, name) }
func (r *RemoteSource) Stop(name string) error       { return r.sendAction(actionStop, name) }
func (r *RemoteSource) Restart(name string) error    { return r.sendAction(actionRestart, name) }
func (r *RemoteSource) StartDebug(name string) error { return r.sendAction(actionStartDebug, name) }
func (r *RemoteSource) StopDebug(name string) error  { return r.sendAction(actionStopDebug, name) }
