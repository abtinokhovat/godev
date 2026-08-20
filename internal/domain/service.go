// Package domain holds the core types shared across godev's subsystems.
package domain

// Service is the static description of a runnable Go application:
// what to build, where it lives, and how it should behave. It does not
// carry any runtime state (PID, current binary, etc) - see ServiceRuntime
// for that.
type Service struct {
	Name        string
	Package     string // import path, e.g. "./cmd/api"
	Directory   string // absolute directory containing the main package
	Args        []string
	Env         map[string]string
	AutoStart   bool
	AutoRestart bool
	HotReload   bool
	Watch       WatchConfig
}

// WatchConfig controls which files trigger a rebuild for a service.
type WatchConfig struct {
	Include []string
	Exclude []string
}
