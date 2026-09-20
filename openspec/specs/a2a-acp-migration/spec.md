# a2a-acp-migration Specification

## Purpose

Define the migration away from the superseded in-house ACP interface: deprecation signalling on the legacy `/acp/v1/` and `/agent-chat/v1/` routes, moving first-party consumers (CLI, SDK, MCP tools) onto A2A, and the eventual removal of the ACP implementation and its persistence naming. Captures the deprecation window so legacy clients keep working until removal.

## Requirements

### Requirement: CLI migration to A2A
The CLI SHALL provide a `memory a2a` command group covering discovery, message send/stream, task get/list/cancel. The `memory acp` command group SHALL have been removed.

#### Scenario: A2A command group exists
- **WHEN** a user runs `memory a2a --help`
- **THEN** discovery, message, and task subcommands are listed

#### Scenario: ACP command group is marked deprecated
- **WHEN** a user runs `memory acp --help`
- **THEN** the command is not recognized, because the `memory acp` group has been removed in favour of `memory a2a`

#### Scenario: ACP commands still work during the window
- **WHEN** a user runs `memory acp ping`
- **THEN** the command is not recognized, because the deprecation window has closed and the group has been removed

### Requirement: SDK migration to A2A
A first-party `pkg/sdk/a2a` client SHALL cover the A2A HTTP+JSON surface, and the `pkg/sdk/acp` package SHALL have been removed.

#### Scenario: A2A SDK client covers core operations
- **WHEN** a caller uses `pkg/sdk/a2a`
- **THEN** it can fetch the extended card, send a message, stream a message, get a task, list tasks, and cancel a task

#### Scenario: ACP SDK is deprecated
- **WHEN** a developer looks for `pkg/sdk/acp`
- **THEN** the package no longer exists, having been removed in favour of `pkg/sdk/a2a`

### Requirement: MCP tool disposition
The `acp-*` MCP tools SHALL have been removed as duplicates of the existing agent listing and triggering tools.

#### Scenario: Duplicative ACP tools are retired or renamed
- **WHEN** the MCP tool catalog is listed after this change
- **THEN** no tool named `acp-*` remains, and the equivalent `agent-list-available`, `trigger_agent`, and A2A HTTP surface are available

#### Scenario: No capability regression
- **WHEN** an agent previously used an `acp-*` MCP tool to list or trigger agents
- **THEN** an equivalent supported tool remains available

### Requirement: ACP implementation removal
The ACP server implementation SHALL have been deleted, with no A2A behavioural change. The reused persistence tables (`kb.acp_sessions`, `kb.acp_run_events`) SHALL be retained with their current names until a separate naming-cleanup change renames them.

#### Scenario: ACP handlers are removed
- **WHEN** the removal milestone lands
- **THEN** `acp_handler.go`, `acp_dto.go`, `acp_routes.go`, `pkg/sdk/acp`, and `apps/cli/internal/cmd/acp.go` no longer exist

#### Scenario: A2A surface is unaffected by removal
- **WHEN** the ACP implementation is deleted
- **THEN** all A2A discovery, message-flow, and HITL behaviours continue to pass their tests

#### Scenario: Reused tables remain valid
- **WHEN** the `kb.acp_sessions` and `kb.acp_run_events` tables are retained (renaming deferred)
- **THEN** task `contextId` continuity and task history reconstruction continue to function
