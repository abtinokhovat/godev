// Package mcpserver exposes a Supervisor to AI agents over the Model
// Context Protocol: list/inspect services, start/stop/restart them,
// fetch recent logs, and start/stop Go debugging - the same
// operations a developer drives through the TUI, callable by an agent
// instead.
//
// A server built here is strictly scoped to the single Supervisor
// (and therefore single project) it's given, matching every other
// godev entry point's single-project-per-process design: nothing in
// this package is global, so multiple `godev mcp` processes for
// different projects never cross-talk (see docs/ROADMAP.md's MCP
// phase for why this matters).
package mcpserver

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/abtinokhovat/godev/internal/application"
)

// New builds an MCP server exposing sup's tools. Call Run (or the
// SDK's Server.Run directly) to serve it over a transport.
func New(sup *application.Supervisor, projectName string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "godev",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: fmt.Sprintf(
			"Controls the %q project's services through godev: list them, inspect status and logs, "+
				"start/stop/restart, and debug Go services via Delve. Every tool call here operates on "+
				"this one project only.", projectName),
	})

	t := &toolset{sup: sup}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_services",
		Description: "List every service in this project: name, group, kind (go/command), current state, PID, uptime, and any TCP ports observed listening.",
	}, t.listServices)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_service",
		Description: "Get full detail for one service by name: state, PID, uptime, listening ports, last error, build status, and debug session info if one is active.",
	}, t.getService)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_logs",
		Description: "Fetch recent log lines, most recent last, optionally filtered to one service.",
	}, t.getLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_service",
		Description: "Build (if needed) and start a service. No-op if it's already running.",
	}, t.startService)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_service",
		Description: "Gracefully stop a running service.",
	}, t.stopService)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "restart_service",
		Description: "Stop (if running) then start a service again.",
	}, t.restartService)

	mcp.AddTool(server, &mcp.Tool{
		Name: "start_debug",
		Description: "Build a debug binary and start headless Delve for a Go service, returning the host/port " +
			"an editor (VS Code, GoLand) can attach to. Only supported for Go services, not command-based ones.",
	}, t.startDebug)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stop_debug",
		Description: "Stop a running debug session for a service.",
	}, t.stopDebug)

	return server
}
