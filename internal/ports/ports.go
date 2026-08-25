// Package ports discovers which TCP ports a running process is
// listening on, purely by observing the OS - no cooperation or
// instrumentation from the process itself is required or possible.
// The mechanism is necessarily platform-specific (see ports_linux.go,
// ports_darwin.go, ports_windows.go): there's no portable API for
// "what ports does PID X own".
package ports

import "sort"

// ForPID returns every distinct TCP port process pid is currently
// listening on, sorted ascending - a process can (and often does)
// listen on more than one (e.g. an app port plus a metrics/debug
// port, or the same port bound on both IPv4 and IPv6). Best-effort:
// an error (permission denied, missing platform tool, pid not found -
// most commonly because it already exited) means "nothing to report
// right now", not a fatal condition; callers should treat it exactly
// like an empty result, not surface it as a service error.
func ForPID(pid int) ([]int, error) {
	found, err := listenPortsForPID(pid)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(found))
	out := make([]int, 0, len(found))
	for _, p := range found {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Ints(out)
	return out, nil
}
