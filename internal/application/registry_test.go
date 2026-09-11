package application

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistrySetLoadRemoveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r := newRegistry(path)

	if got := r.load(); len(got) != 0 {
		t.Fatalf("load() on a nonexistent file = %v, want empty", got)
	}

	started := time.Now().Round(time.Second)
	r.set("api", registryEntry{PID: 1234, StartedAt: started})
	r.set("worker", registryEntry{PID: 5678, StartedAt: started})

	got := r.load()
	if len(got) != 2 {
		t.Fatalf("load() after two sets = %v, want 2 entries", got)
	}
	if got["api"].PID != 1234 {
		t.Errorf("api.PID = %d, want 1234", got["api"].PID)
	}
	if !got["api"].StartedAt.Equal(started) {
		t.Errorf("api.StartedAt = %v, want %v", got["api"].StartedAt, started)
	}

	r.remove("api")
	got = r.load()
	if _, ok := got["api"]; ok {
		t.Error("expected api to be gone after remove")
	}
	if _, ok := got["worker"]; !ok {
		t.Error("expected worker to survive removing a different entry")
	}

	// A second registry pointed at the same file sees the same state -
	// this is the whole point: a fresh Supervisor reads what a
	// previous one wrote.
	r2 := newRegistry(path)
	got2 := r2.load()
	if len(got2) != 1 || got2["worker"].PID != 5678 {
		t.Fatalf("a fresh registry at the same path loaded %v, want just worker/5678", got2)
	}
}

func TestRegistryRemoveNonexistentIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r := newRegistry(path)
	r.remove("nothing-here") // must not panic or create a file
	if got := r.load(); len(got) != 0 {
		t.Errorf("load() = %v, want empty", got)
	}
}

func TestRegistryLoadOnCorruptFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := newRegistry(path)
	if got := r.load(); len(got) != 0 {
		t.Fatalf("load() on a corrupt file = %v, want empty (best-effort, not a panic)", got)
	}

	// A subsequent set() must still work, overwriting the corrupt file
	// rather than being permanently wedged by it.
	r.set("api", registryEntry{PID: 1})
	if got := r.load(); got["api"].PID != 1 {
		t.Fatalf("load() after set() on a previously-corrupt file = %v", got)
	}
}
