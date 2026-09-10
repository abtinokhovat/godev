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

func TestLogWriterRotatesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	w := newLogWriter(dir)

	// Force a small effective limit by writing more than
	// logFileMaxBytes worth of content isn't practical in a fast unit
	// test, so instead this pre-creates an oversized file and confirms
	// the very next write triggers rotation rather than growing it
	// further - the same check fileLocked does internally.
	path := logFilePath(dir, "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	oversized := strings.Repeat("x", logFileMaxBytes+1)
	if err := os.WriteFile(path, []byte(oversized), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w.write(logs.Event{Time: time.Now(), Service: "api", Stream: logs.StreamStdout, Message: "after rotation"})
	drainSettle()

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("expected a .1 backup after rotation, ReadFile: %v", err)
	}
	if len(backup) != len(oversized) {
		t.Errorf("backup file size = %d, want the original oversized content (%d)", len(backup), len(oversized))
	}

	got := readTail(dir, "api", 10)
	if len(got) != 1 || got[0].Message != "after rotation" {
		t.Fatalf("readTail after rotation = %+v, want just the new line", got)
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
