package daemon

import (
	"encoding/json"
	"net"
	"os"
	"sync"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
)

// Server exposes a running *application.Supervisor to attach/kill
// clients over a Unix socket. It never becomes the source of truth for
// anything - it only forwards what the Supervisor already publishes
// (Services/Runtime/BuildInfo/events/logs) and relays actions back to
// it, exactly like internal/tui already does in-process.
type Server struct {
	sup *application.Supervisor

	mu       sync.Mutex
	shutdown chan struct{}
	once     sync.Once
	conns    map[net.Conn]struct{}
}

// NewServer wraps sup for serving. Call Listen to bind the project's
// socket, then Serve(listener) to start accepting connections.
func NewServer(sup *application.Supervisor) *Server {
	return &Server{sup: sup, shutdown: make(chan struct{}), conns: map[net.Conn]struct{}{}}
}

// Listen binds projectRoot's socket, removing a stale one left behind
// by an unclean previous exit first.
func Listen(projectRoot string) (net.Listener, Paths, error) {
	paths, err := ResolvePaths(projectRoot)
	if err != nil {
		return nil, Paths{}, err
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return nil, Paths{}, err
	}
	os.Remove(paths.Socket) // best-effort: clear a stale socket from an unclean exit
	ln, err := net.Listen("unix", paths.Socket)
	if err != nil {
		return nil, Paths{}, err
	}
	return ln, paths, nil
}

// Serve accepts connections until the listener is closed, handling
// each on its own goroutine. It returns nil when the listener closes
// cleanly (e.g. because ShutdownRequested fired and the caller closed
// it), matching net.Listener's own convention.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.shutdown:
				return nil
			default:
				return err
			}
		}
		go s.handleConn(conn)
	}
}

// ShutdownRequested fires once some client has sent a shutdown frame
// (what `godev kill` sends). The caller (the detached instance's main
// loop) should select on this alongside any OS signal it still wants
// to honor, then call Supervisor.Shutdown and exit.
func (s *Server) ShutdownRequested() <-chan struct{} {
	return s.shutdown
}

// requestShutdown fires ShutdownRequested and closes every currently
// connected client - closing the listener alone (which Serve's caller
// does in response to ShutdownRequested) only stops new connections,
// it doesn't touch ones already accepted, so without this an attached
// client would never see its connection drop when the instance it's
// attached to shuts down.
func (s *Server) requestShutdown() {
	s.once.Do(func() {
		close(s.shutdown)
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
	})
}

func (s *Server) trackConn(conn net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[conn] = struct{}{}
	} else {
		delete(s.conns, conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	s.trackConn(conn, true)
	defer s.trackConn(conn, false)

	var closeOnce sync.Once
	stop := make(chan struct{})
	closeConn := func() {
		closeOnce.Do(func() {
			conn.Close()
			close(stop)
		})
	}
	defer closeConn()

	enc := json.NewEncoder(conn)
	dec := json.NewDecoder(conn)

	if err := enc.Encode(frame{Kind: kindSnapshot, Snapshot: s.buildSnapshot()}); err != nil {
		return
	}

	eventsCh, cancelEvents := s.sup.SubscribeEvents(64)
	logsCh, cancelLogs := s.sup.SubscribeLogs(256)
	defer cancelEvents()
	defer cancelLogs()

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-stop:
				return
			case e, ok := <-eventsCh:
				if !ok {
					return
				}
				rt, hasRuntime := s.sup.Runtime(e.Service)
				ef := toEventFrame(e, rt, hasRuntime)
				if err := enc.Encode(frame{Kind: kindEvent, Event: &ef}); err != nil {
					closeConn()
					return
				}
			case l, ok := <-logsCh:
				if !ok {
					return
				}
				if err := enc.Encode(frame{Kind: kindLog, Log: &l}); err != nil {
					closeConn()
					return
				}
			}
		}
	}()

	for {
		var f frame
		if err := dec.Decode(&f); err != nil {
			break
		}
		switch f.Kind {
		case kindAction:
			if f.Action != nil {
				s.dispatchAction(*f.Action)
			}
		case kindShutdown:
			s.requestShutdown()
		}
		if f.Kind == kindShutdown {
			break
		}
	}
	closeConn()
	<-writerDone
}

func (s *Server) buildSnapshot() *snapshot {
	services := s.sup.Services()
	runtimes := make(map[string]domain.ServiceRuntime, len(services))
	buildInfos := make(map[string]application.BuildInfo, len(services))
	for _, svc := range services {
		if rt, ok := s.sup.Runtime(svc.Name); ok {
			runtimes[svc.Name] = rt
		}
		if bi, ok := s.sup.BuildInfo(svc.Name); ok {
			buildInfos[svc.Name] = bi
		}
	}
	return &snapshot{
		Services:    services,
		Runtimes:    runtimes,
		BuildInfos:  buildInfos,
		WatchActive: s.sup.WatchActive(),
		RecentLogs:  s.sup.Logs().Snapshot(""),
	}
}

func (s *Server) dispatchAction(a actionFrame) {
	switch a.Action {
	case actionStart:
		go s.sup.Start(a.Service)
	case actionStop:
		go s.sup.Stop(a.Service)
	case actionRestart:
		go s.sup.Restart(a.Service)
	case actionStartDebug:
		go s.sup.StartDebug(a.Service)
	case actionStopDebug:
		go s.sup.StopDebug(a.Service)
	case actionReload:
		go s.sup.Reload()
	}
}
