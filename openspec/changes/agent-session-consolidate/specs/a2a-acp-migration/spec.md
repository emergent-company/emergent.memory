## MODIFIED Requirements

### Requirement: ACP implementation removal

The ACP server implementation SHALL have been deleted, with no A2A behavioural change. The reused persistence tables SHALL be renamed to their consolidated names (`kb.sessions`, `kb.run_events`) without changing A2A behaviour.

#### Scenario: ACP handlers are removed

- **WHEN** the removal milestone lands
- **THEN** `acp_handler.go`, `acp_dto.go`, `acp_routes.go`, `pkg/sdk/acp`, and `apps/cli/internal/cmd/acp.go` no longer exist

#### Scenario: A2A surface is unaffected by removal

- **WHEN** the ACP implementation is deleted
- **THEN** all A2A discovery, message-flow, and HITL behaviours continue to pass their tests

#### Scenario: Reused tables remain valid

- **WHEN** `kb.acp_sessions` and `kb.acp_run_events` are renamed to `kb.sessions` and `kb.run_events`
- **THEN** task `contextId` continuity and task history reconstruction continue to function
