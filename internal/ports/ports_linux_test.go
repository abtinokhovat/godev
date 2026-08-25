//go:build linux

package ports

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// TestForPIDFindsRealListeningPorts spawns a real child process (not
// this test binary) that opens two TCP listeners and prints their
// ports, then verifies ForPID discovers exactly those two ports -
// end-to-end through the real fd-to-inode-to-/proc/net/tcp path, not
// a mock.
func TestForPIDFindsRealListeningPorts(t *testing.T) {
	script := `
package main

import (
	"fmt"
	"net"
	"time"
)

func main() {
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Println(l1.Addr().(*net.TCPAddr).Port)
	fmt.Println(l2.Addr().(*net.TCPAddr).Port)
	time.Sleep(5 * time.Second)
}
`
	dir := t.TempDir()
	srcPath := dir + "/main.go"
	if err := os.WriteFile(srcPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/go.mod", []byte("module listener\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binPath := dir + "/listener"
	build := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building test listener: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting listener: %v", err)
	}
	defer cmd.Process.Kill()

	sc := bufio.NewScanner(stdout)
	if !sc.Scan() {
		t.Fatalf("reading first port: %v", sc.Err())
	}
	p1, err := strconv.Atoi(sc.Text())
	if err != nil {
		t.Fatalf("parsing first port %q: %v", sc.Text(), err)
	}
	if !sc.Scan() {
		t.Fatalf("reading second port: %v", sc.Err())
	}
	p2, err := strconv.Atoi(sc.Text())
	if err != nil {
		t.Fatalf("parsing second port %q: %v", sc.Text(), err)
	}

	var got []int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err = ForPID(cmd.Process.Pid)
		if err != nil {
			t.Fatalf("ForPID: %v", err)
		}
		if len(got) == 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if len(got) != 2 {
		t.Fatalf("ForPID(%d) = %v, want exactly 2 ports (%d, %d)", cmd.Process.Pid, got, p1, p2)
	}
	want := map[int]bool{p1: true, p2: true}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected port %d in %v, want only %v", p, got, want)
		}
	}
}

// TestForPIDUnknownPIDIsNotAnError covers the common case of a PID
// that no longer exists (the process already exited) - this must
// degrade to "no ports", not an error a caller has to specially
// handle.
func TestForPIDUnknownPIDIsNotAnError(t *testing.T) {
	got, err := ForPID(999999)
	if err != nil {
		t.Fatalf("ForPID for a nonexistent pid should not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want no ports for a nonexistent pid", got)
	}
}

func TestParseHexPort(t *testing.T) {
	port, ok := parseHexPort("0100007F:1F90")
	if !ok || port != 8080 {
		t.Fatalf("parseHexPort = %d, %v, want 8080, true", port, ok)
	}
	if _, ok := parseHexPort("garbage"); ok {
		t.Fatalf("expected ok=false for a malformed field")
	}
}
