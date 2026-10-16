# web-ui-e2e-share Specification

## Purpose

Exercise the agent public-share web surface in CI against a hermetic in-process
mock (no Zitadel session, no live memory backend), and let the hermetic
Playwright lanes that share the `run-e2e.sh` harness run concurrently on one
runner on disjoint, fully parameterised gateway and mock ports.

## Requirements

### Requirement: Agent public-share UI has a hermetic browser CI lane

The agent public-share Playwright suite (`specs/share/agent-public-share.spec.ts`)
SHALL run in CI against the hermetic dev-mode mock harness
(`share.config.ts` → `run-e2e.sh`: stateful `mock-memory.mjs` + gateway
`AUTH_MODE=dev`), needing no Zitadel session and no live memory backend. The lane
SHALL be part of the path-filtered Web UI CI workflow and SHALL report into that
workflow's aggregate gate, so a regression in the public-share surface fails CI
rather than only surfacing locally.

#### Scenario: The share suite runs in the Web UI CI workflow

- **WHEN** a change under `apps/web-ui/**` is pushed
- **THEN** a CI job runs `npx playwright test --config=share.config.ts` against the mock harness and reports into the workflow's aggregate gate

#### Scenario: The lane is hermetic

- **WHEN** the share CI lane runs
- **THEN** it starts only `run-e2e.sh` (mock memory + gateway `AUTH_MODE=dev`) and requires no Zitadel credentials or live backend

### Requirement: Concurrent hermetic lanes use disjoint ports

The hermetic harness surfaces — `run-e2e.sh`, the four Playwright configs
(`share`, `datasources`, `dev-gallery`, `mcp-inspect`), and the mock's fabricated
data-source OAuth origins — SHALL derive their gateway and mock ports from
`MEMORY_PORT`/`MOCK_PORT`, defaulting to `8097`/`5301` when the variables are
unset. Derivation is mandatory: no surface may diverge from the environment, so a
lane started with a non-default `MEMORY_PORT`/`MOCK_PORT` both serves and asserts
against that gateway and mock.

CI jobs that can run concurrently on the shared self-hosted runner SHALL be
pinned to distinct gateway and mock ports — `datasources` on `8097`/`5301` and
`share` on `8098`/`5302` — so two live lanes never attach to each other's gateway
or stateful mock. `run-e2e.sh` SHALL fail fast, with a non-zero exit and a clear
message, when its target port is already served, rather than piggybacking on a
foreign server (`reuseExistingServer:false`).

#### Scenario: The two pinned CI lanes run concurrently on one runner

- **WHEN** the `datasources` and `share` jobs run at the same time on the shared runner
- **THEN** each boots its own gateway + mock on its pinned ports (`datasources` `8097`/`5301`, `share` `8098`/`5302`) and neither lane observes the other's state

#### Scenario: A non-default gateway port is threaded through every harness surface

- **WHEN** a lane is started with a non-default `MEMORY_PORT` (and matching `MOCK_PORT`)
- **THEN** every Playwright config targets that gateway, the mock's fabricated OAuth metadata and the specs' mock-derived expectations name that gateway/mock origin, and no surface points at the default `8097`/`5301`

#### Scenario: The target port is already served

- **WHEN** `run-e2e.sh` starts while something is already listening on its gateway or mock port
- **THEN** it exits non-zero with a message naming the port, without starting the specs against the foreign server
