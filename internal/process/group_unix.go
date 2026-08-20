//go:build linux || darwin

package process

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateGroup(pid int) {
	// Negative pid targets the whole process group (see setpgid above).
	syscall.Kill(-pid, syscall.SIGTERM)
}

func killGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}
