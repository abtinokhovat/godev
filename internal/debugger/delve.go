// Package debugger starts and stops Delve in headless mode so any DAP-
// or Delve-API-speaking client (VS Code, GoLand) can attach, per
// sections 31-37. godev never implements debugger functionality itself.
package debugger

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/abtinokhovat/godev/internal/domain"
)

const defaultBasePort = 2345

// FindAvailablePort returns the first free TCP port on 127.0.0.1
// starting at preferred, per section 35.
func FindAvailablePort(preferred int) (int, error) {
	if preferred <= 0 {
		preferred = defaultBasePort
	}
	for port := preferred; port < preferred+1000; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found starting at %d", preferred)
}

// CheckInstalled verifies `dlv` is on PATH.
func CheckInstalled() error {
	if _, err := exec.LookPath("dlv"); err != nil {
		return fmt.Errorf("dlv not found on PATH: install it with `go install github.com/go-delve/delve/cmd/dlv@latest`")
	}
	return nil
}

// Session wraps a running headless Delve process.
type Session struct {
	*domain.DebugSession
	cmd *exec.Cmd
}

// Start launches `dlv --headless --listen=host:port --api-version=2
// --accept-multiclient exec <binary> -- <args>`, per section 32.
func Start(serviceName, host string, port int, binaryPath string, args []string, dir string, env []string) (*Session, error) {
	dlvArgs := []string{
		"--headless",
		fmt.Sprintf("--listen=%s:%d", host, port),
		"--api-version=2",
		"--accept-multiclient",
		"exec", binaryPath,
	}
	if len(args) > 0 {
		dlvArgs = append(dlvArgs, "--")
		dlvArgs = append(dlvArgs, args...)
	}

	cmd := exec.Command("dlv", dlvArgs...)
	cmd.Dir = dir
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting dlv: %w", err)
	}

	sess := &Session{
		DebugSession: &domain.DebugSession{
			Service:    serviceName,
			DelvePID:   cmd.Process.Pid,
			Host:       host,
			Port:       port,
			BinaryPath: binaryPath,
			State:      domain.DebugStarting,
		},
		cmd: cmd,
	}
	return sess, nil
}

// WaitListening polls the debug port until Delve accepts connections or
// timeout elapses.
func (s *Session) WaitListening(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			s.State = domain.DebugRunning
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.State = domain.DebugFailed
	s.Error = "timed out waiting for delve to start listening"
	return errors.New(s.Error)
}

// Stop terminates the Delve process.
func (s *Session) Stop() {
	if s.cmd.Process == nil {
		return
	}
	s.State = domain.DebugStopping
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
	s.State = domain.DebugStopped
}

// Wait blocks until the dlv process exits.
func (s *Session) Wait() error {
	return s.cmd.Wait()
}

// VSCodeInstructions returns human-readable attach instructions for the
// section 38 "connection information" output.
func (s *Session) VSCodeInstructions() string {
	return fmt.Sprintf("VS Code: add a \"Go: Attach\" (dlv remote) config -> host %s, port %d", s.Host, s.Port)
}

// GoLandInstructions returns human-readable attach instructions for the
// section 39 "connection information" output.
func (s *Session) GoLandInstructions() string {
	return fmt.Sprintf("GoLand: Run -> Edit Configurations -> Go Remote -> host %s, port %d", s.Host, s.Port)
}
