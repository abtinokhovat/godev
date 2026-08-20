package daemon

import "sync"

// fanout is a small generic non-blocking pub/sub, the same shape as
// logs.Manager's and application.EventBus's subscriber lists (a slow
// subscriber drops messages rather than stalling the publisher) - used
// here by RemoteSource to re-publish the daemon's event/log stream to
// however many local listeners the TUI itself sets up.
type fanout[T any] struct {
	mu     sync.Mutex
	subs   map[int]chan T
	nextID int
}

func newFanout[T any]() *fanout[T] {
	return &fanout[T]{subs: make(map[int]chan T)}
}

func (f *fanout[T]) publish(v T) {
	f.mu.Lock()
	chans := make([]chan T, 0, len(f.subs))
	for _, ch := range f.subs {
		chans = append(chans, ch)
	}
	f.mu.Unlock()

	for _, ch := range chans {
		select {
		case ch <- v:
		default:
		}
	}
}

func (f *fanout[T]) subscribe(buf int) (<-chan T, func()) {
	f.mu.Lock()
	id := f.nextID
	f.nextID++
	ch := make(chan T, buf)
	f.subs[id] = ch
	f.mu.Unlock()

	return ch, func() {
		f.mu.Lock()
		delete(f.subs, id)
		f.mu.Unlock()
		close(ch)
	}
}
