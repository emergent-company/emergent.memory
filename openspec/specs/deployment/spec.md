### Requirement: Taskfile is the sole task runner for the monorepo
All build, test, lint, migrate, and dev workflow commands SHALL be available via `task` (Taskfile.dev). No Nx or workspace-cli dependency SHALL be required to run any development command.

#### Scenario: Developer runs tests
- **WHEN** a developer runs `task server:test:e2e` from the repo root
- **THEN** the Go E2E tests SHALL execute against the running server
- **AND** no Node.js or npm invocation SHALL be required

#### Scenario: Developer starts the server
- **WHEN** a developer runs `task dev` from the repo root
- **THEN** `air` SHALL start in `apps/server-go` providing hot reload
- **AND** no PM2 or workspace-cli SHALL be involved

#### Scenario: Root Taskfile delegates to server-go
- **WHEN** a developer runs `task build` from the repo root
- **THEN** the command SHALL be equivalent to running `task build` inside `apps/server-go`
- **AND** the root Taskfile SHALL include `apps/server-go/Taskfile.yml` as a namespace

### Requirement: JS tooling is minimal in the monorepo
After the migration, the monorepo root `node_modules` SHALL contain only the minimum packages required for git hooks (husky). No Nx, workspace-cli, LangChain, or frontend packages SHALL be present at the root level.

#### Scenario: Fresh clone
- **WHEN** a developer clones the repo and runs `npm install` at root
- **THEN** only husky and its minimal dependencies SHALL be installed
- **AND** total install time SHALL be under 10 seconds

### Requirement: Observability services available as opt-in Docker Compose profile
The monorepo deployment SHALL support an `observability` Docker Compose profile that adds trace collection infrastructure without affecting default deployments. Services under this profile SHALL NOT start unless explicitly requested.

#### Scenario: Default compose does not start Tempo
- **WHEN** a developer runs `docker compose up` without `--profile observability`
- **THEN** the Tempo service SHALL NOT start
- **AND** the main server SHALL start normally without any tracing errors

#### Scenario: Observability profile starts Tempo
- **WHEN** a developer runs `docker compose --profile observability up`
- **THEN** Tempo SHALL start alongside the other services
- **AND** the server SHALL automatically send traces to Tempo when `OTEL_EXPORTER_OTLP_ENDPOINT` is set
