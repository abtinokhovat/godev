# godev

A zero-configuration development environment manager, built around Go
projects but not limited to them. Point it at a project and it
discovers every runnable Go `main` package, runs each as its own
process, hot-reloads on source changes, restarts crashed services with
backoff, and can launch headless Delve for VS Code or GoLand to attach
to — all from a terminal UI. Non-Go services (or anything else you want
managed alongside your Go services) come in through explicit
configuration — a `.godev.yaml` entry or an imported JetBrains run
configuration — rather than heuristic guessing, and can be grouped and
run together with `godev run <group>`.

`godev` manages development. [Delve](https://github.com/go-delve/delve)
manages debugging (for Go services). Your IDE remains the debugger UI.

## Install

```sh
go install github.com/abtinokhovat/godev/cmd/godev@latest
```

Delve is only required if you use the debug integration:

```sh
go install github.com/go-delve/delve/cmd/dlv@latest
```

## Usage

Run it from anywhere inside a Go project (a directory containing, or
under one containing, `go.mod`):

```sh
godev
```

This discovers every `main` package under the module (via `go list -json
./...`), starts each as an independent OS process, and opens a TUI built
around a terminal-native dashboard layout rather than an IDE: a narrow,
always-visible sidebar for control and status, and a log-dominant
content pane on the right.

```
┌ my-project ─────────────────────────────────── 2 service(s) · 2 running · reload ✓ ┐
│ SERVICES              │ LOGS · all services                                        │
│ ● api    RUNNING      │ 16:42:31 [api]    GET /users 200                           │
│ ● worker RUNNING      │ 16:42:31 [worker] processing job 9281                      │
│ ────────────────────  │ 16:42:32 [api]    GET /users/42 200                        │
│ RUNTIME                │ ...                                                       │
│ ✓ Hot reload           │                                                           │
│ ✓ Auto restart         │                                                           │
│ ────────────────────  │                                                           │
│ DEBUGGER               │                                                           │
│ ○ None                 │                                                           │
└────────────────────────┴───────────────────────────────────────────────────────────┘
```

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
1-4 views      pgup/pgdn scroll           q quit
```

`tab` temporarily widens the sidebar to show extra detail (package,
uptime, build status, arguments, environment) for the selected service,
without leaving the log-first layout.

Editing a `.go` file anywhere in the project triggers a debounced
rebuild-and-restart of every hot-reload-enabled service.

### Other commands

```sh
godev list                 # list discovered/configured services, with groups
godev run <group>          # open the TUI scoped to one named group
godev init                 # write a starter .godev.yaml
godev debug <service>      # build a debug binary, start headless Delve,
                            # print VS Code / GoLand attach instructions
godev <service> [-- args]  # run one service in the foreground, with
                            # hot reload, passing one-off arguments
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

## Configuration

Configuration is entirely optional for Go services — discovery alone
produces a working service list for conventional `cmd/<name>` layouts.
To customize arguments, environment variables, or auto-start/restart/
reload behavior per service, copy
[`.godev.example.yaml`](./.godev.example.yaml) to `.godev.yaml` at your
project root (or run `godev init`).

A `.godev.yaml` entry either overrides fields on a service discovery
already found (matched by name), or, if `command` is set, defines a
brand new **standalone service** — see below.

## Other services & grouping

godev never heuristically guesses at non-Go projects (no scanning for
`package.json` scripts or bare entrypoints). Instead, any service - Go
or not - is added through one of two explicit sources:

**A manual `.godev.yaml` entry**, with a `command` godev execs directly
(no build step):

```yaml
services:
  web:
    command: ["npm", "run", "dev"]
    directory: frontend       # relative to the project root, or absolute
    env:
      PORT: "3000"
    group: [core]
```

**An imported JetBrains run configuration** — if `.idea/runConfigurations/`
exists, godev reads it (read-only; nothing is ever written back) on
every load. `GoApplicationRunConfiguration` entries enrich a matching
discovered Go service (by working directory) with its arguments,
environment, and folder as `group` - they don't create a new service.
`NodeJSConfigurationType`, npm, and `ShConfigurationType`
("Shell Script") entries become standalone services the same way a
manual `command` entry does. Other configuration types are ignored.

A service's `group` (a `.godev.yaml` field, or a JetBrains
configuration's folder) organizes the TUI sidebar into a tree, and
`godev run <group>` opens the TUI scoped to just that group's services
- e.g. `godev run core` if `web` above is grouped with some Go services
named `core`. Groups can mix Go and non-Go services freely: each
service's own settings decide its behavior within the group - a Go
service still gets the full hot-reload pipeline (watch → rebuild →
restart), a command-based service gets hot reload without a rebuild
step (there's nothing to build), since a command-based service's build
step is an instant no-op to begin with.

Debugging (currently Delve-only) is not available for command-based
services - `godev debug <service>` and the TUI's `d` key report a clear
error rather than silently failing.

Merge order (later wins): discovery (`go list`) → JetBrains import →
`.godev.yaml` → one-off CLI args (for `godev <service> -- args`).

## How it works

- **Discovery** (`internal/discovery`): `go list -json ./...`, filtered
  to `Name == "main"` packages. No source parsing.
- **JetBrains import** (`internal/discovery/jetbrains`): read-only
  parsing of `.idea/runConfigurations/*.xml`, mapping known
  configuration types to service fields and enriching or adding to the
  discovered list. Never writes back to `.idea/`.
- **Build** (`internal/builder`): `go build` into a per-project cache
  under `~/.cache/godev/<project-id>/<service>/`, installed via atomic
  rename so a failed build never destroys the last good binary. Debug
  builds add `-gcflags=all=-N -l`. Command-based (non-Go) services skip
  this entirely - there's nothing to build, so `Supervisor` treats their
  build step as an instant no-op and execs their configured command
  directly.
- **Process** (`internal/process`): each service is its own OS process
  in its own process group (so stopping it also stops its children),
  started with `os.Environ()` plus service-specific overrides.
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
  loops, resetting once a service stays up for 30s.
- **TUI** (`internal/tui`): a Bubble Tea front end that only calls the
  supervisor's public API — it never touches a process or Delve
  directly.

## Explicitly out of scope

godev is a process/development environment manager, not an IDE. It does
not implement a debugger UI, DAP, breakpoint/variable/stack viewers,
Docker/Kubernetes management, or remote development. VS Code and GoLand
remain the debugger UI; Delve remains the debugger.

## Roadmap

Manual and JetBrains-imported run configurations for non-Go services,
plus grouping and `godev run <group>`, are implemented (see above).
Still planned: an MCP server so AI agents can run and debug services
through godev, a local daemon/API, and (lowest priority) IDE
extensions — tracked in [`docs/ROADMAP.md`](./docs/ROADMAP.md).
