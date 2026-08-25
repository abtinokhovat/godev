# godev

A development environment manager, built around Go projects but not
limited to them. Point it at a project once (`godev init`, or just run
`godev` and it'll prompt you) to pick which discovered Go packages and
JetBrains run configurations become services; from then on `godev`
only ever reads `.godev.yaml` — no scanning, no re-discovery — so it
opens instantly regardless of project size, whether that's 3 services
or 300. It runs each service as its own process, hot-reloads on source
changes, restarts crashed services with backoff, and can launch
headless Delve for VS Code or GoLand to attach to — all from a
terminal UI. Non-Go services (or anything else you want managed
alongside your Go services) come in through the same explicit
selection — never heuristic guessing — and can be grouped and run
together with `godev run <group-or-service>...`.

Nothing starts on its own by default. `godev` opens the dashboard with
every configured service sitting idle; you start what you want, either
from the TUI (`s`) or by naming it (`godev run <target>...`), which
starts it regardless of its `auto_start` setting. That's deliberate: a
service you never reviewed - an imported shell script in particular -
should never execute just because it happened to get discovered.

`godev` manages development. [Delve](https://github.com/go-delve/delve)
manages debugging (for Go services). Your IDE remains the debugger UI.

## Install

**Linux or macOS**, one line — downloads the latest release binary and puts it on your PATH ([`install.sh`](./install.sh) source):

```sh
curl -fsSL https://raw.githubusercontent.com/abtinokhovat/godev/main/install.sh | sh
```

Pin a version or choose the install location with `VERSION`/`INSTALL_DIR`:

```sh
curl -fsSL https://raw.githubusercontent.com/abtinokhovat/godev/main/install.sh | VERSION=v0.2.0 INSTALL_DIR="$HOME/bin" sh
```

**Windows**, or to install manually on any OS: grab the matching archive from the [Releases page](https://github.com/abtinokhovat/godev/releases) (built by [`.github/workflows/release.yml`](.github/workflows/release.yml) on every `vX.Y.Z` tag) and put the binary on your PATH yourself.

**From source** (any OS with Go installed):

```sh
go install github.com/abtinokhovat/godev/cmd/godev@latest
```

Delve is only required if you use the debug integration:

```sh
go install github.com/go-delve/delve/cmd/dlv@latest
```

## Usage

Run it from anywhere inside a Go project (a directory containing, or
under one containing, `go.mod`) - or a directory with no Go in it at
all, as long as `.godev.yaml`/an imported JetBrains run configuration
defines at least one service.

```sh
godev
```

The first time, with no `.godev.yaml` yet, this drops straight into an
interactive checklist of everything discovered - every `main` package
under the module (via `go list -json ./...`) and every recognized
JetBrains run configuration - so you can pick exactly which ones
become services (and rename any of them, since the config name never
has to match the directory/package it points at) before anything is
written. See [Getting started](#getting-started) below.

From then on, `godev` just reads `.godev.yaml` and opens a TUI built
around a terminal-native dashboard layout rather than an IDE: a narrow,
always-visible sidebar for control and status, and a log-dominant
content pane on the right. It never scans or rebuilds the service list
on its own again - re-run `godev init` whenever you want to add newly
discovered services, or press `ctrl+r` inside the TUI to pick up edits
already made to `.godev.yaml` by hand without restarting `godev`
itself (see below).

```
┌ my-project ──────────────────────────────────── 3 service(s) · 1 running · reload ✓ ┐
│ SERVICES               │ LOGS · all services                                        │
│ ● api    RUNNING       │ 16:42:31 [api]    GET /users 200                           │
│ ○ worker DISCOVERED    │ 16:42:31 [worker] processing job 9281                      │
│ ○ web    DISCOVERED    │ 16:42:32 [api]    GET /users/42 200                        │
│ ─────────────────────  │ ...                                                       │
│ RUNTIME                │                                                           │
│ ✓ Hot reload            │                                                          │
│ ✓ Auto restart          │                                                          │
│ ─────────────────────  │                                                           │
│ DEBUGGER                │                                                          │
│ ○ None                  │                                                          │
└─────────────────────────┴───────────────────────────────────────────────────────────┘
```

With more services than fit the terminal, the sidebar scrolls
independently (`SERVICES 12-16/57`-style header) while RUNTIME/DEBUGGER
stay pinned below it - `↑`/`↓` scrolls the window to follow the
selection automatically.

The content pane has four views, switched with `1`-`4` (or `F1`-`F4`):

1. **Logs** (default) — unified, service-prefixed, timestamped log
   stream. Press `enter` on a selected service to scope the log view to
   just that service, `a` to go back to all services.
2. **Build** — the selected service's last build output. Shown
   automatically whenever the selected service starts building, and
   automatically returns to whatever view was active once the build
   settles.
3. **Problems** — every service currently crashed or build-failed,
   with its error output. Empty when everything is healthy.
4. **Debug** — the selected service's Delve session: PID, endpoint,
   and VS Code / GoLand attach instructions, or a prompt to start one.

Keys:

```
↑↓ select      enter focus service logs   a all logs        tab expand detail
r restart      s start/stop               d start/stop debug  c clear logs
1-4 views      pgup/pgdn scroll           ctrl+r reload config q quit
```

`ctrl+r` re-reads `.godev.yaml` and reconciles it against what's
already running, without restarting anything that doesn't need it: a
new entry is added (in the sidebar, not started - what to run is still
a separate decision, same as `auto_start`), a service whose definition
actually changed gets the new config and, only if it was already
running, a restart with it; a service whose entry is byte-for-byte
unchanged - the common case for editing a *different* service - is
left completely alone. An entry removed from the file is left running
as-is; reload only ever adds or updates, never stops something out
from under you.

`tab` temporarily widens the sidebar to show extra detail (package,
uptime, ports, build status, arguments, environment) for the selected
service, without leaving the log-first layout. Ports are never
configured - godev watches the OS for what a running process is
actually listening on (`:8080`, or `:8080,:9090` for more than one)
and shows whatever it finds, a few seconds after the process starts.

Editing a `.go` file triggers a debounced rebuild-and-restart, scoped
to only the hot-reload-enabled services whose build actually depends
on that file (via `go list -json -deps`, refreshed after every
change) - editing `pkg/shared/x.go` restarts every service that
imports it, editing `cmd/api/main.go` restarts only `api`. A `go.mod`/
`go.sum` change, or a change arriving before the dependency index has
finished its first (background, non-blocking) computation, falls back
to restarting everyone, since either could affect anything.

## Getting started

```sh
godev init
```

Runs Go discovery and JetBrains import once, then an interactive
checklist:

```
godev init · select services to add to .godev.yaml

[x] api                  go       ./cmd/api
[ ] worker               go       ./cmd/worker
[ ] migrate               command  /bin/bash scripts/migrate.sh

↑↓ move   space select   a select all/none   r rename   enter confirm and write   q cancel
```

`space` toggles a service; `r` renames it in place before it's written
(fixes an auto-derived or collision-suffixed name like `core-2` once
and for all, since the config key never has to match the directory it
came from); `enter` writes the selection to `.godev.yaml` with
`auto_start: false` on every entry - what to run automatically is a
separate, deliberate decision, never a side effect of curating what
*exists*. Re-running `godev init` later only ever offers what's
genuinely new; it never touches or re-prompts for services already in
the file.

`godev` (bare) does this automatically the first time there's no
`.godev.yaml` yet, so a brand new project still only takes one command.

### Other commands

```sh
godev list                 # list configured services, with groups
godev run <target>...      # open the TUI scoped to the given groups
                            # and/or individual services, starting them
                            # regardless of their auto_start setting
godev [run <target>...] --detach
                            # run in the background instead of opening the TUI
godev attach                # reattach the TUI to a --detach'd instance
godev kill                   # stop a --detach'd instance
godev init                 # interactively discover and select services
                            # to add to .godev.yaml (see above)
godev debug <service>      # build a debug binary, start headless Delve,
                            # print VS Code / GoLand attach instructions
godev mcp                  # serve this project's services to an AI agent
                            # over MCP (stdio), for it to run/inspect/debug
godev <service> [-- args]  # run one service in the foreground, with
                            # hot reload, passing one-off arguments
godev version               # print the build's commit/version - useful
                            # for confirming you're not on a stale binary
```

### Running detached

`godev --detach` (everything) or `godev run <target>... --detach` (a
scoped subset) starts services in the background instead of opening
the TUI, and returns immediately — the services keep running after the
launching shell exits. Only one detached instance runs per project at
a time; a second `--detach` is refused with a pointer to the commands
below instead of silently starting a competing instance.

```sh
godev attach   # opens the normal TUI against the running instance —
               # live logs, and restart/stop/debug act on the real
               # processes, exactly like the foreground TUI
godev kill     # stops it; waits for it to actually exit before
               # reporting success
```

### Attaching a debugger

```sh
godev debug api
```

prints something like:

```
Debugger ready
Service:
  api
Delve:
  127.0.0.1:2345
VS Code:
  attach to 127.0.0.1:2345
GoLand:
  Go Remote
  Host: 127.0.0.1
  Port: 2345
```

In the TUI, press `d` on a selected service to start/stop its debugger
the same way; the detail view (`enter`) shows the live endpoint.

## AI agent access (MCP)

```sh
godev mcp
```

Serves this one project's services to an AI agent over the
[Model Context Protocol](https://modelcontextprotocol.io), stdio
transport — the same operations available through the TUI, callable by
an agent: `list_services`, `get_service`, `get_logs`,
`start_service`/`stop_service`/`restart_service`,
`start_debug`/`stop_debug`. Debugging over MCP works the same as
everywhere else in godev — Delve, Go services only.

Nothing here is global: a `godev mcp` process serves exactly the
project root it was started in, the same as every other godev command,
so running it concurrently in several different projects' directories
is safe and produces no cross-talk between them.

## Configuration

`.godev.yaml` is the only thing godev ever reads to build its service
list at runtime - see [Getting started](#getting-started) for how it
gets written. Copy [`.godev.example.yaml`](./.godev.example.yaml) to
`.godev.yaml` at your project root to see every field, or hand-edit
what `godev init` wrote: change `auto_start` to `true` for a service
you always want running on a bare `godev`, rename a key, adjust args/
env, add a `group`.

An entry with `path` set is a Go service (godev builds and runs it);
an entry with `command` set is a non-Go service godev execs directly,
no build step. Exactly one of the two must be set - see below.

## Other services & grouping

godev never heuristically guesses at non-Go projects (no scanning for
`package.json` scripts or bare entrypoints). Instead, any service - Go
or not - is added through `godev init`'s interactive checklist (see
[Getting started](#getting-started)), sourced from one of two places:

**A manual `.godev.yaml` entry**, with a `command` godev execs directly
(no build step) - written by hand, not through discovery:

```yaml
services:
  web:
    command: ["npm", "run", "dev"]
    directory: frontend       # relative to the project root, or absolute
    env:
      PORT: "3000"
    group: [core]
```

**An imported JetBrains run configuration** — `godev init` reads
`.idea/runConfigurations/` if it exists (read-only; nothing is ever
written back) alongside Go discovery. `GoApplicationRunConfiguration`
entries enrich a matching discovered Go service (by working directory)
with its arguments, environment, and folder as `group` before it's
even offered in the checklist - they don't add a separate item.
`NodeJSConfigurationType`, npm, and `ShConfigurationType`
("Shell Script") entries become their own selectable candidates the
same shape as a manual `command` entry. Other configuration types are
ignored. A run configuration with no name is skipped outright - it has
nothing to key a service on.

A service's `group` (a `.godev.yaml` field, or a JetBrains
configuration's folder) organizes the TUI sidebar into a tree, and
`godev run <target>...` opens the TUI scoped to whatever mix of groups
and individual service names you give it - e.g. `godev run core` if
`web` above is grouped with some Go services named `core`, or
`godev run core web api` to combine a group with extra individual
services. Each matching service runs exactly once even if more than
one requested target resolves to it (a service in two overlapping
groups, or a group plus that same service named explicitly). Groups
can mix Go and non-Go services freely: each service's own settings
decide its behavior within the group - a Go service still gets the
full hot-reload pipeline (watch → rebuild → restart), a command-based
service gets hot reload without a rebuild step (there's nothing to
build), since a command-based service's build step is an instant
no-op to begin with.

Debugging (currently Delve-only) is not available for command-based
services - `godev debug <service>` and the TUI's `d` key report a clear
error rather than silently failing.

## How it works

- **Discovery** (`internal/discovery`) and **JetBrains import**
  (`internal/discovery/jetbrains`) only ever run inside `godev init` -
  never on a normal `godev`/`godev run`/etc. invocation, which reads
  only `.godev.yaml`. `go list -json ./...`, filtered to `Name ==
  "main"` packages, no source parsing; JetBrains import is read-only
  parsing of `.idea/runConfigurations/*.xml`, mapping known
  configuration types to service fields. Neither ever writes back to
  `.idea/`; `godev init`'s interactive checklist (`cmd/godev/initmenu.go`)
  is the only thing that writes `.godev.yaml`, always with
  `auto_start: false`.
- **Build** (`internal/builder`): `go build` into a per-project cache
  under `~/.cache/godev/<project-id>/<service>/`, installed via atomic
  rename so a failed build never destroys the last good binary. Debug
  builds add `-gcflags=all=-N -l`. Command-based (non-Go) services skip
  this entirely - there's nothing to build, so `Supervisor` treats their
  build step as an instant no-op and execs their configured command
  directly.
- **Process** (`internal/process`): each service is its own OS process
  in its own process group (so stopping it also stops its children),
  started with `os.Environ()` plus service-specific overrides. A Go
  service execs through a same-named symlink to its built binary
  (`internal/builder`), and a command-based service gets its argv[0]
  overridden the same way, so `ps`/`top`/`pgrep` show the service name
  (`api`) instead of a cache path or an interpreter's own name.
- Concurrent service launches (build, when there is one, plus the
  actual process start) are capped at `GOMAXPROCS` across the whole
  Supervisor (`buildSem`), so a crash loop or a big `godev run
  <group>` correlated across many services can't fire unbounded
  concurrent builds *and* unbounded concurrent process launches on
  top of that.
- **Ports** (`internal/ports`): a background poll (every 2s, starting
  immediately after launch) checks what TCP ports each running
  process is actually listening on - `/proc` on Linux, `lsof` on
  macOS, `netstat` on Windows - and shows them in the sidebar detail
  view and over MCP. Never configured; a process can (and often does)
  report more than one.
- **Watch** (`internal/watcher`): recursive `fsnotify` on `*.go`,
  `go.mod`, `go.sum`, debounced (default 200ms) into single rebuild
  batches.
- **Debug** (`internal/debugger`): launches `dlv --headless
  --accept-multiclient` on an auto-allocated `127.0.0.1` port; VS Code
  and GoLand both speak to it directly, so godev never implements a
  debugger UI itself.
- **Supervisor** (`internal/application`): owns each service's
  lifecycle state machine, serializes build/start/stop/restart per
  service, and applies exponential backoff (1s → 30s, capped) on crash
  loops, resetting once a service stays up for 30s. `Reload` (`ctrl+r`
  in the TUI, or over the attach socket) re-reads `.godev.yaml` and
  diffs it against the live service set by value (`reflect.DeepEqual`)
  - new names are added un-started, changed-and-running services are
  restarted, everything else is untouched.
- **TUI** (`internal/tui`): a Bubble Tea front end that only calls the
  supervisor's public API — it never touches a process or Delve
  directly.
- **MCP server** (`internal/mcpserver`): the same supervisor API again,
  exposed as MCP tools over stdio via the official
  `modelcontextprotocol/go-sdk`, for `godev mcp`.

## Explicitly out of scope

godev is a process/development environment manager, not an IDE. It does
not implement a debugger UI, DAP, breakpoint/variable/stack viewers,
Docker/Kubernetes management, or remote development. VS Code and GoLand
remain the debugger UI; Delve remains the debugger.

## Roadmap

Manual and JetBrains-imported run configurations for non-Go services,
grouping and `godev run <target>...`, and an MCP server (`godev mcp`)
for AI agents are all implemented (see above). Still planned: a local
daemon/API so multiple entry points (MCP, the CLI, eventually IDE
extensions) can share one running instance per project instead of each
starting their own, and (lowest priority) IDE extensions — tracked in
[`docs/ROADMAP.md`](./docs/ROADMAP.md).
