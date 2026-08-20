package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/debugger"
	"github.com/abtinokhovat/godev/internal/domain"
	"github.com/abtinokhovat/godev/internal/logs"
)

type toolset struct {
	sup *application.Supervisor
}

// ServiceSummary is list_services' per-service entry.
type ServiceSummary struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"` // "go" or "command"
	Group []string `json:"group,omitempty"`
	State string   `json:"state"`
	PID   int      `json:"pid,omitempty"`
	// Uptime is empty unless the service is currently running.
	Uptime string `json:"uptime,omitempty"`
}

func summarize(svc domain.Service, rt domain.ServiceRuntime) ServiceSummary {
	kind := "go"
	if svc.IsCommand() {
		kind = "command"
	}
	s := ServiceSummary{
		Name:  svc.Name,
		Kind:  kind,
		Group: svc.Group,
		State: rt.State.String(),
		PID:   rt.PID,
	}
	if rt.State == domain.StateRunning && !rt.StartedAt.IsZero() {
		s.Uptime = time.Since(rt.StartedAt).Round(time.Second).String()
	}
	return s
}

func findService(sup *application.Supervisor, name string) (domain.Service, bool) {
	for _, s := range sup.Services() {
		if s.Name == name {
			return s, true
		}
	}
	return domain.Service{}, false
}

func (t *toolset) listServices(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, []ServiceSummary, error) {
	services := t.sup.Services()
	out := make([]ServiceSummary, 0, len(services))
	for _, svc := range services {
		rt, _ := t.sup.Runtime(svc.Name)
		out = append(out, summarize(svc, rt))
	}
	return nil, out, nil
}

type serviceNameInput struct {
	Name string `json:"name" jsonschema:"the service's name, as returned by list_services"`
}

// DebugInfo mirrors a service's active debug session, if any.
type DebugInfo struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// ServiceDetail is get_service's single-service response.
type ServiceDetail struct {
	ServiceSummary
	Directory   string     `json:"directory"`
	Command     string     `json:"command,omitempty"`
	Package     string     `json:"package,omitempty"`
	Args        []string   `json:"args,omitempty"`
	AutoRestart bool       `json:"auto_restart"`
	HotReload   bool       `json:"hot_reload"`
	LastError   string     `json:"last_error,omitempty"`
	BuildOK     *bool      `json:"build_ok,omitempty"`
	Debug       *DebugInfo `json:"debug,omitempty"`
}

func (t *toolset) getService(_ context.Context, _ *mcp.CallToolRequest, in serviceNameInput) (*mcp.CallToolResult, ServiceDetail, error) {
	svc, ok := findService(t.sup, in.Name)
	if !ok {
		return nil, ServiceDetail{}, fmt.Errorf("unknown service %q", in.Name)
	}
	rt, _ := t.sup.Runtime(in.Name)

	detail := ServiceDetail{
		ServiceSummary: summarize(svc, rt),
		Directory:      svc.Directory,
		Package:        svc.Package,
		Args:           svc.Args,
		AutoRestart:    svc.AutoRestart,
		HotReload:      svc.HotReload,
		LastError:      rt.LastError,
	}
	if svc.IsCommand() {
		detail.Command = strings.Join(svc.Command, " ")
	}
	if bi, ok := t.sup.BuildInfo(in.Name); ok && bi.Attempted {
		success := bi.Success
		detail.BuildOK = &success
	}
	if rt.Debug != nil {
		detail.Debug = &DebugInfo{
			Host: rt.Debug.Host, Port: rt.Debug.Port,
			State: rt.Debug.State.String(), Error: rt.Debug.Error,
		}
	}
	return nil, detail, nil
}

type getLogsInput struct {
	Name  string `json:"name,omitempty" jsonschema:"optional service name to filter to; omit for every service"`
	Limit int    `json:"limit,omitempty" jsonschema:"max number of lines to return, most recent first; defaults to 100"`
}

// LogLine is one get_logs entry.
type LogLine struct {
	Time    string `json:"time"`
	Service string `json:"service"`
	Stream  string `json:"stream"` // "stdout", "stderr", or "system"
	Message string `json:"message"`
}

func streamName(s logs.Stream) string {
	switch s {
	case logs.StreamStdout:
		return "stdout"
	case logs.StreamStderr:
		return "stderr"
	default:
		return "system"
	}
}

