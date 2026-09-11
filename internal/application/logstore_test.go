package application

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/logs"
)

// drainSettle gives logWriter's background drain goroutine a moment to
// actually persist whatever was just enqueued - write() only enqueues,
// it never blocks until the line is on disk (see logWriter's doc
// comment for why), so a test reading the file back right after write()
// needs to wait for that to happen first.
func drainSettle() { time.Sleep(50 * time.Millisecond) }

func TestLogWriterPersistsAndReadTailRoundTrips(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)

	w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamStdout, Message: "hello"})
	w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamSystem, Message: "line 2\nwith an embedded newline"})
	w.write(logs.Event{Time: time.Now(), Service: "worker", Stream: logs.StreamStdout, Message: "unrelated"})
	drainSettle()

	got := readTail(dir, "api", 10)
	if len(got) != 2 {
		t.Fatalf("readTail(api) = %d events, want 2 (got %+v)", len(got), got)
	}
	if got[0].Message != "hello" {
		t.Errorf("got[0].Message = %q, want %q", got[0].Message, "hello")
	}
	if got[1].Message != "line 2\nwith an embedded newline" {
		t.Errorf("embedded newline not preserved through the round trip, got %q", got[1].Message)
	}
	if got[1].Stream != logs.StreamSystem {
		t.Errorf("got[1].Stream = %v, want StreamSystem", got[1].Stream)
	}

	if got := readTail(dir, "worker", 10); len(got) != 1 || got[0].Message != "unrelated" {
		t.Errorf("readTail(worker) = %+v, want one event with message %q", got, "unrelated")
	}
}

func TestReadTailCapsToRequestedCount(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)
	for i := 0; i < 20; i++ {
		w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamStdout, Message: "line"})
	}
	drainSettle()

	got := readTail(dir, "api", 5)
	if len(got) != 5 {
		t.Fatalf("readTail with n=5 returned %d events, want 5", len(got))
	}
}

func TestReadTailOnMissingFileReturnsNil(t *testing.T) {
	if got := readTail(t.TempDir(), "nonexistent", 10); got != nil {
		t.Errorf("readTail on a missing file = %v, want nil", got)
	}
}

// TestLogWriterNeverRotatesMidRun guards the actual bug report: a
// service's log used to get rotated away at a fixed byte size even
// while it kept running, silently truncating history a user might
// still be searching through. A file just has to keep growing,
// however big it gets, for as long as the service is up - see reset
// for the only thing that's now allowed to clear it.
func TestLogWriterNeverRotatesMidRun(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)

	path := logFilePath(dir, "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	oversized := strings.Repeat("x", 64*1024) + "\n"
	if err := os.WriteFile(path, []byte(oversized), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamStdout, Message: "still running"})
	drainSettle()

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("a .1 backup should never be created (rotation is gone), stat err = %v", err)
	}
	got := readTail(dir, "api", 10)
	if len(got) != 1 || got[0].Message != "still running" {
		t.Fatalf("readTail = %+v, want just the one new line appended after the pre-existing content", got)
	}
}

// TestLogWriterResetClearsFile guards the "prune on stop" half: a
// deliberately stopped service's file should be gone, so its next run
// starts clean instead of accumulating across unrelated runs forever.
func TestLogWriterResetClearsFile(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)

	w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamStdout, Message: "hello"})
	drainSettle()

	if got := readTail(dir, "api", 10); len(got) != 1 {
		t.Fatalf("precondition failed: readTail = %+v, want 1 event before reset", got)
	}

	w.reset("api")

	if _, err := os.Stat(logFilePath(dir, "api")); !os.IsNotExist(err) {
		t.Fatalf("log file should be removed after reset, stat err = %v", err)
	}
	if got := readTail(dir, "api", 10); len(got) != 0 {
		t.Fatalf("readTail after reset = %+v, want none", got)
	}

	// A write after reset should transparently recreate the file -
	// reset must not leave the writer unable to persist for that
	// service again on its next run.
	w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamStdout, Message: "fresh run"})
	drainSettle()
	if got := readTail(dir, "api", 10); len(got) != 1 || got[0].Message != "fresh run" {
		t.Fatalf("readTail after post-reset write = %+v, want just %q", got, "fresh run")
	}
}

func TestSeedHistoryMergesServicesInChronologicalOrder(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)

	now := time.Now()
	w.write(logs.Event{Time: now.Add(-2 * time.Second), Service: "api", Stream: logs.StreamStdout, Message: "api first"})
	w.write(logs.Event{Time: now.Add(-1 * time.Second), Service: "worker", Stream: logs.StreamStdout, Message: "worker second"})
	w.write(logs.Event{Time: now, Service: "api", Stream: logs.StreamStdout, Message: "api third"})
	drainSettle()

	s := &Supervisor{logDir: dir, order: []string{"api", "worker"}, entries: map[string]*serviceEntry{
		"api":    {},
		"worker": {},
	}, logsMgr: logs.NewManager(100)}

	s.seedHistory()

	snap := s.logsMgr.Snapshot("")
	if len(snap) != 3 {
		t.Fatalf("Snapshot() after seedHistory = %d events, want 3 (got %+v)", len(snap), snap)
	}
	want := []string{"api first", "worker second", "api third"}
	for i, w := range want {
		if snap[i].Message != w {
			t.Errorf("snap[%d].Message = %q, want %q (order should be chronological across services)", i, snap[i].Message, w)
		}
	}
}

func TestMergeServiceHistorySkipsWhatDiskAlreadyHas(t *testing.T) {
	base := time.Now()
	disk := []logs.Event{
		{Time: base, Message: "one"},
		{Time: base.Add(time.Second), Message: "two"},
	}
	live := []logs.Event{
		{Time: base.Add(time.Second), Message: "two"},       // already the disk tail's last event
		{Time: base.Add(2 * time.Second), Message: "three"}, // newer than disk, must survive
	}

	got := mergeServiceHistory(disk, live)
	want := []string{"one", "two", "three"}
	if len(got) != len(want) {
		t.Fatalf("mergeServiceHistory = %d events, want %d (got %+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("got[%d].Message = %q, want %q", i, got[i].Message, w)
		}
	}
}

// TestServiceLogsSurvivesSharedBufferEviction guards the actual bug
// report: switching the log view to a quiet service used to show an
// empty page once a noisier service's volume pushed it out of
// logs.Manager's shared, globally-capped buffer. Supervisor.ServiceLogs
// falls back to the durable per-service disk file exactly for this.
func TestServiceLogsSurvivesSharedBufferEviction(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)
	w.write(logs.Event{Time: time.Now(), Service: "quiet", Stream: logs.StreamStdout, Message: "quiet's only line"})
	drainSettle()

	mgr := logs.NewManager(2) // small enough for "noisy" to fully evict "quiet"
	for i := 0; i < 5; i++ {
		mgr.Publish(logs.Event{Service: "noisy", Stream: logs.StreamStdout, Message: "spam"})
	}
	if snap := mgr.Snapshot("quiet"); len(snap) != 0 {
		t.Fatalf("precondition failed: quiet should already be evicted from the shared buffer, got %+v", snap)
	}

	s := &Supervisor{logDir: dir, logsMgr: mgr}
	got := s.ServiceLogs("quiet")
	if len(got) != 1 || got[0].Message != "quiet's only line" {
		t.Fatalf("ServiceLogs(quiet) = %+v, want its one line recovered from disk despite shared-buffer eviction", got)
	}
}
