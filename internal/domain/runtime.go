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
	// Ports lists every TCP port the running process is currently
	// observed listening on (discovered from the outside via
	// internal/ports - never configured, since nothing here requires
	// the service to declare its own port). Empty until the OS-level
	// poll finds one, which can take a moment after startup; nil for
	// a service that isn't running or doesn't listen on anything.
	Ports []int
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
