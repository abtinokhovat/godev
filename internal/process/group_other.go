//go:build !linux && !darwin

package process

import (
	"os"
	"os/exec"
)

// Windows fallback: no process-group semantics yet (section 52 notes
// Windows support comes after Linux/macOS). We kill just the direct
// process for now.

func setProcessGroup(cmd *exec.Cmd) {}

func terminateGroup(pid int) {
	killGroup(pid)
}

func killGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

// isAlive is unreachable in practice on this fallback platform:
// argvMatchesName below always fails, so Supervisor.adoptRunning never
// calls Adopt (which is what actually relies on isAlive) here at all.
// os.FindProcess essentially always succeeds on Windows regardless of
// whether pid is real, so this can't be a meaningful check without a
// real process-group story - not worth building out further until
// Windows gets one.
func isAlive(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}

// argvMatchesName always fails on this fallback platform - without a
// process-group story here yet, adopting a possibly-reused PID isn't
// worth the risk; a fresh process is started instead (see
// Supervisor.adoptRunning).
func argvMatchesName(pid int, name string) bool {
	return false
}
