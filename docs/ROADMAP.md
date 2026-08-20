# Roadmap: Multi-Runtime Support, Grouping, IDE Extensions

This is a planning document for future work — nothing here is
implemented yet. It exists so the design decisions and phasing are
written down before implementation starts, rather than re-derived from
scratch later.

## Why

godev today is Go-only end to end: discovery shells `go list -json`,
the build step shells `go build`, the debugger shells `dlv`, and the
supervisor holds concrete builder/debugger types with no interfaces
anywhere. The goal is to grow godev into a general multi-runtime dev
environment manager: define and run non-Go services (JS/Node as the
first example, but more generally "other project runs"), debug them
too (not just via Delve), group services in the TUI, auto-populate
that grouping from JetBrains' own `.run` XML configs, and eventually
ship IDE extensions (JetBrains first, then VS Code, then Zed) that
talk to a running godev instance.

One framing decision shapes everything below: the primary mechanism
for adding a non-Go service is **explicit, user-authored
configuration** — either through a new "add service" menu in the TUI,
or imported from JetBrains' own explicit `.run` XML files — not
heuristic auto-discovery (no `package.json` script sniffing, no
entrypoint guessing). A manually-defined service with an arbitrary
command is really "add other project runs" of any kind; Go and Node
earn first-class (build + debug) treatment, and everything else gets
basic process management through a generic runtime.

## Also folding in: gaps from the original implementation plan

Small items, bundled into the phases below where the same files are
already being touched rather than tracked separately:

- `Service.Watch.Include`/`Exclude` is parsed from `.godev.yaml` but
  never actually read by the watcher (`isWatchedFile` is hardcoded to
  `.go`/`go.mod`/`go.sum`) — dead field. Fix while generalizing the
  watcher for multi-runtime defaults (Phase 0).
- Debounce window is hardcoded (200ms) — make it a `.godev.yaml`
  `watch.debounce_ms` setting (Phase 0).
- VS Code `launch.json` / GoLand Go-Remote config auto-generation —
  smaller and nearer-term than full IDE extensions; natural once the
  `Debugger` interface exists (Phase 0/1).
- Per-package dependency-graph-scoped rebuilds — today *any* file
  change rebuilds *every* hot-reload-enabled service project-wide.
  Natural extension of the watcher generalization work (Phase 0).
- Interactive `godev init` — currently auto-enables everything with no
  prompts. Independent, low-effort, no dependency on anything else.

## Phase 0 — Interface refactor (prerequisite for everything else)

Today: zero interfaces exist; the supervisor holds a concrete builder
and concrete debugger session type. This phase introduces:

- `domain.Service` gains `Runtime` (`"go" | "node" | "custom"`,
  defaulting to `"go"` so existing config/discovery keep working
  unchanged) and `Group []string` (hierarchical path, empty =
  ungrouped).
- A `Builder` interface (`Build(svc, mode) (Result, error)`). `Result`
  gains a `Command []string` field — how to actually exec the service
  (`[binaryPath]` for Go, `[node, server.js]` or `[npm, run, dev]` for
  Node) — so the process-management layer, which is already fully
  language-agnostic, never needs to special-case a runtime.
- A `Debugger`/`Session` interface: `Start`, `Stop`, `WaitListening`,
  `Wait`, and a generalized `ConnectInstructions()` replacing the
  Delve-specific VS Code/GoLand instruction methods. The debug session
  type's Delve-specific PID field generalizes to a plain `PID` +
  `Protocol` pair.
- A `Discoverer` interface (`Detect`/`Discover`) so `go list`
  discovery and future sources (the JetBrains importer, Phase 4) share
  one shape feeding a discovery registry.
- The supervisor resolves the right builder/debugger per service by
  its `Runtime` at entry-creation time, instead of holding one
  concrete builder for the whole project.
- Watcher generalization (folds in the dead-field fix above):
  per-service watch predicate from `Watch.Include`/`Exclude`, with
  runtime-based defaults when unset (Go: `*.go`; Node: `*.js`, `*.ts`,
  `*.jsx`, `*.tsx`, excluding `node_modules`).

## Phase 1 — Manual "add run configuration" + Node runtime

The concrete deliverable for "other project runs," built around
explicit configuration rather than auto-discovery:

- **Config schema**: `.godev.yaml` merging currently can only override
  services discovery already found by name — it can't invent new
  ones. Extend it so a `services.<name>` entry with no discovered
  counterpart is treated as a standalone service definition (needs at
  minimum a runtime, a command, and a directory).
