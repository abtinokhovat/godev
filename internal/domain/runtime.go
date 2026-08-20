package domain

import "time"

// ServiceRuntime is the mutable "what is happening right now" counterpart
// to the static Service configuration.
type ServiceRuntime struct {
	State        State
	PID          int
	BinaryPath   string
	StartedAt    time.Time
	LastError    string
	RestartCount int
	Debug        *DebugSession
}

// DebugSession describes a running (or starting/stopping) Delve headless
// server attached to a service's debug binary.
type DebugSession struct {
	Service    string
	DelvePID   int
	Host       string
	Port       int
	BinaryPath string
	State      DebugState
	Error      string
}
