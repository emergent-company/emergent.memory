## Why

The agent currently interacts with the project's scripts and processes manually, which can be inefficient and error-prone. By creating a set of standardized agent skills based on the existing `Taskfile.yml` and `scripts/` directory, we can streamline operations like running tests, managing migrations, debugging, and analyzing traces, making development and maintenance more efficient.

## What Changes

- Propose standard agent skills mapping to project commands.
- Define skills for test execution (unit, integration, e2e).
- Define skills for database migration management.
- Define skills for observability (listing and fetching traces).
- Define skills for executing common debugging scripts.

## Capabilities

### New Capabilities
- `agent-test-runner`: Run project tests (unit, e2e, integration) using defined Taskfile commands.
- `agent-db-migrations`: Manage database migrations (up, down, status) using Taskfile commands.
- `agent-trace-viewer`: List and retrieve OpenTelemetry traces using `emergent traces`.
- `agent-script-runner`: Execute ad-hoc debug and maintenance scripts in the `scripts/` directory.

### Modified Capabilities

## Impact

This will improve the agent's efficiency and autonomy when working in the workspace, standardizing how it runs tests, manages the database, and investigates issues via traces and scripts. No production code is modified; only agent capabilities are expanded.
