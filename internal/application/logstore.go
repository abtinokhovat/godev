package application

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/abtinokhovat/godev/internal/logs"
)

// seedLinesPerService caps how much of each service's prior-run log
// gets loaded back into memory at startup (see seedHistory) - enough
// to feel like nothing was lost, without one chatty service's huge
// log file crowding out every other service's history in the shared
// in-memory buffer it all lands in afterward.
const seedLinesPerService = 1000

// serviceScopeBacklog caps how many lines Supervisor.ServiceLogs reads
// back off disk for a single service - generous, since a service's
// log file is no longer rotated away mid-run (see reset), just bounded
// enough that a service which has logged for days doesn't make
// switching to it in the TUI read an unbounded file.
const serviceScopeBacklog = 20000

// logWriterQueue is how many events logWriter will buffer ahead of the
// disk while its background goroutine catches up - generous enough
// that an ordinary burst (a noisy build, several services starting at
// once) never has to drop anything in practice.
const logWriterQueue = 4096

// logWriter appends every published logs.Event to a per-service file
// on disk (NDJSON: one JSON object per line, which - unlike a naive
// "timestamp\tmessage" format - survives a message with embedded
// newlines, like a multi-line build failure, without corrupting line
// boundaries on the way back in). Installed as the Supervisor's
// logs.Manager sink, so it's called for every log line from every
// call site (build output, lifecycle messages, pumped stdout/stderr)
// with no changes needed at any of them.
//
// The actual file write happens on a dedicated background goroutine,
// off write's own call path: write only ever does a non-blocking
// channel send (dropping the line, same as a slow logs.Manager
// subscriber would, if the queue is ever actually full) rather than
// touching the filesystem itself. This matters beyond just avoiding
// I/O latency in the logging path generally - Supervisor.startProcess
// calls s.log() for its own "started" message before launching the
// goroutines that watch the new process, and synchronous disk I/O
// there once measurably delayed that launch enough for a Stop()
// called immediately after to race all the way to Stopped before
// monitor() ever got scheduled to see it, misreporting a clean stop
// as a crash.
//
// This is what makes log history survive a godev crash - a real file
// keeps growing untouched even if the process reading it dies, unlike
// the in-memory buffer or a pipe. A service's file is never rotated or
// trimmed while it's running, however long that is or however much it
// logs - only a deliberate Stop (see reset) clears it, so mid-run
// history search is never missing something that "aged out". Best-
// effort throughout: a filesystem problem here should never take down
// log delivery to the TUI, which still works via logs.Manager's
// in-memory path regardless.
type logWriter struct {
	dir   string
	queue chan logs.Event

	mu    sync.Mutex
	files map[string]*os.File
}

func newLogWriter(dir string) *logWriter {
	w := &logWriter{
		dir:   dir,
		files: map[string]*os.File{},
		queue: make(chan logs.Event, logWriterQueue),
	}
	go w.drain()
	return w
}

func logFilePath(dir, service string) string {
	return filepath.Join(dir, service+".log")
}

// write is logs.Manager's sink: a non-blocking enqueue, never the
// actual file I/O - see the type doc for why that distinction matters.
func (w *logWriter) write(e logs.Event) {
	if w.dir == "" || e.Service == "" {
		return
	}
	select {
	case w.queue <- e:
	default:
	}
}

// drain is the one goroutine that actually touches the filesystem,
// serializing every write without needing write's own callers to
// block on each other. It runs for the life of the process rather
// than being explicitly stopped by closeAll: the queue is never
// closed (a concurrent write() racing a close would panic - a sender
// checking for a full channel via select/default has no way to also
// check for a closed one), so any goroutine still calling s.log()
// during shutdown (monitor, pollPorts, ...) is always safe to do so.
// The cost is that whatever's still sitting in the queue at process
// exit never reaches disk - an acceptable loss for what's already
// documented as best-effort persistence, and a single idle goroutine
// blocked on an empty channel read costs nothing worth avoiding it for.
func (w *logWriter) drain() {
	for e := range w.queue {
		w.persist(e)
	}
}

func (w *logWriter) persist(e logs.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := w.fileLocked(e.Service)
	if err != nil {
		return
	}
	f.Write(data)
	f.Write([]byte("\n"))
}

func (w *logWriter) fileLocked(service string) (*os.File, error) {
	if f, ok := w.files[service]; ok {
		return f, nil
	}
	path := logFilePath(w.dir, service)
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w.files[service] = f
	return f, nil
}

// reset closes and removes a service's on-disk log file. Called only
// when a service is deliberately stopped (see Supervisor.Stop) - never
// on crash, where the log up to the moment it died is exactly what
// crash recovery needs and must survive untouched. This is the
// "prune" half of the durability story: a file grows without bound
// for as long as a service runs (see fileLocked), and only goes away
// once that run is over, so a later start of the same service begins
// a clean file instead of one that keeps accumulating across unrelated
// runs forever.
func (w *logWriter) reset(service string) {
	if w.dir == "" || service == "" {
		return
	}
	w.mu.Lock()
	if f, ok := w.files[service]; ok {
		f.Close()
		delete(w.files, service)
	}
	w.mu.Unlock()
	os.Remove(logFilePath(w.dir, service))
}

// closeAll closes every open per-service file - called from
// Supervisor.Shutdown for a clean exit; harmless to skip on a crash,
// since the OS closes file descriptors on process exit regardless.
// Does not stop drain (see its doc comment for why) - a write() racing
// this is just a few more lines written to a file about to be closed
// anyway, never a panic.
func (w *logWriter) closeAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for name, f := range w.files {
		f.Close()
		delete(w.files, name)
	}
}

// readTail returns up to n events from the tail of service's on-disk
// log file. Best-effort: a missing file or unparseable line yields
// whatever could be read, never an error the caller needs to handle.
func readTail(dir, service string, n int) []logs.Event {
	f, err := os.Open(logFilePath(dir, service))
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
	}

	out := make([]logs.Event, 0, len(lines))
	for _, line := range lines {
		var e logs.Event
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// mergeServiceHistory combines a service's on-disk tail with its
// in-memory scrollback into one chronological list, for
// Supervisor.ServiceLogs. The two overlap rather than concatenate
// cleanly: every event lands in both eventually (the disk writer is
// Manager's sink, called synchronously in Publish order), but the
// async disk drain can lag behind what's already visible in memory.
// Taking disk as the base and appending only the in-memory events
// strictly newer than disk's last timestamp avoids double-counting
// that overlap without needing to dedupe event-by-event.
func mergeServiceHistory(disk, live []logs.Event) []logs.Event {
	if len(disk) == 0 {
		return live
	}
	if len(live) == 0 {
		return disk
	}
	cutoff := disk[len(disk)-1].Time
	out := append([]logs.Event{}, disk...)
	for _, e := range live {
		if e.Time.After(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

// seedHistory loads each service's recent on-disk log history (see
// readTail) and installs it as the Manager's starting scrollback, in
// overall chronological order - restoring "what was here before" for
// every service that has a log file already, whether that's from a
// clean previous run or one this instance is about to adopt a
// still-running process from (see adoptRunning). Best-effort: services
// with no log file yet (a first-ever run) simply contribute nothing.
func (s *Supervisor) seedHistory() {
	if s.logDir == "" {
		return
	}
	var all []logs.Event
	for _, name := range s.serviceNames() {
		all = append(all, readTail(s.logDir, name, seedLinesPerService)...)
	}
	if len(all) == 0 {
		return
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Time.Before(all[j].Time) })
	s.logsMgr.SeedHistory(all)
}
