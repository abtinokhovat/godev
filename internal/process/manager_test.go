package process

import (
	"runtime"
	"testing"
	"time"
)

func TestStartCapturesStdoutAndExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	out := make(chan OutputLine, 8)
	h, err := Start(StartOptions{
		Binary: "/bin/sh",
		Args:   []string{"-c", "echo hello; echo oops 1>&2"},
		Env:    BuildEnv(nil),
		Output: out,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var stdout, stderr []string
	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case line, ok := <-out:
			if !ok {
				break loop
			}
			if line.Stream == StreamStdout {
				stdout = append(stdout, line.Text)
			} else {
				stderr = append(stderr, line.Text)
			}
		case <-h.Done():
			// Drain any remaining buffered lines then stop.
			for {
				select {
				case line := <-out:
					if line.Stream == StreamStdout {
						stdout = append(stdout, line.Text)
					} else {
						stderr = append(stderr, line.Text)
					}
				default:
					break loop
				}
			}
		case <-timeout:
			t.Fatal("timed out waiting for process output")
		}
	}

	if err := h.Wait(); err != nil {
		t.Fatalf("process should exit cleanly: %v", err)
	}
	if len(stdout) != 1 || stdout[0] != "hello" {
		t.Fatalf("stdout = %v, want [hello]", stdout)
	}
	if len(stderr) != 1 || stderr[0] != "oops" {
		t.Fatalf("stderr = %v, want [oops]", stderr)
	}
}

func TestStartSetsNameAsArgvZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses /bin/sh")
	}
	out := make(chan OutputLine, 8)
	h, err := Start(StartOptions{
		Binary: "/bin/sh",
		Args:   []string{"-c", "echo $0"},
		Env:    BuildEnv(nil),
		Output: out,
		Name:   "api",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer h.Wait()

	select {
	case line := <-out:
		if line.Text != "api" {
			t.Fatalf("argv[0] = %q, want %q (ps/top should show the service name, not the binary path)", line.Text, "api")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for output")
	}
}

func TestBuildEnvOverridesWithoutDiscardingRest(t *testing.T) {
	env := []string{"FOO=bar", "PATH=/usr/bin"}
	merged := buildEnvFrom(env, map[string]string{"FOO": "baz", "NEW": "1"})

	got := map[string]string{}
	for _, kv := range merged {
		for i, c := range kv {
			if c == '=' {
				got[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	if got["FOO"] != "baz" {
		t.Errorf("FOO = %q, want baz", got["FOO"])
	}
	if got["PATH"] != "/usr/bin" {
		t.Errorf("PATH should be preserved, got %q", got["PATH"])
	}
	if got["NEW"] != "1" {
		t.Errorf("NEW = %q, want 1", got["NEW"])
	}
}
