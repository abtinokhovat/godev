# godev

A terminal dev environment manager for multi-service Go projects.
`godev` builds and runs each service as its own process, hot-reloads
on source changes, restarts crashes with backoff, and attaches Delve
for VS Code/GoLand — all from one TUI. Non-Go services (frontend dev
servers, shell scripts, anything) run alongside Go ones the same way.

Config is explicit: `godev init` discovers Go packages and writes
`.godev.yaml`; every later `godev` run reads only that file, so
startup is instant regardless of project size and nothing runs that
you didn't select.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/abtinokhovat/godev/main/install.sh | sh
```

Pin a version or a location:

```sh
curl -fsSL .../install.sh | VERSION=v0.2.5 INSTALL_DIR="$HOME/bin" sh
```

Windows, or manual install on any OS: grab a binary from
[Releases](https://github.com/abtinokhovat/godev/releases).

From source:

```sh
go install github.com/abtinokhovat/godev/cmd/godev@latest
```

Delve, only if you use the debug integration:

```sh
go install github.com/go-delve/delve/cmd/dlv@latest
```

## Quick start

```sh
godev
```

With no `.godev.yaml`, this runs discovery and drops into a checklist
of every Go `main` package — pick what becomes a service, rename any
of them, confirm. From then on `godev` just opens the TUI against
`.godev.yaml`.

```
┌ my-project ──────────────────────────────────── 3 service(s) · 1 running ┐
│ SERVICES               │ LOGS · all services                            │
│ ● api    :8080 · PID.. │ 16:42:31 [api]    GET /users 200               │
│ ○ worker DISCOVERED    │ 16:42:31 [worker] processing job 9281          │
│ ○ web    DISCOVERED    │ 16:42:32 [api]    GET /users/42 200            │
└─────────────────────────┴───────────────────────────────────────────────┘
```

Nothing auto-starts unless a service has `auto_start: true`. Start
what you want with `s` in the TUI, or by name: `godev run api worker`.

## Commands

```sh
godev                        # open the TUI (runs init first if no config exists)
godev init                   # (re)run discovery, select services into .godev.yaml
godev list                   # list configured services and groups
godev run <target>...        # open the TUI scoped to these services/groups, and start them
godev <service> [-- args]    # run one service in the foreground, with hot reload
godev debug <service>        # start headless Delve, print VS Code/GoLand attach info
godev mcp                    # serve this project's services to an AI agent over MCP
godev version                # print build/commit info

godev --detach                     # run everything in the background
godev run <target>... --detach     # run a subset in the background
godev attach                       # open the TUI against a --detach'd instance
godev kill                         # stop a --detach'd instance
```

`<target>` is any mix of service names and group names, space
separated. Each matching service runs once even if named by more
than one target.

## TUI keys

```
↑↓ select          enter focus logs      a all logs        tab expand detail
r restart          s start/stop          d start/stop debug c clear logs
R restart group    S start/stop group    : run by name/group
1-4 switch view    pgup/pgdn/←→ scroll   ctrl+r reload config  q quit
```

- `R`/`S` act on the selected service's whole group (start if none
  running, else stop/restart all members), one at a time, not
  concurrently.
- `:` opens a prompt — type service/group names, `enter` starts them
  without navigating the sidebar first. Works with any configured
  service, not just the ones named on the command line that launched
  this session.
- Mouse: click a service to focus its logs; wheel scrolls whichever
  pane the cursor is over; click-and-drag over the log pane selects
  text and copies it to your clipboard on release, same as a normal
  terminal — dragging above or below the visible log lines keeps
  scrolling to reveal more while you hold it there. Selection only
  ever applies to the log pane, never the sidebar.
- `ctrl+r` re-reads `.godev.yaml`: adds new entries, restarts only
  services whose config actually changed, leaves everything else
  running untouched.

Four content views (`1`-`4`): **Logs** (default), **Build** (auto-shown
while the selected service builds), **Problems** (crashed/failed
services only), **Debug** (Delve endpoint + attach instructions).

## Configuration

`.godev.yaml` is the only file godev reads at runtime. See
[`.godev.example.yaml`](./.godev.example.yaml) for every field.

```yaml
services:
  api:
    path: ./cmd/api          # Go service — godev builds it
    args: ["--port", "8080"]
    env:
      LOG_LEVEL: debug
    auto_start: true
    hot_reload: true
    group: [core]

  web:
    command: ["npm", "run", "dev"]   # non-Go service — execs directly, no build
    directory: frontend
    group: [core]
