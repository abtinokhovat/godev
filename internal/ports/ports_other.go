//go:build !linux && !darwin && !windows

package ports

// listenPortsForPID has no implementation on this platform - port
// discovery just reports nothing rather than failing anything that
// depends on it.
func listenPortsForPID(pid int) ([]int, error) {
	return nil, nil
}
