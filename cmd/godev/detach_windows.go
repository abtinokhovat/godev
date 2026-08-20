//go:build windows

package main

import "syscall"

// Windows has no session/controlling-terminal concept to detach from
// the way Unix does; DETACHED_PROCESS (no console) plus its own
// process group is the closest equivalent, keeping the child alive
// after the launching console window closes. Defined locally rather
// than relied on from syscall since these creation-flag constants
// aren't guaranteed exported there.
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
