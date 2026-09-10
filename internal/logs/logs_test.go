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

func TestSinkReceivesEveryEventRegardlessOfSubscribers(t *testing.T) {
	m := NewManager(10)
	var got []Event
	m.SetSink(func(e Event) { got = append(got, e) })

	m.Publish(Event{Service: "api", Message: "one"})
	m.Publish(Event{Service: "worker", Message: "two"})

	if len(got) != 2 {
		t.Fatalf("sink received %d events, want 2 (got %+v)", len(got), got)
	}
	if got[0].Message != "one" || got[1].Message != "two" {
		t.Errorf("sink events = %+v, want one then two in order", got)
	}
}

func TestNilSinkIsFine(t *testing.T) {
	m := NewManager(10)
	m.Publish(Event{Service: "api", Message: "x"}) // must not panic with no sink set
	if got := len(m.Snapshot("")); got != 1 {
		t.Fatalf("buffer len = %d, want 1", got)
	}
}

func TestSeedHistoryInstallsScrollbackAheadOfCurrentBuffer(t *testing.T) {
	m := NewManager(10)
	m.Publish(Event{Service: "api", Message: "live"})

	sinkCalls := 0
	m.SetSink(func(Event) { sinkCalls++ })
	subCh, cancel := m.Subscribe(4)
	defer cancel()

	m.SeedHistory([]Event{
		{Service: "api", Message: "old 1"},
		{Service: "api", Message: "old 2"},
	})

	snap := m.Snapshot("")
	if len(snap) != 3 {
		t.Fatalf("Snapshot() after SeedHistory = %d events, want 3 (got %+v)", len(snap), snap)
	}
	want := []string{"old 1", "old 2", "live"}
	for i, w := range want {
		if snap[i].Message != w {
			t.Errorf("snap[%d].Message = %q, want %q", i, snap[i].Message, w)
		}
	}

	if sinkCalls != 0 {
		t.Errorf("SeedHistory should not invoke the sink (it would re-persist already-persisted lines), got %d calls", sinkCalls)
	}
	select {
	case e := <-subCh:
		t.Errorf("SeedHistory should not notify live subscribers (it would re-broadcast old lines as new), got %+v", e)
	default:
	}
}

func TestSeedHistoryRespectsMaxBuffer(t *testing.T) {
	m := NewManager(2)
	m.SeedHistory([]Event{
		{Service: "api", Message: "1"},
		{Service: "api", Message: "2"},
		{Service: "api", Message: "3"},
	})
	snap := m.Snapshot("")
	if len(snap) != 2 {
		t.Fatalf("Snapshot() len = %d, want 2 (trimmed to maxBuffer)", len(snap))
	}
	if snap[0].Message != "2" || snap[1].Message != "3" {
		t.Errorf("expected the most recent 2 events to survive trimming, got %+v", snap)
	}
}
