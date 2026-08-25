package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/debugger"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
	"github.com/abtinokhovat/godev/internal/mcpserver"
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
	if err := ensureConfigured(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Printf("%d service(s) in %s:\n", len(p.Services), p.Root)
	for _, s := range p.Services {
		fmt.Printf("  %-16s %s\n", s.Name, serviceSource(s))
	}

	return runTUI(p, p.Services, filepath.Base(p.Root), nil)
}

// ensureConfigured runs the interactive `godev init` flow inline the
// first time a project has no .godev.yaml yet, so a fresh project
// still only takes one command to get going - but the scan it takes
// to get there only ever happens this once, not on every subsequent
// `godev` invocation (see loadProject). A project that's already
// configured (even with zero services, which loadProject will reject
// on its own with a clearer message) is left untouched.
func ensureConfigured() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, isGoModule, err := resolveRoot(cwd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("loading %s: %w", config.FileName, err)
	}
	if cfg != nil && len(cfg.Services) > 0 {
		return nil
	}
	fmt.Printf("No %s found in %s - let's set one up.\n\n", config.FileName, root)
	_, err = runInitFlow(root, isGoModule)
	return err
}

// cmdRun opens the TUI scoped to the given targets, e.g.
// `godev run core web api` - each target is a group name or an
// individual service name, resolved and deduplicated by resolveTargets
// so a service shared by two requested groups still only runs once.
// It never affects services outside the resolved set.
func cmdRun(targets []string) int {
	if err := ensureConfigured(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
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

	names := make([]string, len(services))
	for i, s := range services {
		names[i] = s.Name
	}
	return runTUI(p, services, filepath.Base(p.Root)+" · "+label, names)
}

func serviceSource(s domain.Service) string {
	if s.IsCommand() {
		return strings.Join(s.Command, " ")
	}
	return s.Package
}

// runTUI builds a Supervisor scoped to exactly the given services,
// starts them, and runs the TUI until the user quits or the process is
// signaled, then shuts everything down gracefully. startNames, when
// non-nil, starts exactly those services regardless of their
// auto_start setting - the caller named them explicitly (`godev run
// <target>...`), which is itself the signal to start them. nil means
// a bare invocation: only what's configured to auto-start actually
// starts.
func runTUI(p *project, services []domain.Service, label string, startNames []string) int {
	sup, err := application.NewSupervisor(p.Root, services)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if startNames != nil {
		sup.StartServices(startNames)
	} else {
		sup.StartAll()
	}

	stopWatch, err := sup.WatchAndReload(200)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: hot reload disabled:", err)
	} else {
		defer stopWatch()
	}

	m := tui.New(sup, label)
	program := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
	}

	sup.Shutdown()
	return 0
}

// cmdMCP starts every auto-start service (exactly like `godev`, bare)
// and serves them over MCP on stdio instead of opening the TUI, so an
// AI agent can list/inspect/start/stop/restart services and drive Go
// debugging the same way a developer would. It never prints anything
// to stdout itself - that stream is reserved for the MCP JSON-RPC
// framing; startup/shutdown notes go to stderr only, and log/status
// data is meant to be queried through the get_logs tool instead.
//
// Like every other godev command, this process is scoped to exactly
// the one project it was started in - nothing here is global, so
// running `godev mcp` concurrently in several different projects'
// directories is safe and produces no cross-talk between them.
func cmdMCP() int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	sup, err := newSupervisor(p)
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

	fmt.Fprintf(os.Stderr, "godev mcp: serving %q (%d service(s)) over stdio\n", filepath.Base(p.Root), len(p.Services))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := mcpserver.New(sup, filepath.Base(p.Root))
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
	}

	sup.Shutdown()
	return 0
}

// cmdVersion prints whatever build/VCS info Go's toolchain embedded
// automatically (since Go 1.18, `go build`/`go install` from a git
// checkout capture the commit without any extra ldflags) - the
// fastest way to tell "am I actually running the build with feature
// X" apart from "I'm on a stale binary that predates it".
func cmdVersion() int {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		fmt.Println("godev (build info unavailable)")
		return 0
	}
	fmt.Printf("godev %s\n", info.Main.Version)
	var revision, buildTime string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			buildTime = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision != "" {
		if len(revision) > 12 {
			revision = revision[:12]
		}
		if dirty {
			revision += "-dirty"
		}
		fmt.Printf("  commit  %s\n", revision)
	}
	if buildTime != "" {
		fmt.Printf("  built   %s\n", buildTime)
	}
	fmt.Printf("  go      %s\n", info.GoVersion)
	return 0
}

// cmdList prints every configured service, grouped exactly like the
// TUI sidebar does (see domain.PrimaryGroups): ungrouped services
// first, then each group as a header with its services indented
// beneath it, groups in first-appearance order. A service tagged into
// more than one group is filed under its smallest one and noted as
// "also in: ..." for the rest, since those extra tags are there for
// `godev run <target>...` convenience, not to split the service
// across multiple places in this listing.
func cmdList() int {
	p, err := loadProject()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	primary := domain.PrimaryGroups(p.Services)
	printService := func(s domain.Service, indent string) {
		kind := "package"
		if s.IsCommand() {
			kind = "command"
		}
		fmt.Printf("%s%s\n%s  %s: %s\n", indent, s.Name, indent, kind, serviceSource(s))
		if also := otherGroups(s, primary[s.Name]); also != "" {
			fmt.Printf("%s  also in: %s\n", indent, also)
		}
	}

	var ungrouped []domain.Service
	var groupOrder []string
	byGroup := map[string][]domain.Service{}
	for _, s := range p.Services {
		g, ok := primary[s.Name]
		if !ok {
			ungrouped = append(ungrouped, s)
			continue
		}
		if _, seen := byGroup[g]; !seen {
			groupOrder = append(groupOrder, g)
		}
		byGroup[g] = append(byGroup[g], s)
	}

	fmt.Println("SERVICES")
	for _, s := range ungrouped {
		printService(s, "")
	}
	for _, g := range groupOrder {
		fmt.Printf("\n[%s]\n", g)
		for _, s := range byGroup[g] {
			printService(s, "  ")
		}
	}
	return 0
}

// otherGroups lists a service's groups besides its primary (display)
// one - the ones it's tagged into purely for `godev run
// <target>...` convenience.
func otherGroups(s domain.Service, primary string) string {
	var extra []string
	for _, g := range s.Group {
		if g != primary {
			extra = append(extra, g)
		}
	}
	return strings.Join(extra, ", ")
}

// cmdInit is godev's only entry point for discovery: it runs `go
// list`/JetBrains import once, lets the user pick exactly which
// results become services (renaming any of them first, if the
// auto-derived name isn't the one they want), and writes the result
// into .godev.yaml with auto_start left off. Every other command only
// ever reads .godev.yaml - see loadProject - so this is also the only
// command whose runtime cost scales with project size.
func cmdInit() int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	root, isGoModule, err := resolveRoot(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if _, err := runInitFlow(root, isGoModule); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
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
