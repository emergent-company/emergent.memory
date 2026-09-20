# a2a-acp-migration Specification

## Purpose

Define the migration away from the superseded in-house ACP interface: deprecation signalling on the legacy `/acp/v1/` and `/agent-chat/v1/` routes, moving first-party consumers (CLI, SDK, MCP tools) onto A2A, and the eventual removal of the ACP implementation and its persistence naming. Captures the deprecation window so legacy clients keep working until removal.

## Requirements

### Requirement: ACP deprecation signaling
While the legacy routes remain available, `/acp/v1/` and `/agent-chat/v1/` responses SHALL carry HTTP `Deprecation` and `Sunset` headers pointing clients at the A2A endpoints.

#### Scenario: Deprecated route advertises sunset
- **WHEN** a client calls any `/acp/v1/` or `/agent-chat/v1/` endpoint
- **THEN** the response includes a `Deprecation` header and a `Sunset` header

#### Scenario: Deprecation does not break existing clients
- **WHEN** an existing ACP client calls a legacy endpoint during the deprecation window
- **THEN** the response status and body are unchanged from before this change

### Requirement: CLI migration to A2A
The CLI SHALL provide a `memory a2a` command group covering discovery, message send/stream, task get/list/cancel, and the `memory acp` group SHALL be marked deprecated in its help output while continuing to function during the window.

#### Scenario: A2A command group exists
- **WHEN** a user runs `memory a2a --help`
- **THEN** discovery, message, and task subcommands are listed

#### Scenario: ACP command group is marked deprecated
- **WHEN** a user runs `memory acp --help`
- **THEN** the output indicates the command group is deprecated in favour of `memory a2a`

#### Scenario: ACP commands still work during the window
- **WHEN** a user runs `memory acp ping` during the deprecation window
- **THEN** the command succeeds as before

### Requirement: SDK migration to A2A
A first-party `pkg/sdk/a2a` client SHALL cover the A2A HTTP+JSON surface, and `pkg/sdk/acp` SHALL be marked deprecated and scheduled for removal.

#### Scenario: A2A SDK client covers core operations
- **WHEN** a caller uses `pkg/sdk/a2a`
- **THEN** it can fetch the extended card, send a message, stream a message, get a task, list tasks, and cancel a task

#### Scenario: ACP SDK is deprecated
- **WHEN** a developer reads `pkg/sdk/acp`
- **THEN** the package documentation states it is deprecated in favour of `pkg/sdk/a2a`

### Requirement: MCP tool disposition
The `acp-*` MCP tools SHALL be either repointed to A2A semantics or retired as duplicates of the existing agent listing and triggering tools, and the chosen disposition SHALL be recorded.

#### Scenario: Duplicative ACP tools are retired or renamed
- **WHEN** the MCP tool catalog is listed after this change
- **THEN** no tool named `acp-*` remains, or each remaining one documents its A2A repointing

#### Scenario: No capability regression
- **WHEN** an agent previously used an `acp-*` MCP tool to list or trigger agents
- **THEN** an equivalent supported tool remains available

### Requirement: ACP implementation removal
After the deprecation window, the ACP server implementation SHALL be deleted and the reused persistence tables MAY be renamed, with no A2A behavioural change.

#### Scenario: ACP handlers are removed
- **WHEN** the removal milestone lands
- **THEN** `acp_handler.go`, `acp_dto.go`, `acp_routes.go`, `pkg/sdk/acp`, and `apps/cli/internal/cmd/acp.go` no longer exist

#### Scenario: A2A surface is unaffected by removal
- **WHEN** the ACP implementation is deleted
- **THEN** all A2A discovery, message-flow, and HITL behaviours continue to pass their tests

#### Scenario: Reused tables remain valid
- **WHEN** the `kb.acp_sessions` and `kb.acp_run_events` tables are renamed or retained
- **THEN** task `contextId` continuity and task history reconstruction continue to function
