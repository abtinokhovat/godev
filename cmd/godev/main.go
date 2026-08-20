// Command godev is a zero-configuration development environment manager
// for multi-service Go projects: it discovers main packages, runs each
// as its own process, hot-reloads on source changes, and can launch
// headless Delve for VS Code / GoLand to attach to.
package main

import (
	"fmt"
	"os"
)

const usage = `godev - zero-config Go development environment manager

Usage:
  godev                        Discover services and open the TUI
  godev list                   List discovered/configured services
  godev init                   Write a starter .godev.yaml
  godev debug <service>        Build a debug binary and start headless Delve
  godev <service> [-- args]    Run one service in the foreground with hot reload
  godev help                   Show this message

In the TUI: up/down select, enter details, r restart, s start/stop,
d toggle debugger, c clear logs, pgup/pgdn scroll logs, q quit.
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
