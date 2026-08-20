package daemon

import (
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const probeTimeout = 500 * time.Millisecond

// Status is what a caller (godev run --detach's single-instance check,
// godev attach, godev kill) needs to know about a project's detached
// instance before acting.
type Status struct {
	Running bool
	PID     int // best-effort, from the PID file; 0 if unknown
	Paths   Paths
}

// Probe reports whether a detached instance for projectRoot is
// actually reachable. Dialing the socket - not just checking whether a
// PID file exists, or whether that PID happens to be alive - is the
// source of truth: it's the one check that can't be fooled by a stale
// PID reused by an unrelated process, and it doubles as a liveness
// check of the socket itself. A dial failure cleans up a stale
// socket/PID file pair so the next start doesn't have to.
func Probe(projectRoot string) (Status, error) {
	paths, err := ResolvePaths(projectRoot)
	if err != nil {
		return Status{}, err
	}
	pid, _ := readPID(paths.PID) // best-effort; absence/corruption isn't fatal here

	conn, err := net.DialTimeout("unix", paths.Socket, probeTimeout)
	if err != nil {
		os.Remove(paths.Socket)
		os.Remove(paths.PID)
		return Status{Running: false, PID: pid, Paths: paths}, nil
	}
	conn.Close()
	return Status{Running: true, PID: pid, Paths: paths}, nil
}

// WritePID records the current process's PID for Status's benefit.
// It's advisory only - Probe never trusts it alone.
func WritePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}
