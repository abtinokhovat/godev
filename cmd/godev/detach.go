package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/abtinokhovat/godev/internal/daemon"
)

// detachedMarker is the hidden first argument spawnDetached re-execs
// itself with. It is never something a user types - it isn't listed in
// usage, and main's dispatch checks for it before any normal parsing.
const detachedMarker = "__daemon-run__"

// spawnDetached re-execs the current binary fully detached from this
// process's controlling terminal/session (so it outlives the shell
// that launched it) and returns as soon as the child has started - it
// does not wait for it to finish, since the whole point is for it to
// keep running after this process exits.
func spawnDetached(projectRoot string, targets []string) (pid int, err error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding godev's own binary path: %w", err)
	}

	args := append([]string{detachedMarker}, targets...)
	cmd := exec.Command(exe, args...)
	cmd.Dir = projectRoot
	cmd.SysProcAttr = detachSysProcAttr()

	// Once detached, stdout/stderr have nowhere meaningful to go - the
	// service logs already flow through the control socket to whatever
	// attaches. Redirect to a per-project log file so an early failure
	// (before the socket even exists) isn't silently lost.
	if paths, perr := daemon.ResolvePaths(projectRoot); perr == nil {
		if merr := os.MkdirAll(paths.Dir, 0o755); merr == nil {
			if f, ferr := os.OpenFile(paths.Log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); ferr == nil {
				defer f.Close()
				cmd.Stdout = f
				cmd.Stderr = f
			}
		}
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}
