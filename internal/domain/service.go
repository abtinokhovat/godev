// Package domain holds the core types shared across godev's subsystems.
package domain

// Service is the static description of a runnable service: what to
// build (if anything), where it lives, and how it should behave. It
// does not carry any runtime state (PID, current binary, etc) - see
// ServiceRuntime for that.
//
// A Service comes from one of two places:
//   - Go discovery (`go list`): Package is set to the main package's
//     import path, Command is empty, and godev compiles it before
//     running the resulting binary.
//   - A manual .godev.yaml entry or an imported JetBrains run
//     configuration: Command is set directly (e.g. ["node","server.js"]
//     or ["npm","run","dev"]) and Package is empty. There is no build
//     step for these - godev execs Command as-is. Debugging (currently
//     Delve-only) is not supported for Command-based services.
type Service struct {
	Name        string
	Package     string   // Go import path, e.g. "./cmd/api". Empty for Command-based services.
	Command     []string // explicit run command for non-Go/manual services. Empty for Go services.
	Directory   string   // absolute working directory
	Args        []string
	Env         map[string]string
	AutoStart   bool
	AutoRestart bool
	HotReload   bool
	Watch       WatchConfig
	Group       []string // hierarchical group path for the TUI sidebar and `godev run <group>`; empty = ungrouped
}

// IsCommand reports whether this service runs an explicit command
// rather than a compiled Go binary - i.e. whether it skips the build
// step entirely.
func (s Service) IsCommand() bool {
	return len(s.Command) > 0
}

// WatchConfig controls which files trigger a rebuild for a service.
type WatchConfig struct {
	Include []string
	Exclude []string
}
