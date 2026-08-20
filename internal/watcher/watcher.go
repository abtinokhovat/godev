// Package watcher watches a Go project's source tree for changes and
// emits debounced Change events, per sections 19-21 of the plan.
package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Change is a debounced batch of filesystem events.
type Change struct {
	Paths []string
}

var defaultExcludeDirs = []string{
	".git", "vendor", "node_modules", "tmp", "dist", "build", ".godev", ".cache",
}

// Watcher recursively watches a root directory for *.go / go.mod /
// go.sum changes and debounces bursts of events into single Change
// notifications.
type Watcher struct {
	root     string
	debounce time.Duration
	fsw      *fsnotify.Watcher
	changes  chan Change
	done     chan struct{}
}

// New creates a Watcher rooted at root with the given debounce window.
// A debounce <= 0 defaults to 200ms, per section 21.
func New(root string, debounce time.Duration) (*Watcher, error) {
	if debounce <= 0 {
		debounce = 200 * time.Millisecond
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		root:     root,
		debounce: debounce,
		fsw:      fsw,
		changes:  make(chan Change, 8),
		done:     make(chan struct{}),
	}
	if err := w.addTree(root); err != nil {
		fsw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// Changes returns the channel of debounced change batches.
func (w *Watcher) Changes() <-chan Change {
	return w.changes
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	close(w.done)
	return w.fsw.Close()
}

func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable entries
		}
		if !d.IsDir() {
			return nil
		}
		if isExcluded(path) {
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

func isExcluded(path string) bool {
	base := filepath.Base(path)
	for _, ex := range defaultExcludeDirs {
		if base == ex {
			return true
		}
	}
	return false
}

func isWatchedFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".go") || base == "go.mod" || base == "go.sum"
}

func (w *Watcher) loop() {
	var timer *time.Timer
	pending := make(map[string]bool)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		paths := make([]string, 0, len(pending))
		for p := range pending {
			paths = append(paths, p)
		}
		pending = make(map[string]bool)
		select {
		case w.changes <- Change{Paths: paths}:
		default:
		}
	}

	var timerC <-chan time.Time
	for {
		select {
		case <-w.done:
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			// New directories need watching too (e.g. `mkdir` inside root).
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() && !isExcluded(ev.Name) {
					w.fsw.Add(ev.Name)
				}
			}
			if !isWatchedFile(ev.Name) {
				continue
			}
			pending[ev.Name] = true
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(w.debounce)
			timerC = timer.C
		case <-timerC:
			flush()
			timerC = nil
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}
