//go:build linux || darwin

package main

import "syscall"

// detachSysProcAttr fully detaches the child from this process's
// controlling terminal and session (a new session via setsid), so it
// isn't killed when the shell that launched it exits or the terminal
// closes.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
