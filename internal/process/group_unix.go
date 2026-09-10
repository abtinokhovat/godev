//go:build linux || darwin

package process

import (
	"os/exec"
	"strconv"
	"strings"
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

// isAlive reports whether pid names a live process, via the standard
// POSIX "does this process exist" trick: signal 0 delivers no actual
// signal, just the permission/existence check.
func isAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// argvMatchesName reports whether pid's argv[0] is still name - the
// fingerprint that makes adopting a PID from the on-disk registry safe
// despite the OS being free to reuse PIDs once their original process
// exits. `ps -o args=` (not `-o comm=`, which reports the executable's
// own on-disk name and ignores an overridden argv[0] entirely) reflects
// exactly what process.StartOptions.Name sets argv[0] to for every
// service godev starts, so a mismatch here means pid now belongs to a
// completely unrelated process - time to give up and start fresh, not
// adopt something that just happens to share a number.
func argvMatchesName(pid int, name string) bool {
	out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	args := strings.TrimSpace(string(out))
	if args == "" {
		return false
	}
	first, _, _ := strings.Cut(args, " ")
	return first == name
}
