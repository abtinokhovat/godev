# Roadmap: Grouping, Run Configurations, MCP, IDE Extensions

This is a planning document for future work — nothing here is
implemented yet. It exists so the design decisions and phasing are
written down before implementation starts, rather than re-derived from
scratch later. Phases are ordered by priority, as directed by the
project owner; earlier phases are wanted sooner, not necessarily
"blocking" in a strict dependency sense unless noted.

## Why

godev today discovers and runs only Go programs: discovery shells
`go list -json`, the build step shells `go build`, the debugger shells
`dlv`. The near-term goal is **not** to make godev natively understand
every language — auto-discovery stays Go-only, and no other language
gets bespoke build/debug integration right now. Instead, the goal is
to let *any* run configuration be added explicitly — from
`.godev.yaml` directly, or imported from JetBrains' own `.run` XML
files (which already capture Go, Node/JS, npm, and shell run configs a
developer set up in their IDE) — and to organize the resulting service
list into groups. On top of that, expose godev to AI agents via MCP so
an agent can run and debug services the same way a developer would
through the TUI. IDE extensions (JetBrains, VS Code, Zed) remain the
lowest priority — useful eventually, not urgent.

## Phase 0 — Minimal data-model groundwork

Small, foundational changes that Phase 1 needs; not a large refactor.

- `domain.Service` gains `Group []string` (hierarchical path, empty =
  ungrouped) and a way to represent a service that came from manual
  config or an imported run configuration rather than `go list`
  discovery — i.e. a standalone entry needs at minimum a command,
  directory, and optional args/env, without requiring a discovered Go
  package backing it.
- `.godev.yaml` merging (`config.Merge`) currently can only override
  services discovery already found by name — it can't add new ones.
  Extend it so a `services.<name>` entry with no discovered
  counterpart is treated as a standalone service definition.
- Go's existing `go list`-based auto-discovery is unchanged and stays
  the only auto-discovery mechanism — no heuristic scanning is added
  for any other language.

## Phase 1 — Run configurations + grouping (top priority)

The two features the project owner wants first, and they're naturally
coupled (JetBrains run configs carry both the run details *and* the
grouping folder in one file), so they ship together:

- **Manual run configurations via `.godev.yaml`**: a `services.<name>`
  entry can fully define a service — command, directory, args, env,
  group — independent of anything godev auto-discovered. This is the
  general mechanism for "other project runs" of any kind (JS, Python,
  shell scripts, anything): no per-language build/debug integration is
  required to add one, just an explicit command to run.
- **JetBrains `.run` XML importer**: parses
  `.idea/runConfigurations/*.xml`, read-only (never writes back —
  avoids corrupting JetBrains' own state or fighting a second writer).
  Maps the run-configuration types actually in scope —
  `GoApplicationRunConfiguration`, Node.js/npm configurations, and
  shell-script configurations — to a service definition (working
  directory, program args, env vars) and to `Group` via the
  configuration's `folderName`. Other configuration types are ignored
  rather than guessed at.
- Merge order: `.godev.yaml` overrides everything, then the JetBrains
  import, then Go's `go list` discovery — keyed by directory (not
  name, since XML run-config names are freeform) so the same service
  doesn't get double-listed if it appears in more than one source.
- **Sidebar grouping**: the TUI's service list — currently one flat
  loop — becomes a tree render: services grouped by `Group` prefix
  under collapsible group headers, reusing the existing section
  styling. Ungrouped services keep today's flat list unchanged.
- **`godev run <group>` CLI command**: opens the TUI scoped to a
  single named group instead of every discovered/configured service —
  e.g. `godev run core` starts and displays only the services in the
  `core` group. `godev` (bare, no argument) keeps today's behavior of
  running everything. A group can mix Go and non-Go services; each
  service's own runtime still determines its behavior within that
  group — a Go service in the group still gets the full hot-reload
  pipeline (watch → rebuild → restart), while a non-Go service in the
  same group gets hot reload without a compile step (watch → restart
  directly), since only Go has a build step to run. This isn't new
  machinery: the existing hot-reload path already restarts a service
  after its build step completes, and a no-op build (Phase 0's generic
  runtime) already completes instantly — the group command just needs
  to filter which services are in play, not change how any individual
  service reloads.
- **Explicitly not in scope for this phase**: bespoke build or debug
  integration for JS/Node or any other language. A JS service added
  through `.godev.yaml` or imported from a JetBrains Node run
  configuration gets process management (start/stop/restart/hot
  reload/logs) exactly like any Go service, but no specialized
  debugger — Delve remains Go-only for now. If a later need arises for
  first-class Node (or other language) debugging, that's a separate,
  deliberately deferred phase, not part of this one.

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
talk to); benefits from Phase 1's grouping data.

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

Bundled into the phases above where the same files are already being
touched, rather than tracked as their own phase:

- `Service.Watch.Include`/`Exclude` is parsed from `.godev.yaml` but
  never actually read by the watcher (hardcoded to `.go`/`go.mod`/
  `go.sum`) — dead field, fix as part of Phase 0/1's config work.
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
