package application

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// registryEntry is one service's last-known running state, as
// recorded on disk - just enough for a fresh Supervisor to tell
// whether a process a previous instance started might still be the
// one it thinks it is. Liveness alone isn't proof of that: the OS is
// free to reuse a PID for something completely unrelated once the
// original process is gone, so the actual safety check also verifies
// the live process's argv[0] still matches the service name (see
// process.ArgvMatchesName and Supervisor.adoptRunning).
type registryEntry struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// registry persists registryEntry per service name to a single JSON
// file under the project's cache directory, so a crashed-and-restarted
// godev (foreground or --detach) can recognize and adopt processes a
// previous instance left running instead of either duplicating them or
// losing track of them forever. Every write replaces the whole file
// atomically (write to a temp file, then rename) - the same pattern
// internal/builder already uses for installing a binary, for the same
// reason: a crash mid-write must never leave a half-written,
// unparseable file behind.
//
// Best-effort throughout: a filesystem problem here should never break
// the actual service lifecycle it's just bookkeeping for. A missing or
// corrupt file is treated as "nothing recorded", not an error.
type registry struct {
	mu   sync.Mutex
	path string
}

func newRegistry(path string) *registry {
	return &registry{path: path}
}

// load returns every currently-recorded entry, keyed by service name.
func (r *registry) load() map[string]registryEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readLocked()
}

// set records (or replaces) name's entry.
func (r *registry) set(name string, e registryEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.readLocked()
	entries[name] = e
	r.writeLocked(entries)
}

// remove clears name's entry, if any - a no-op if it's already gone.
func (r *registry) remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.readLocked()
	if _, ok := entries[name]; !ok {
		return
	}
	delete(entries, name)
	r.writeLocked(entries)
}

func (r *registry) readLocked() map[string]registryEntry {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return map[string]registryEntry{}
	}
	var out map[string]registryEntry
	if err := json.Unmarshal(data, &out); err != nil || out == nil {
		return map[string]registryEntry{}
	}
	return out
}

func (r *registry) writeLocked(entries map[string]registryEntry) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return
	}
	tmp := fmt.Sprintf("%s.tmp-%d", r.path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, r.path)
}
