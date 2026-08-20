package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/debugger"
	"github.com/abtinokhovat/godev/internal/discovery"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/tui"
)

func waitForSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}

// printLogsToStdout streams a supervisor's log manager to stdout/stderr,
// prefixed per service, for foreground (non-TUI) commands.
func printLogsToStdout(sup *application.Supervisor) func() {
	ch, cancel := sup.Logs().Subscribe(256)
	go func() {
		for e := range ch {
			out := os.Stdout
			if e.Stream == logs.StreamStderr {
				out = os.Stderr
			}
			fmt.Fprintf(out, "[%s] %s\n", e.Service, e.Message)
		}
	}()
	return cancel
}

func cmdRoot() int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("Discovered %d service(s) in %s:\n", len(p.Services), p.Root)
	for _, s := range p.Services {
		fmt.Printf("  %-16s %s\n", s.Name, serviceSource(s))
	}

	return runTUI(p, p.Services, filepath.Base(p.Root))
}

// cmdRun opens the TUI scoped to the given targets, e.g.
// `godev run core web api` - each target is a group name or an
// individual service name, resolved and deduplicated by resolveTargets
// so a service shared by two requested groups still only runs once.
// It never affects services outside the resolved set.
func cmdRun(targets []string) int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	services, err := resolveTargets(p.Services, targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v (try `godev list`)\n", err)
		return 1
	}

	label := strings.Join(targets, ", ")
	fmt.Printf("Running %s: %d service(s)\n", label, len(services))
	for _, s := range services {
		fmt.Printf("  %-16s %s\n", s.Name, serviceSource(s))
	}

	return runTUI(p, services, filepath.Base(p.Root)+" · "+label)
}

func serviceSource(s domain.Service) string {
	if s.IsCommand() {
		return strings.Join(s.Command, " ")
	}
	return s.Package
}

// runTUI builds a Supervisor scoped to exactly the given services,
// starts them, and runs the TUI until the user quits or the process is
// signaled, then shuts everything down gracefully.
func runTUI(p *project, services []domain.Service, label string) int {
	sup, err := application.NewSupervisor(p.Root, services)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	sup.StartAll()

	stopWatch, err := sup.WatchAndReload(200)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: hot reload disabled:", err)
	} else {
		defer stopWatch()
	}

	m := tui.New(sup, label)
	program := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
	}

	sup.Shutdown()
	return 0
}

func cmdList() int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	fmt.Println("SERVICES")
	for _, s := range p.Services {
		kind := "package"
		if s.IsCommand() {
			kind = "command"
		}
		fmt.Printf("%s\n  %s: %s\n", s.Name, kind, serviceSource(s))
		if len(s.Group) > 0 {
			fmt.Printf("  group:   %s\n", strings.Join(s.Group, "/"))
		}
	}
	return 0
}

func cmdInit() int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	root, err := discovery.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: not a Go project: no go.mod found")
		return 1
	}

	cfgPath := filepath.Join(root, config.FileName)
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(os.Stderr, "error: %s already exists\n", cfgPath)
		return 1
	}

	apps, err := discovery.Discover(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if len(apps) == 0 {
		fmt.Fprintln(os.Stderr, "error: no main packages found")
		return 1
	}

	fmt.Printf("Discovered %d Go application(s):\n", len(apps))
	f := config.File{Services: map[string]config.ServiceConfig{}}
	trueVal := true
	for _, a := range apps {
		fmt.Printf("  ✓ %s\n    %s\n", a.Name, a.Package)
		f.Services[a.Name] = config.ServiceConfig{
			Path:        a.Package,
			AutoStart:   &trueVal,
			AutoRestart: &trueVal,
			HotReload:   &trueVal,
		}
	}

	if err := writeConfig(cfgPath, f); err != nil {
		fmt.Fprintln(os.Stderr, "error writing config:", err)
		return 1
	}
	fmt.Printf("\nWrote %s\n", cfgPath)
	return 0
}

func cmdDebug(name string) int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	svc, ok := findService(p, name)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown service %q\n", name)
		return 1
	}
	if svc.IsCommand() {
		fmt.Fprintf(os.Stderr, "error: debugging isn't supported for command-based services (%q runs %q, not a Go build)\n",
			name, strings.Join(svc.Command, " "))
		return 1
	}
	if err := debugger.CheckInstalled(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	sup, err := newSupervisor(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	cancelLogs := printLogsToStdout(sup)
	defer cancelLogs()

	fmt.Println("Building debug binary...")
	if err := sup.StartDebug(name); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	rt, _ := sup.Runtime(name)
	if rt.Debug == nil {
		fmt.Fprintln(os.Stderr, "error: debugger did not start")
		return 1
	}
	fmt.Println("✓")
	fmt.Println("\nDebugger ready")
	fmt.Printf("Service:\n  %s\n", name)
	fmt.Printf("Delve:\n  %s:%d\n", rt.Debug.Host, rt.Debug.Port)
	fmt.Printf("VS Code:\n  attach to %s:%d\n", rt.Debug.Host, rt.Debug.Port)
	fmt.Printf("GoLand:\n  Go Remote\n  Host: %s\n  Port: %d\n", rt.Debug.Host, rt.Debug.Port)
	fmt.Println("\nPress Ctrl+C to stop.")

	waitForSignal()
	sup.StopDebug(name)
	return 0
}

func cmdRunOne(name string, extraArgs []string) int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	svc, ok := findService(p, name)
	if !ok {
		fmt.Fprintf(os.Stderr, "error: unknown service %q (try `godev list`)\n", name)
		return 1
	}
	if len(extraArgs) > 0 {
		svc.Args = extraArgs // one-off args override config, per section 17
	}
	svc.AutoStart = true

	sup, err := application.NewSupervisor(p.Root, []domain.Service{svc})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	cancelLogs := printLogsToStdout(sup)
	defer cancelLogs()

	if err := sup.Start(name); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	stopWatch, err := sup.WatchAndReload(200)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: hot reload disabled:", err)
	} else {
		defer stopWatch()
	}

	waitForSignal()
	sup.Shutdown()
	return 0
}
