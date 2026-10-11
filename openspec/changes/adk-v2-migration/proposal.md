## Why

The server's agent runtime is pinned to `google.golang.org/adk v1.4.0`, which
predates ADK's graph-based workflow API. Graph workflows (typed node-to-node
data flow, explicit conditional routing, fan-out/join) ship only in the v2
module — `google.golang.org/adk/v2` (a major-version import-path change). The
extraction pipeline currently orchestrates its nodes with the template workflow
agents (`sequentialagent` + `loopagent`) and threads typed data through
stringly-typed `session.State` maps and repeated `type` switches.

Migrating the module path is the prerequisite for the planned typed-graph
rewrite of extraction. This change performs **only** the module migration
(`v1.4.0` → `v2.5.0`) with behaviour preserved; the graph rewrite ships as a
separate change on top of a clean v2 base.

## What Changes

- **Module path move.** Every server import of `google.golang.org/adk/...`
  becomes `google.golang.org/adk/v2/...`; `apps/server/go.mod` and `go.work`
  require `adk/v2 v2.5.0` and raise the Go directive to `1.26.6` (the minimum
  adk/v2 accepts).
- **Three mechanical API breakages** (all trivial, no logic change):
  - `session.NewEvent(id)` gains a leading `context.Context`:
    `session.NewEvent(ctx, id)`.
  - `tool.Context` and `agent.CallbackContext` are unified into
    `agent.Context` in v2 (functiontool handlers and model callbacks).
- **e2e isolation.** The `e2e` module is built with `GOWORK=off`. Its test
  framework (`runlog v0.3.0`) pins the ancient `google.golang.org/adk v0.6.0`,
  and adk/v2's newer `otel` bump breaks that module's telemetry. Detaching e2e
  from the workspace build list keeps its dependency graph stable — matching the
  existing `task test:e2e` behaviour, which already sets `GOWORK=off`.
- **No behavioural change** to the extraction pipeline, agent executor, tools,
  or session storage. The pipeline remains entity-extractor → entity-processor →
  relationship retry-loop.

## Capabilities

### New Capabilities

- `server-agent-runtime`: the ADK module contract the server compiles against
  (v2 import path), the behaviour-parity guarantee across the migration, and the
  e2e module's build isolation from the workspace.

### Modified Capabilities

<!-- none -->

## Impact

- `apps/server/go.mod`, `apps/server/go.sum` — `google.golang.org/adk/v2 v2.5.0`,
  Go `1.26.6`, transitive otel/genai/grpc bumps.
- `go.work`, `go.work.sum` — Go directive `1.26.6` + v2 module sums.
- All `apps/server/**` ADK importers (~60 files): `domain/agents`, `domain/chat`,
  `domain/extraction{,/agents}`, `domain/skills`, `domain/provider`,
  `domain/agentcompat`, `domain/discoveryjobs`, `pkg/adk{,/session/bunsession}`,
  `internal/testutil`, `tests/integration`.
- `.github/workflows/e2e.yml` — `GOWORK: "off"` on the e2e jobs.
