package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDebounceCollapsesBurstIntoOneChange(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A wide debounce window relative to the burst's own spacing (10ms
	// gaps into a 500ms window) so a scheduling delay under load (a
	// loaded CI runner, -race's overhead, ...) can't push a gap between
	// two writes past the window and split one burst into two flushes -
	// that's a test-timing flake, not a real debounce bug, since
	// watcher.loop's timer already resets on every event.
	const debounceWindow = 500 * time.Millisecond
	w, err := New(root, debounceWindow)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	// Burst of rapid writes, well within the debounce window.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(file, []byte("package main\n//"+time.Now().String()), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case <-w.Changes():
	case <-time.After(5 * time.Second):
		t.Fatal("expected a debounced change")
	}

	// No second change should follow - the burst was one batch. Wait
	// past a full debounce window plus margin, since a genuine
	// double-flush wouldn't fire until roughly one window after the
	// last write.
	select {
	case <-w.Changes():
		t.Fatal("unexpected second change from a single debounced burst")
	case <-time.After(debounceWindow + 300*time.Millisecond):
	}
}

func TestIgnoresNonGoFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := New(root, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case c := <-w.Changes():
		t.Fatalf("unexpected change for a non-Go file: %+v", c)
	case <-time.After(300 * time.Millisecond):
	}
}
