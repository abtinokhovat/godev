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
