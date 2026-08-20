// Command godev is a zero-configuration development environment manager
// for multi-service Go projects: it discovers main packages, runs each
// as its own process, hot-reloads on source changes, and can launch
// headless Delve for VS Code / GoLand to attach to.
package main

import (
	"fmt"
	"os"
)

const usage = `godev - zero-config development environment manager

Usage:
  godev                        Discover services and open the TUI
  godev run <target>...        Open the TUI scoped to the given groups
                                and/or individual services
  godev list                   List discovered/configured services
  godev init                   Write a starter .godev.yaml
  godev debug <service>        Build a debug binary and start headless Delve
  godev <service> [-- args]    Run one service in the foreground with hot reload
  godev help                   Show this message

Services come from Go's own "go list" discovery, from an imported
JetBrains .run configuration (.idea/runConfigurations), or from a
standalone entry in .godev.yaml with an explicit "command" - the last
two work for any language, not just Go. Group a service by setting
"group" in .godev.yaml or importing it from a JetBrains run
configuration folder.

"godev run" accepts any mix of group names and individual service
names, e.g. "godev run core web api" - each matching service is
started exactly once even if it belongs to more than one requested
group.

In the TUI: up/down select, enter focus a service's logs, tab expand
detail, r restart, s start/stop, d toggle debugger, c clear logs,
1-4 (or F1-F4) switch views, pgup/pgdn scroll, q quit.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return cmdRoot()
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	case "list":
		return cmdList()
	case "init":
		return cmdInit()
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: godev run <group-or-service> [<group-or-service>...]")
			return 1
		}
		return cmdRun(args[1:])
	case "debug":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: godev debug <service>")
			return 1
		}
		return cmdDebug(args[1])
	default:
		// godev <service> [-- args...]
		name := args[0]
		var extra []string
		for i, a := range args[1:] {
			if a == "--" {
				extra = args[i+2:]
				break
			}
		}
		return cmdRunOne(name, extra)
	}
}
