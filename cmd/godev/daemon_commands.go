package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/daemon"
	"github.com/abtinokhovat/godev/internal/tui"
)

const attachDialTimeout = 5 * time.Second

// cmdRunDetached validates targets, refuses to start a second instance
// if one is already running for this project, then re-execs itself as
// a fully detached background process and returns immediately - it
// never opens a TUI or blocks on anything itself.
func cmdRunDetached(targets []string) int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	services, label, err := resolveRunServices(p, targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v (try `godev list`)\n", err)
		return 1
	}
	if len(services) == 0 {
		fmt.Fprintln(os.Stderr, "error: no services to run")
		return 1
	}

	status, err := daemon.Probe(p.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if status.Running {
		fmt.Fprintf(os.Stderr, "error: a detached instance is already running for this project (PID %d) - use `godev attach` or `godev kill`\n", status.PID)
		return 1
	}

	pid, err := spawnDetached(p.Root, targets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error starting detached instance:", err)
		return 1
	}

	fmt.Printf("Started %s detached (PID %d).\n", label, pid)
	fmt.Println("  godev attach   view logs and control it")
	fmt.Println("  godev kill     stop it")
	return 0
}

// cmdDaemonRun is the hidden entry point spawnDetached re-execs into
// (never invoked directly by a user): it's the actual background
// instance - it builds the Supervisor, starts services and hot reload
// exactly like the foreground path, then serves them over the
// project's control socket until `godev kill` or a signal asks it to
// stop, instead of opening a TUI.
func cmdDaemonRun(targets []string) int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	services, _, err := resolveRunServices(p, targets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	sup, err := application.NewSupervisor(p.Root, services)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	sup.StartAll()

	stopWatch, err := sup.WatchAndReload(200)
	if err == nil {
		defer stopWatch()
	}

	ln, paths, err := daemon.Listen(p.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: starting control socket:", err)
		sup.Shutdown()
		return 1
	}
	defer os.Remove(paths.Socket)

	if err := daemon.WritePID(paths.PID, os.Getpid()); err != nil {
		fmt.Fprintln(os.Stderr, "warning: writing PID file:", err)
	}
	defer os.Remove(paths.PID)

	srv := daemon.NewServer(sup)
	go srv.Serve(ln)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-srv.ShutdownRequested():
	}

	ln.Close()
	sup.Shutdown()
	return 0
}

// cmdAttach connects to this project's detached instance and opens the
// same TUI the foreground path uses, driven by a live-synced replica
// of the instance's state instead of an in-process Supervisor.
func cmdAttach() int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	remote, err := daemon.Dial(p.Root, attachDialTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer remote.Close()

	m := tui.New(remote, filepath.Base(p.Root)+" (attached)")
	program := tea.NewProgram(m, tea.WithAltScreen())

	// If the daemon connection drops for any reason (killed elsewhere,
	// crashed), quit the attached TUI instead of leaving it sitting on
	// a silently-dead connection.
	go func() {
		<-remote.Closed()
		program.Quit()
	}()

	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
	}
	return 0
}

// cmdKill asks this project's detached instance to shut down (over the
// same control socket `godev attach` uses) and waits for it to
// actually exit before reporting success.
func cmdKill() int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	status, err := daemon.Probe(p.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if !status.Running {
		fmt.Println("No detached instance is running for this project.")
		return 0
	}

	remote, err := daemon.Dial(p.Root, attachDialTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := remote.RequestShutdown(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		remote.Close()
		return 1
	}

	select {
	case <-remote.Closed():
		fmt.Println("Stopped.")
	case <-time.After(10 * time.Second):
		fmt.Fprintln(os.Stderr, "warning: instance did not confirm shutdown within 10s")
		return 1
	}
	return 0
}
