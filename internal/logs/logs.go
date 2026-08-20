// Package logs implements the unified, service-prefixed log stream
// described in sections 26-27: every service's stdout/stderr is tagged
// with its name and fed into one ordered buffer that the TUI (or any
// other subscriber) can follow.
package logs

import (
	"sync"
	"time"
)

type Stream int

const (
	StreamStdout Stream = iota
	StreamStderr
	StreamSystem // godev's own messages about a service (build, restart, etc)
)

// Event is a single log line, tagged with its origin.
type Event struct {
	Time    time.Time
	Service string
	Stream  Stream
	Message string
}

// Manager buffers recent log events and fans them out to subscribers.
type Manager struct {
	mu          sync.Mutex
	buffer      []Event
	maxBuffer   int
	subscribers map[int]chan Event
	nextSubID   int
}

// NewManager creates a Manager retaining up to maxBuffer lines of
// scrollback.
func NewManager(maxBuffer int) *Manager {
	if maxBuffer <= 0 {
		maxBuffer = 5000
	}
	return &Manager{
		maxBuffer:   maxBuffer,
		subscribers: make(map[int]chan Event),
	}
}

// Publish records an event and notifies subscribers. Non-blocking: a
// slow subscriber drops events rather than stalling the producer.
func (m *Manager) Publish(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	m.mu.Lock()
	m.buffer = append(m.buffer, e)
	if len(m.buffer) > m.maxBuffer {
		m.buffer = m.buffer[len(m.buffer)-m.maxBuffer:]
	}
	subs := make([]chan Event, 0, len(m.subscribers))
	for _, ch := range m.subscribers {
		subs = append(subs, ch)
	}
	m.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Snapshot returns a copy of the current scrollback buffer, optionally
// filtered to a single service ("" means all services).
func (m *Manager) Snapshot(service string) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	if service == "" {
		out := make([]Event, len(m.buffer))
		copy(out, m.buffer)
		return out
	}
	var out []Event
	for _, e := range m.buffer {
		if e.Service == service {
			out = append(out, e)
		}
	}
	return out
}

// Subscribe registers a channel for live updates. Call the returned
// cancel func to unsubscribe.
func (m *Manager) Subscribe(buf int) (<-chan Event, func()) {
	m.mu.Lock()
	id := m.nextSubID
	m.nextSubID++
	ch := make(chan Event, buf)
	m.subscribers[id] = ch
	m.mu.Unlock()

	return ch, func() {
		m.mu.Lock()
		delete(m.subscribers, id)
		m.mu.Unlock()
		close(ch)
	}
}

// Clear empties the scrollback buffer (section 27's "Clear" feature).
func (m *Manager) Clear() {
	m.mu.Lock()
	m.buffer = nil
	m.mu.Unlock()
}
