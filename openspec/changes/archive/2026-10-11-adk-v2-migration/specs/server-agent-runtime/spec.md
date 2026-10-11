## Purpose

The Agent Development Kit (ADK) module the server compiles against, the
behaviour-parity guarantee across the v1→v2 module move, and the e2e module's
isolation from the workspace build list.

## ADDED Requirements

### Requirement: Server agent runtime SHALL compile against ADK v2

The server SHALL import the agent runtime from the `google.golang.org/adk/v2`
module (v2.5.0 or a later compatible v2.x). No server package SHALL import the
pre-v2 `google.golang.org/adk` module path.

#### Scenario: Only the v2 module path is imported

- **WHEN** the server packages are inspected for ADK imports
- **THEN** every import resolves to `google.golang.org/adk/v2/...`
- **AND** no import resolves to the bare `google.golang.org/adk/...` path

#### Scenario: Server builds and type-checks

- **WHEN** `go build ./...` and `go vet ./...` run in `apps/server`
- **THEN** both exit 0

### Requirement: Migration SHALL preserve agent-runtime behaviour

The module move SHALL NOT change the observable behaviour of the extraction
pipeline, the agent executor, the registered tools, or session storage. Node
ordering, retry/escalation semantics, and persisted state keys SHALL remain as
before.

#### Scenario: Extraction pipeline result is unchanged

- **WHEN** the extraction unit tests run (`go test ./domain/extraction/...`)
- **THEN** they pass without changes to pipeline logic or output shape

#### Scenario: Tool and callback signatures still dispatch

- **WHEN** a function tool or a model callback is invoked
- **THEN** it receives the unified `agent.Context` and dispatches as before the
  migration

### Requirement: E2E module SHALL be isolated from the workspace build list

The `e2e` module SHALL resolve its own dependency graph (`GOWORK=off`) in CI and
local runs. It SHALL NOT share the repo workspace build list, because its test
framework (`runlog`) pins an older `google.golang.org/adk` whose telemetry is
incompatible with the `otel` version adk/v2 selects.

#### Scenario: e2e builds independently of the server workspace

- **WHEN** `GOWORK=off go build ./...` runs in `e2e`
- **THEN** it exits 0 regardless of the server's ADK major version

#### Scenario: e2e CI sets GOWORK=off

- **WHEN** the e2e workflow jobs run `go test`
- **THEN** `GOWORK=off` is set for those jobs