func (t *toolset) getLogs(_ context.Context, _ *mcp.CallToolRequest, in getLogsInput) (*mcp.CallToolResult, []LogLine, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}

	events := t.sup.Logs().Snapshot(in.Name)
	if len(events) > limit {
		events = events[len(events)-limit:]
	}

	out := make([]LogLine, 0, len(events))
	for _, e := range events {
		out = append(out, LogLine{
			Time:    e.Time.Format(time.RFC3339),
			Service: e.Service,
			Stream:  streamName(e.Stream),
			Message: e.Message,
		})
	}
	return nil, out, nil
}

// ActionResult is the response shape for the start/stop/restart-style
// tools: enough for an agent to confirm the outcome without a
// follow-up get_service call, though it may still want one for detail.
type ActionResult struct {
	Service string `json:"service"`
	State   string `json:"state"`
	Message string `json:"message"`
}

func (t *toolset) result(name, message string) ActionResult {
	rt, _ := t.sup.Runtime(name)
	return ActionResult{Service: name, State: rt.State.String(), Message: message}
}

func (t *toolset) startService(_ context.Context, _ *mcp.CallToolRequest, in serviceNameInput) (*mcp.CallToolResult, ActionResult, error) {
	if _, ok := findService(t.sup, in.Name); !ok {
		return nil, ActionResult{}, fmt.Errorf("unknown service %q", in.Name)
	}
	if err := t.sup.Start(in.Name); err != nil {
		return nil, ActionResult{}, fmt.Errorf("starting %q: %w", in.Name, err)
	}
	return nil, t.result(in.Name, "started"), nil
}

func (t *toolset) stopService(_ context.Context, _ *mcp.CallToolRequest, in serviceNameInput) (*mcp.CallToolResult, ActionResult, error) {
	if _, ok := findService(t.sup, in.Name); !ok {
		return nil, ActionResult{}, fmt.Errorf("unknown service %q", in.Name)
	}
	if err := t.sup.Stop(in.Name); err != nil {
		return nil, ActionResult{}, fmt.Errorf("stopping %q: %w", in.Name, err)
	}
	return nil, t.result(in.Name, "stopped"), nil
}

func (t *toolset) restartService(_ context.Context, _ *mcp.CallToolRequest, in serviceNameInput) (*mcp.CallToolResult, ActionResult, error) {
	if _, ok := findService(t.sup, in.Name); !ok {
		return nil, ActionResult{}, fmt.Errorf("unknown service %q", in.Name)
	}
	if err := t.sup.Restart(in.Name); err != nil {
		return nil, ActionResult{}, fmt.Errorf("restarting %q: %w", in.Name, err)
	}
	return nil, t.result(in.Name, "restarted"), nil
}

func (t *toolset) startDebug(_ context.Context, _ *mcp.CallToolRequest, in serviceNameInput) (*mcp.CallToolResult, DebugInfo, error) {
	svc, ok := findService(t.sup, in.Name)
	if !ok {
		return nil, DebugInfo{}, fmt.Errorf("unknown service %q", in.Name)
	}
	if svc.IsCommand() {
		return nil, DebugInfo{}, fmt.Errorf(
			"debugging isn't supported for command-based services (%q runs %q, not a Go build)",
			in.Name, strings.Join(svc.Command, " "))
	}
	if err := debugger.CheckInstalled(); err != nil {
		return nil, DebugInfo{}, err
	}
	if err := t.sup.StartDebug(in.Name); err != nil {
		return nil, DebugInfo{}, fmt.Errorf("starting debug for %q: %w", in.Name, err)
	}

	rt, _ := t.sup.Runtime(in.Name)
	if rt.Debug == nil {
		return nil, DebugInfo{}, fmt.Errorf("debugger did not start for %q", in.Name)
	}
	return nil, DebugInfo{Host: rt.Debug.Host, Port: rt.Debug.Port, State: rt.Debug.State.String()}, nil
}

func (t *toolset) stopDebug(_ context.Context, _ *mcp.CallToolRequest, in serviceNameInput) (*mcp.CallToolResult, ActionResult, error) {
	if _, ok := findService(t.sup, in.Name); !ok {
		return nil, ActionResult{}, fmt.Errorf("unknown service %q", in.Name)
	}
	if err := t.sup.StopDebug(in.Name); err != nil {
		return nil, ActionResult{}, fmt.Errorf("stopping debug for %q: %w", in.Name, err)
	}
	return nil, t.result(in.Name, "debugger stopped"), nil
}
