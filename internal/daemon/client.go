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

// serviceLogsTimeout bounds how long ServiceLogs waits for the daemon
// to reply before giving up and returning nothing - better an
// occasional empty scope-switch than the TUI hanging because a reply
// was lost (e.g. the daemon died mid-request).
const serviceLogsTimeout = 3 * time.Second

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
	recentLogs  []logs.Event

	eventSubs *fanout[application.Event]
	logSubs   *fanout[logs.Event]

	closed chan struct{}

	// writeMu serializes every frame this client sends: actions are
	// fired off from whatever goroutine handles a keypress (see
	// sendAction), and ServiceLogs below both sends and blocks for a
	// reply, so without this two calls racing each other could
	// interleave partial writes on the same json.Encoder.
	writeMu sync.Mutex

	// nextReqID/pendingServiceLogs correlate a ServiceLogs reply (see
	// serviceLogsResponse) back to the call that's waiting on it - the
	// one request/response exchange in an otherwise fire-and-forget or
	// push-only protocol.
	nextReqID          int
	pendingServiceLogs map[int]chan []logs.Event
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
		conn:               conn,
		enc:                json.NewEncoder(conn),
		runtimes:           map[string]domain.ServiceRuntime{},
		buildInfos:         map[string]application.BuildInfo{},
		eventSubs:          newFanout[application.Event](),
		logSubs:            newFanout[logs.Event](),
		closed:             make(chan struct{}),
		pendingServiceLogs: map[int]chan []logs.Event{},
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
	// Stored for RecentLogs() to hand to a subscriber that asks, not
	// published here - nothing has called SubscribeLogs yet at this
	// point (Dial hasn't even returned to the caller that will), so
	// fanout.publish's no-history, current-subscribers-only delivery
	// would just silently drop every one of these.
	r.recentLogs = s.RecentLogs
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
		case kindServiceLogsResp:
			if f.ServiceLogsResp == nil {
				continue
			}
			r.mu.Lock()
			ch, ok := r.pendingServiceLogs[f.ServiceLogsResp.ID]
			delete(r.pendingServiceLogs, f.ServiceLogsResp.ID)
			r.mu.Unlock()
			if ok {
				ch <- f.ServiceLogsResp.Logs
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
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.enc.Encode(frame{Kind: kindShutdown})
}

func (r *RemoteSource) sendAction(action, service string) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.enc.Encode(frame{Kind: kindAction, Action: &actionFrame{Action: action, Service: service}})
}

func (r *RemoteSource) sendBatchAction(action string, names []string) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_ = r.enc.Encode(frame{Kind: kindAction, Action: &actionFrame{Action: action, Services: names}})
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

func (r *RemoteSource) RecentLogs() []logs.Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]logs.Event(nil), r.recentLogs...)
}

// ServiceLogs asks the detached instance for one service's full log
// history (see Server's kindServiceLogsReq handling and
// application.Supervisor.ServiceLogs) and blocks for the reply -
// unlike every other method here, which either reads the locally kept
// replica or fires an action without waiting. Returns nil on timeout
// or if the connection drops before a reply arrives, same as any other
// best-effort log source.
func (r *RemoteSource) ServiceLogs(name string) []logs.Event {
	r.mu.Lock()
	id := r.nextReqID
	r.nextReqID++
	ch := make(chan []logs.Event, 1)
	r.pendingServiceLogs[id] = ch
	r.mu.Unlock()

	r.writeMu.Lock()
	err := r.enc.Encode(frame{Kind: kindServiceLogsReq, ServiceLogsReq: &serviceLogsRequest{ID: id, Service: name}})
	r.writeMu.Unlock()
	if err != nil {
		r.mu.Lock()
		delete(r.pendingServiceLogs, id)
		r.mu.Unlock()
		return nil
	}

	select {
	case events := <-ch:
		return events
	case <-time.After(serviceLogsTimeout):
		r.mu.Lock()
		delete(r.pendingServiceLogs, id)
		r.mu.Unlock()
		return nil
	case <-r.closed:
		return nil
	}
}

func (r *RemoteSource) Start(name string) error      { return r.sendAction(actionStart, name) }
func (r *RemoteSource) Stop(name string) error       { return r.sendAction(actionStop, name) }
func (r *RemoteSource) Restart(name string) error    { return r.sendAction(actionRestart, name) }
func (r *RemoteSource) StartDebug(name string) error { return r.sendAction(actionStartDebug, name) }
func (r *RemoteSource) StopDebug(name string) error  { return r.sendAction(actionStopDebug, name) }

func (r *RemoteSource) StartServices(names []string) { r.sendBatchAction(actionStartServices, names) }
func (r *RemoteSource) StopServices(names []string)  { r.sendBatchAction(actionStopServices, names) }
func (r *RemoteSource) RestartServices(names []string) {
	r.sendBatchAction(actionRestartServices, names)
}

// Reload asks the detached instance to re-read its .godev.yaml and
// reconcile - fire-and-forget like every other action; the outcome
// (new services discovered, changed services restarted, or a failure
// reading the file) surfaces the normal way, as events and log lines
// flowing back through the stream.
func (r *RemoteSource) Reload() error { return r.sendAction(actionReload, "") }