- **TUI "add service" menu**: a new keybinding opens a form collecting
  name, runtime (go/node/custom), command, directory, args, env,
  group, and watch patterns. On submit it writes the entry into
  `.godev.yaml` (creating the file if absent) and registers it with
  the live supervisor immediately — no restart required. Lives
  alongside the existing four content views (Logs/Build/Problems/Debug)
  as a new view/mode.
- **Node runtime**: a builder that's usually a no-op (nothing to
  compile) but can optionally shell a user-specified build command
  (e.g. `tsc`) if the manual config supplies one; a debugger that
  launches the service's own process with `--inspect=host:port`
  rather than Delve's wrap-a-separate-process model.
- **`custom` runtime**: arbitrary command, optional build step,
  process management only, no specialized debugger. This is the
  general escape hatch for "other project runs" beyond Go/Node
  specifically (Python, Rust, shell scripts, anything) without
  bespoke per-language work up front.

## Phase 2 — Grouping

- `Service.Group` (added in Phase 0) is populated from, in priority
  order: an explicit `group:` field in `.godev.yaml` (highest) → a
  JetBrains `.run` XML `folderName` (Phase 4) → directory-structure
  inference as a low-priority fallback. This phase ships independently
  of the JetBrains importer — directory-based fallback and manual
  config are enough to be useful on their own.
- The TUI sidebar's service list — currently one flat loop — becomes a
  tree render: services grouped by `Group` prefix under collapsible
  group headers, reusing existing section styling. Ungrouped services
  keep today's flat list unchanged, so projects with no groups defined
  see no visual change.

## Phase 3 — Local daemon/API layer

The supervisor's event bus and log manager already have the right
shape for this (non-blocking pub/sub, drop-slow-subscriber semantics,
a complete read/write method surface) — but today there is no
daemon/server/IPC of any kind; every CLI invocation is a fresh,
standalone, in-process instance.

- A Unix-socket JSON-RPC layer (socket path derived the same way the
  build cache directory is today, per-project) for CLI-to-daemon
  control — simple, filesystem-permission auth, no port collisions
  across projects.
- A local HTTP+SSE layer on the same supervisor, bound to an ephemeral
  localhost port, specifically for IDE extensions: list services,
  start/stop/restart, start/stop debug, stream logs and events over
  SSE. IDE extension hosts have far easier native HTTP/EventSource
  clients than raw socket RPC.
- `godev` (bare) starts the daemon in-process alongside the TUI, best
  effort — if the socket can't bind, fall back to today's standalone
  behavior with a warning, never hard-fail.
- Resolves a previously-identified gap: `godev stop/restart/logs
  <service>` become real daemon-client commands instead of today's
  foreground-only workaround.

## Phase 4 — JetBrains `.run` XML importer

- A new discovery source implementing the Phase 0 `Discoverer`
  interface: parses `.idea/runConfigurations/*.xml`, mapping known
  configuration types (Go application, Node.js/npm, shell script) to
  runtime, working directory, args, env, and group (from the
  configuration's `folderName`).
- **Read-only** — never writes back into `.idea/`, to avoid corrupting
  JetBrains' own state or fighting a second writer.
- Merge order: `.godev.yaml` overrides everything, then the JetBrains
  import, then `go list` discovery. Treated as an enrichment pass
  keyed by directory (not name, since XML run-config names are
  freeform) rather than a competing discovery source that could
  double-list a service.

## Phase 5 — IDE extensions (lowest priority; JetBrains first)

Hard-blocked on Phase 3 (needs a stable daemon API to talk to);
benefits from Phase 4 (JetBrains ecosystem synergy) and Phase 2
(grouping data to display).

1. **JetBrains plugin** — tool window listing services/groups sourced
   from the daemon, start/stop/restart/debug actions, one-click attach
   using the daemon's debug-connection-info instead of a hand-built Go
   Remote config.
2. **VS Code extension** — same shape: sidebar tree view plus a debug
   configuration provider that reads connection info from the daemon
   so debugging "just attaches."
3. **Zed extension** — same shape, scoped to whatever Zed's extension
   API supports whenever this phase is actually picked up (Zed's
   extension surface is newer and more limited than VS Code's or
   IntelliJ's — a risk to revisit at that point, not a design
   commitment now).

## Explicit non-goals

- No heuristic auto-detection of JS projects (`package.json` script
  sniffing, entrypoint guessing) — deliberately replaced by manual
  configuration (Phase 1) and JetBrains import (Phase 4).
- No two-way sync writing back into `.idea/runConfigurations` — the
  importer is read-only.
- No commitment to which languages beyond Go/Node get first-class
  builder+debugger treatment — everything else rides the generic
  `custom` runtime until a specific need arises.
