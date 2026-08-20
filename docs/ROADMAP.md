# Roadmap: Grouping, Run Configurations, MCP, IDE Extensions

Phases are ordered by priority, as directed by the project owner.
Phases 0-1 are implemented; the rest are still planning documents for
future work.

## Why

godev discovers and runs Go programs via `go list`, `go build`, and
`dlv`. Rather than trying to make godev natively understand every
language, non-Go services (or anything else you want managed alongside
your Go services) are added through **explicit configuration** - a
`.godev.yaml` entry or an imported JetBrains run configuration - not
heuristic scanning. Services can be grouped and run together. On top of
that, the plan is to expose godev to AI agents via MCP, and eventually
ship IDE extensions (JetBrains first, then VS Code, then Zed) - the
lowest priority, useful eventually, not urgent.

## Phase 0/1 — Run configurations + grouping (implemented)

- **`domain.Service.Command []string`**: a service either has a `Package`
  (Go, compiled by `internal/builder`) or a `Command` (execed directly,
  no build step - `Service.IsCommand()`). `internal/application`'s
  `build()` treats a command-based service's build as an instant
  synthetic success and runs `Command` (plus `Args`) directly; debugging
  such a service is explicitly rejected with a clear error (Delve
  remains Go-only).
- **`.godev.yaml` standalone entries**: `config.Merge` now does two
  things - overrides fields on a discovered service by name (as
  before), and, for any config entry with a `command` that *doesn't*
  match a discovered service, builds a brand new standalone service
  from it (`internal/config/config.go`'s `newStandaloneService`).
- **JetBrains `.run` XML importer** (`internal/discovery/jetbrains`):
  read-only parsing of `.idea/runConfigurations/*.xml`. Recognizes
  `GoApplicationRunConfiguration` (enriches a matching discovered Go
  service, keyed by working directory, with args/env/group - doesn't
  create a new service), `NodeJSConfigurationType`, `js.build_tools.npm`,
  and `ShConfigurationType` (each becomes a standalone command-based
  service). Other types are ignored. Wired into `cmd/godev/setup.go`'s
  `applyJetBrainsImport`, between discovery and `.godev.yaml` merging.
- **`Service.Group []string`**: set via `.godev.yaml`'s `group:` field
  or a JetBrains configuration's `folderName`. The TUI sidebar
  (`internal/tui/sidebar.go`, `model.go`) renders ungrouped services
  first (unchanged for projects with no groups), then each group under
  a header, in first-seen order - not sorted, not collapsible (that's
  a possible later refinement, not required for grouping to be useful).
- **`godev run <target>...`**: opens the TUI scoped to any mix of group
  names and individual service names (`cmd/godev/commands.go`'s
  `cmdRun`, resolving via `resolveTargets` in `setup.go`). Each target
  is matched as a group first, then as an exact service name; a
  service reachable through more than one requested target (its own
  name plus a group it's in, or two overlapping groups) still only
  runs once - `resolveTargets` dedupes by service name as it walks the
  target list. An unmatched target fails the whole call (listing every
  target that didn't match) rather than silently starting a smaller
  set than asked for. `godev` (bare) is unchanged. A group can mix Go
  and command-based services freely - hot reload still means "watch →
  rebuild → restart" for a Go service and "watch → restart" for a
  command-based one, since the latter's build step is already a no-op.

**Deviations from the original sketch**, made deliberately while
implementing rather than over-building ahead of need:
- No formal `Discoverer` interface. `go list` discovery and the
  JetBrains importer are separate, purpose-built functions called
  directly from `setup.go` - there's no current need for polymorphism
  with only two sources, and introducing one prematurely would be
  speculative abstraction. Worth reconsidering if a third source shows
  up (a real Discoverer interface would also be the natural seam for
  it).
- Group headers aren't collapsible - always-expanded sections. Simpler,
  and grouping is useful without it; collapsibility is a nice-to-have,
  not blocking.
- The `Watch.Include`/`Exclude` dead-field bug (parsed from
  `.godev.yaml` but never read by the watcher) is **not** fixed by this
  phase - it was originally scoped in alongside multi-runtime watcher
  defaults, but hot reload for command-based services turned out not to
  need it: the project-wide watcher only ever restarts services in
  response to `.go`/`go.mod`/`go.sum` changes, so a command-based
  service only reloads today if something else in the project (a Go
  file) changes while it has `hot_reload: true` set - imprecise, but
  consistent with the existing (documented) "no per-service dependency
  graph" limitation for Go services, not a new gap. Still tracked below.

## Phase 2 — MCP server for AI agents

Expose godev to AI agents (Claude and others) as a set of MCP tools:
list services (and their status/logs), start/stop/restart a service,
start/stop debugging a service, fetch recent logs, get a running
debug session's connection info. This lets an agent drive the same
operations a developer does through the TUI — including debugging a
Go service via the existing Delve integration — without needing to
shell out to ad hoc commands.

Ship this as a `godev mcp` subcommand (stdio transport, the standard
MCP transport for local tools) wrapping an in-process
`application.Supervisor` the same way the TUI does today — no daemon
required for a first version, since MCP already provides the
client/server framing. The one limitation this leaves: an MCP-driven
agent and a developer's own `godev` TUI can't operate on the same
project's services at once if MCP spins up its own separate
`Supervisor` instance each time. Phase 3 below removes that limitation
once it exists; it is not a blocker for shipping Phase 2 first.

**Design constraint carried over from `godev run`**: a `godev mcp`
process is scoped to one project root, exactly like every other godev
invocation today (`loadProject` always resolves from the current
working directory) — nothing in the codebase is global. The same AI
agent, or several different agents, must be able to run `godev mcp`
concurrently against different projects without any cross-talk, the
same way `godev run <target>...` in one project's directory never
touches another project's services. Concretely: an MCP tool call
should never take a "project" argument that could be pointed anywhere
on disk — a given `godev mcp` process serves exactly the project root
it was started in (mirroring `godev run`/`godev`), so isolation is
structural (one OS process per project) rather than something the tool
layer has to enforce by checking arguments. This also means the
project-scoped cache-dir hashing already used for the build cache
(`builder.New`) is the right pattern to reuse for anything Phase 2/3
needs to key per-project (a future daemon socket path, in particular).

## Phase 3 — Local daemon/API layer

Generalizes Phase 2's plumbing so MCP (and, later, IDE extensions) can
attach to an *already-running* `godev` instance instead of each
spinning up a competing one, and resolves a longstanding CLI gap along
the way. The supervisor's event bus and log manager already have the
right shape for this (non-blocking pub/sub, drop-slow-subscriber
semantics, a complete read/write method surface) — today there's
simply no daemon/server/IPC of any kind; every CLI invocation is a
fresh, standalone, in-process instance.

- A Unix-socket JSON-RPC layer (socket path derived the same way the
  build cache directory is today, per-project) for CLI-to-daemon and
  MCP-to-daemon control.
- `godev` (bare) starts the daemon in-process alongside the TUI, best
  effort — if the socket can't bind, fall back to today's standalone
  behavior with a warning, never hard-fail.
- `godev mcp` prefers attaching to an existing project daemon if one
  is running, falling back to Phase 2's embedded-`Supervisor` mode
  otherwise.
- Resolves a previously-identified gap: `godev stop/restart/logs
  <service>` become real daemon-client commands instead of today's
  foreground-only workaround.
- A local HTTP+SSE layer on the same daemon, for IDE extensions
  (Phase 4) specifically — easier for extension hosts to consume than
  raw socket RPC.

## Phase 4 — IDE extensions (lowest priority)

JetBrains first, then VS Code, then Zed — order confirmed by the
project owner. Hard-blocked on Phase 3 (needs a stable daemon API to
talk to); benefits from Phase 0/1's grouping data.

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

## Also folding in: smaller gaps from the original implementation plan

Still open, bundled here rather than tracked as their own phase:

- `Service.Watch.Include`/`Exclude` dead-field wiring (see Phase 0/1's
  deviations note above for why this wasn't needed yet, and what it
  would take: a per-service watch predicate instead of the current
  hardcoded project-wide `.go`/`go.mod`/`go.sum` filter).
- Debounce window is hardcoded (200ms) — make it a `.godev.yaml`
  `watch.debounce_ms` setting.
- Per-package dependency-graph-scoped rebuilds — today *any* file
  change rebuilds *every* hot-reload-enabled service project-wide.
- Interactive `godev init` — currently auto-enables everything with no
  prompts. Independent, low-effort, no dependency on anything above.

## Explicit non-goals

- No heuristic auto-detection of JS (or any other non-Go) projects —
  `package.json` script sniffing, entrypoint guessing, etc. Explicit
  configuration (manual or JetBrains-imported) is the only mechanism.
- No two-way sync writing back into `.idea/runConfigurations` — the
  importer is read-only.
- No bespoke build or debugger integration for any language besides Go
  right now. Other languages get process management through a generic
  run configuration; first-class debugging for them is a deliberately
  separate, deferred decision, not bundled into this roadmap.
