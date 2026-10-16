## ADDED Requirements

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

The hermetic lanes (`datasources`, `share`) that both boot `run-e2e.sh` SHALL run
on distinct gateway and mock ports, so that two lanes live on the same
self-hosted runner never attach to each other's gateway or stateful mock. The
harness SHALL default to `8097`/`5301` for local runs and SHALL fail fast, with a
non-zero exit and a clear message, when its target port is already served —
never silently serve specs from a foreign server (`reuseExistingServer:false`).

#### Scenario: Two lanes run concurrently on one runner

- **WHEN** the `datasources` and `share` jobs run at the same time on the shared runner
- **THEN** each boots its own gateway + mock on its own `MEMORY_PORT`/`MOCK_PORT` and neither lane observes the other's state

#### Scenario: The target port is already served

- **WHEN** `run-e2e.sh` starts while something is already listening on its gateway or mock port
- **THEN** it exits non-zero with a message naming the port, without starting the specs against the foreign server
