package application

import (
	"sync"
	"time"
)

// EventType identifies the kind of Event, per the list in section 28.
type EventType string

const (
	EventServiceDiscovered EventType = "service_discovered"
	EventBuildStarted      EventType = "build_started"
	EventBuildSucceeded    EventType = "build_succeeded"
	EventBuildFailed       EventType = "build_failed"
	EventServiceStarting   EventType = "service_starting"
	EventServiceStarted    EventType = "service_started"
	EventServiceStopping   EventType = "service_stopping"
	EventServiceStopped    EventType = "service_stopped"
	EventServiceCrashed    EventType = "service_crashed"
	EventServiceRestarting EventType = "service_restarting"
	EventFileChanged       EventType = "file_changed"
	EventDebuggerStarting  EventType = "debugger_starting"
	EventDebuggerStarted   EventType = "debugger_started"
	EventDebuggerStopped   EventType = "debugger_stopped"
	EventDebuggerFailed    EventType = "debugger_failed"
	EventPortsChanged      EventType = "ports_changed"
)

// Event is a single notification from the supervisor to any listener
// (primarily the TUI). Message is a human-readable summary; Err carries
// failure detail when relevant.
type Event struct {
	Type    EventType
	Time    time.Time
	Service string
	Message string
	Err     error
}

// EventBus is a small non-blocking pub/sub for Events, mirroring
// logs.Manager's approach: slow subscribers drop events instead of
// stalling producers.
type EventBus struct {
	subs   map[int]chan Event
	nextID int
	mu     sync.Mutex
}

func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[int]chan Event)}
}

func (b *EventBus) Publish(e Event) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	b.mu.Lock()
	subs := make([]chan Event, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (b *EventBus) Subscribe(buf int) (<-chan Event, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	ch := make(chan Event, buf)
	b.subs[id] = ch
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
		close(ch)
	}
}
