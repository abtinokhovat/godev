// Package process runs services as independent OS processes, per
// sections 12 and 52: each service gets its own PID and process group,
// managed through a small platform-abstracted interface.
package process

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"time"
)

// OutputLine is one line of output from a running process.
type OutputLine struct {
	Stream Stream
	Text   string
}

type Stream int

const (
	StreamStdout Stream = iota
	StreamStderr
)

// StartOptions configures a process launch.
type StartOptions struct {
	Binary string
	Args   []string
	Dir    string
	Env    []string // full environment (os.Environ() + overrides already merged)
	Output chan<- OutputLine
	// Name, when set, becomes the process's argv[0] instead of Binary's
	// full path - so `ps`/`top`/Activity Monitor show the service name
	// (e.g. "api") instead of an unrecognizable cache path
	// (".cache/godev/<hash>/api/current-normal"). This only relabels
	// what the OS reports as the command name; Binary is still what
	// actually gets exec'd.
	Name string
}

// Handle represents a running process.
type Handle struct {
	cmd     *exec.Cmd
	PID     int
	done    chan struct{}
	exitErr error
}

// Start launches a binary as its own process group leader so that Stop
// can terminate it and any children together (section 51).
func Start(opts StartOptions) (*Handle, error) {
	cmd := exec.Command(opts.Binary, opts.Args...)
	if opts.Name != "" {
		cmd.Args[0] = opts.Name
	}
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	h := &Handle{cmd: cmd, PID: cmd.Process.Pid, done: make(chan struct{})}

	pump := func(r io.Reader, s Stream) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if opts.Output != nil {
				opts.Output <- OutputLine{Stream: s, Text: scanner.Text()}
			}
		}
	}

	var pumpsDone = make(chan struct{}, 2)
	go func() { pump(stdout, StreamStdout); pumpsDone <- struct{}{} }()
	go func() { pump(stderr, StreamStderr); pumpsDone <- struct{}{} }()

	go func() {
		<-pumpsDone
		<-pumpsDone
		h.exitErr = cmd.Wait()
		close(h.done)
	}()

	return h, nil
}

// Wait blocks until the process exits and returns its exit error (nil
// on a clean exit).
func (h *Handle) Wait() error {
	<-h.done
	return h.exitErr
}

// Done returns a channel closed when the process has exited.
func (h *Handle) Done() <-chan struct{} {
	return h.done
}

// Stop sends SIGTERM to the process group, waiting up to timeout before
// escalating to SIGKILL, per section 50.
func (h *Handle) Stop(timeout time.Duration) {
	terminateGroup(h.cmd.Process.Pid)
	select {
	case <-h.done:
		return
	case <-time.After(timeout):
	}
	killGroup(h.cmd.Process.Pid)
	<-h.done
}

// Kill immediately force-kills the process group.
func (h *Handle) Kill() {
	killGroup(h.cmd.Process.Pid)
	<-h.done
}

// BuildEnv merges the current process environment with service-specific
// overrides, per section 18: start from os.Environ(), then apply
// overrides without discarding the rest.
func BuildEnv(overrides map[string]string) []string {
	return buildEnvFrom(os.Environ(), overrides)
}

// buildEnvFrom is BuildEnv's testable core: it takes the base
// environment explicitly instead of always reading os.Environ().
func buildEnvFrom(env []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return env
	}
	result := make([]string, 0, len(env)+len(overrides))
	seen := make(map[string]bool, len(overrides))
	for _, kv := range env {
		key := kv
		for i, c := range kv {
			if c == '=' {
				key = kv[:i]
				break
			}
		}
		if v, ok := overrides[key]; ok {
			result = append(result, key+"="+v)
			seen[key] = true
		} else {
			result = append(result, kv)
		}
	}
	for k, v := range overrides {
		if !seen[k] {
			result = append(result, k+"="+v)
		}
	}
	return result
}