```

Exactly one of `path` / `command` per service. `group` puts services
under a shared sidebar header and lets `godev run <group>` start them
together; a service in multiple groups displays under its
smallest/most-specific one but works with all of them.

Non-Go services are added by hand-editing `.godev.yaml` — there's no
discovery for them. Review a command before setting `auto_start: true`
on it, the same as any command you'd run yourself.

## Debugging

```sh
godev debug api
```

Starts headless Delve and prints the attach endpoint for VS Code or
GoLand. In the TUI, `d` does the same for the selected service. Go
services only — command-based services return a clear error instead
of silently failing.

## AI agent access (MCP)

```sh
godev mcp
```

Exposes this project's services to an AI agent over
[MCP](https://modelcontextprotocol.io) (stdio): `list_services`,
`get_service`, `get_logs`, `start_service`/`stop_service`/
`restart_service`, `start_debug`/`stop_debug`. Scoped to the project
root it's started in — safe to run in several projects at once.

## Crash recovery and log persistence

Every running service's PID is recorded to `state.json` under the
project's cache dir (`~/.cache/godev/<project-id>/`), and every log
line is persisted to a per-service file there too
(`logs/<service>.log`, rotating at 10MB). This applies whether you're
running in the foreground or via `--detach` — there's no separate
"safe mode" to opt into.

If godev crashes or is killed outright (not a clean `godev kill`),
the services it was managing keep running — they're each their own
process group, independent of godev's own lifetime. The next `godev`
run for that project reads the registry back: a service whose
recorded PID is still alive, and whose process still looks like the
one godev started (its argv[0] still matches the service name — PIDs
get reused by the OS, so liveness alone isn't proof), is adopted
instead of duplicated or abandoned. Its pre-crash log history is
restored too, read back from disk. One limitation: an adopted
service's *live* output usually doesn't resume automatically — its
original stdout/stderr were pipes to the now-dead instance, which a
new one can't reconnect to (and many processes get killed by SIGPIPE
the next time they try to write to a pipe with no reader anyway).
Restarting an adopted service (`r`, or `R` for its group) gives it a
fresh pipe and resumes live output normally.

This also means a completely normal, non-crashed `godev` restart
picks up right where the log view left off, instead of starting
blank.

## Architecture

| Package | Responsibility |
|---|---|
| `internal/discovery` | Go package discovery, `godev init` only |
| `internal/config` | reads/merges `.godev.yaml`, preserves declaration order |
| `internal/builder` | `go build` into `~/.cache/godev/<project-id>/`, atomic install |
| `internal/process` | process lifecycle, own process group, argv[0] renamed to service name, PID-based adoption |
| `internal/watcher` | debounced fsnotify on `*.go`/`go.mod`/`go.sum` |
| `internal/application` | Supervisor — state machine, backoff, hot-reload cascade, concurrency caps, crash-recovery registry + log persistence |
| `internal/ports` | polls listening TCP ports per process |
| `internal/debugger` | launches/manages headless Delve |
| `internal/daemon` | `--detach`/`attach`/`kill` client-server protocol |
| `internal/tui` | Bubble Tea front end, talks only to Supervisor's public API |
| `internal/mcpserver` | same Supervisor API exposed as MCP tools |

Builds and process starts across the whole Supervisor are capped at
`GOMAXPROCS` concurrent, so a crash loop or a big `godev run <group>`
can't spawn unbounded concurrent work.

## Out of scope

Not an IDE: no debugger UI, DAP, breakpoint/variable viewers, or
container/remote-development management. VS Code/GoLand stay the
debugger UI; Delve stays the debugger.

## Roadmap

See [`docs/ROADMAP.md`](./docs/ROADMAP.md). Next up: a shared daemon
so MCP/CLI/IDE extensions reuse one running instance per project.
