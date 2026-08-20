package logs

import "testing"

func TestPublishAndSnapshot(t *testing.T) {
	m := NewManager(10)
	m.Publish(Event{Service: "api", Message: "a"})
	m.Publish(Event{Service: "worker", Message: "b"})
	m.Publish(Event{Service: "api", Message: "c"})

	all := m.Snapshot("")
	if len(all) != 3 {
		t.Fatalf("got %d events, want 3", len(all))
	}
	apiOnly := m.Snapshot("api")
	if len(apiOnly) != 2 {
		t.Fatalf("got %d api events, want 2", len(apiOnly))
	}
}

func TestBufferTrimsToMax(t *testing.T) {
	m := NewManager(3)
	for i := 0; i < 10; i++ {
		m.Publish(Event{Service: "api", Message: "x"})
	}
	if got := len(m.Snapshot("")); got != 3 {
		t.Fatalf("buffer len = %d, want 3", got)
	}
}

func TestSubscribeReceivesLiveEvents(t *testing.T) {
	m := NewManager(10)
	ch, cancel := m.Subscribe(4)
	defer cancel()

	m.Publish(Event{Service: "api", Message: "hi"})

	select {
	case e := <-ch:
		if e.Message != "hi" {
			t.Fatalf("got %q, want hi", e.Message)
		}
	default:
		t.Fatal("expected a buffered event on the subscriber channel")
	}
}

func TestClearEmptiesBuffer(t *testing.T) {
	m := NewManager(10)
	m.Publish(Event{Service: "api", Message: "x"})
	m.Clear()
	if got := len(m.Snapshot("")); got != 0 {
		t.Fatalf("buffer len = %d, want 0 after Clear", got)
	}
}
