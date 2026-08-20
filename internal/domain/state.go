package domain

// State is a service's lifecycle state, per the state machine in the
// implementation plan (discovered -> building -> starting -> running ->
// stopping/crashed -> stopped/restarting).
type State int

const (
	StateDiscovered State = iota
	StateBuilding
	StateStarting
	StateRunning
	StateStopping
	StateStopped
	StateCrashed
	StateRestarting
	StateBuildFailed
)

func (s State) String() string {
	switch s {
	case StateDiscovered:
		return "DISCOVERED"
	case StateBuilding:
		return "BUILDING"
	case StateStarting:
		return "STARTING"
	case StateRunning:
		return "RUNNING"
	case StateStopping:
		return "STOPPING"
	case StateStopped:
		return "STOPPED"
	case StateCrashed:
		return "CRASHED"
	case StateRestarting:
		return "RESTARTING"
	case StateBuildFailed:
		return "BUILD FAILED"
	default:
		return "UNKNOWN"
	}
}

// DebugState is the debugger's lifecycle state, kept separate from the
// service's own State so debugging never overloads normal run state.
type DebugState int

const (
	DebugStopped DebugState = iota
	DebugStarting
	DebugRunning
	DebugStopping
	DebugFailed
)

func (s DebugState) String() string {
	switch s {
	case DebugStopped:
		return "STOPPED"
	case DebugStarting:
		return "STARTING"
	case DebugRunning:
		return "RUNNING"
	case DebugStopping:
		return "STOPPING"
	case DebugFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}
