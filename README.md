# godev

A zero-configuration development environment manager for multi-service Go
projects. Point it at a project and it discovers every runnable (`main`)
package, runs each as its own process, hot-reloads on source changes,
restarts crashed services with backoff, and can launch headless Delve for
VS Code or GoLand to attach to — all from a terminal UI.

`godev` manages development. [Delve](https://github.com/go-delve/delve)
manages debugging. Your IDE remains the debugger UI.

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
./...`), starts each as an independent OS process, and opens a TUI:

```
↑↓ select   enter details   r restart   s start/stop   d debug
c clear logs   pgup/pgdn scroll   q quit
```

Editing a `.go` file anywhere in the project triggers a debounced
rebuild-and-restart of every hot-reload-enabled service.

### Other commands

```sh
godev list                 # list discovered/configured services
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

Configuration is entirely optional — discovery alone produces a working
service list for conventional `cmd/<name>` layouts. To customize
arguments, environment variables, or auto-start/restart/reload behavior
per service, copy [`.godev.example.yaml`](./.godev.example.yaml) to
`.godev.yaml` at your project root (or run `godev init`), or run:

```sh
godev init
```

Config overrides only affect services discovery already found — it
can't invent new ones — and merges on top of discovery in this order:
discovery → defaults → `.godev.yaml` → one-off CLI args.

## How it works

- **Discovery** (`internal/discovery`): `go list -json ./...`, filtered
  to `Name == "main"` packages. No source parsing.
- **Build** (`internal/builder`): `go build` into a per-project cache
  under `~/.cache/godev/<project-id>/<service>/`, installed via atomic
  rename so a failed build never destroys the last good binary. Debug
  builds add `-gcflags=all=-N -l`.
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
