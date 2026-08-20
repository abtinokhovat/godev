package daemon

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/domain"
)

func TestResolvePathsDeterministicAndDistinct(t *testing.T) {
	a1, err := ResolvePaths("/proj/a")
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	a2, err := ResolvePaths("/proj/a")
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if a1 != a2 {
		t.Fatalf("same root produced different paths: %+v vs %+v", a1, a2)
	}

	b, err := ResolvePaths("/proj/b")
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if a1.Socket == b.Socket || a1.PID == b.PID {
		t.Fatalf("different roots collided: a=%+v b=%+v", a1, b)
	}
}

func TestProbeNotRunning(t *testing.T) {
	status, err := Probe(t.TempDir())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if status.Running {
		t.Fatalf("expected Running=false with nothing listening, got %+v", status)
	}
}

func TestProbeCleansUpStaleFiles(t *testing.T) {
	root := t.TempDir()
	paths, err := ResolvePaths(root)
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WritePID(paths.PID, 999999); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Socket, []byte("not a socket"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := Probe(root)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if status.Running {
		t.Fatalf("expected Running=false for a stale/bogus socket, got %+v", status)
	}
	if _, err := os.Stat(paths.Socket); !os.IsNotExist(err) {
		t.Errorf("stale socket file should have been removed, stat err=%v", err)
	}
	if _, err := os.Stat(paths.PID); !os.IsNotExist(err) {
		t.Errorf("stale PID file should have been removed, stat err=%v", err)
	}
}

func TestServerClientRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}

	root := t.TempDir()
	sup, err := application.NewSupervisor(root, []domain.Service{
		{Name: "echoer", Command: []string{"/bin/sh", "-c", "echo hello; sleep 5"},
			Directory: root, AutoRestart: true},
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}

	ln, paths, err := Listen(root)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer os.Remove(paths.Socket)
	defer os.Remove(paths.PID)

	srv := NewServer(sup)
	go srv.Serve(ln)
	defer ln.Close()

	if err := sup.Start("echoer"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client, err := Dial(root, 3*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if got := client.Services(); len(got) != 1 || got[0].Name != "echoer" {
		t.Fatalf("client.Services() = %+v, want one service named echoer", got)
	}

	logCh, cancelLogs := client.SubscribeLogs(64)
	defer cancelLogs()

	waitFor(t, 3*time.Second, "client to see echoer running", func() bool {
		rt, ok := client.Runtime("echoer")
		return ok && rt.State == domain.StateRunning
	})

	before, _ := client.Runtime("echoer")
	if err := client.Restart("echoer"); err != nil {
		t.Fatalf("client.Restart: %v", err)
	}
	waitFor(t, 5*time.Second, "restart to complete with a new PID", func() bool {
		rt, ok := client.Runtime("echoer")
		return ok && rt.State == domain.StateRunning && rt.PID != 0 && rt.PID != before.PID
	})

	select {
	case <-logCh:
	case <-time.After(5 * time.Second):
		t.Fatal("no log line received via the client within 5s")
	}

	if err := client.RequestShutdown(); err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	select {
	case <-srv.ShutdownRequested():
	case <-time.After(3 * time.Second):
		t.Fatal("server never observed the shutdown request")
	}

	select {
	case <-client.Closed():
	case <-time.After(3 * time.Second):
		t.Fatal("client connection never closed after RequestShutdown")
	}

	sup.Shutdown()
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
