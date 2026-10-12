# server-agent-runtime Specification

## Purpose
The Agent Development Kit (ADK) module the server compiles against, the
behaviour-parity guarantee across the v1→v2 module move, and the e2e module's
isolation from the workspace build list.

## Requirements

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

The `e2e` module SHALL resolve its own dependency graph. It SHALL NOT share the
repo workspace build list, because its test framework (`runlog`) pins an older
`google.golang.org/adk` whose telemetry is incompatible with the `otel` version
adk/v2 selects. Isolation SHALL be structural:

- the repo-root `go.work` SHALL NOT list the `e2e` module or the stub modules
  beneath it;
- `e2e/go.work` SHALL claim the `e2e` module and every module beneath it, so a
  bare `go build ./...` / `go test ./...` inside `e2e/` resolves that workspace;
- CI SHALL run the e2e modules with `GOWORK=off`;
- the `go.work` coverage guard SHALL treat a module as covered when the root
  `go.work` lists it, or when the allowlisted isolated root's own `go.work`
  claims it, and SHALL fail when neither does.

#### Scenario: e2e builds independently of the server workspace

- **WHEN** `go build ./...` runs in `e2e` with no `GOWORK` override
- **THEN** it exits 0 regardless of the server's ADK major version
- **AND** `go env GOWORK` resolves to `e2e/go.work`, not the repo-root `go.work`

#### Scenario: e2e CI sets GOWORK=off

- **WHEN** the e2e workflow jobs run `go test`
- **THEN** `GOWORK=off` is set for those jobs

#### Scenario: Coverage guard rejects an unauthorised isolated workspace

- **WHEN** a module is dropped from the root `go.work` and relies on a nested
  `go.work` that is outside the guard's allowlisted isolated roots
- **THEN** the coverage guard fails

#### Scenario: Coverage guard still rejects drift inside an isolated root

- **WHEN** a module under an allowlisted isolated root is not claimed by that
  root's `go.work`
- **THEN** the coverage guard fails
