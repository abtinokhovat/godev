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

	w, err := New(root, 100*time.Millisecond)
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
	case <-time.After(2 * time.Second):
		t.Fatal("expected a debounced change")
	}

	// No second change should follow immediately - the burst was one batch.
	select {
	case <-w.Changes():
		t.Fatal("unexpected second change from a single debounced burst")
	case <-time.After(300 * time.Millisecond):
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
