# mcp-hosted-lifecycle Specification

## Purpose
Makes hosted MCP server workspaces reclaimable by policy as well as by explicit deletion: an idle server, measured by the already-maintained `last_used_at` column, can be reclaimed automatically once an operator enables the idle policy, and reclamation decisions are observable.

## Requirements

### Requirement: Persistent MCP servers SHALL be reclaimable by a configurable idle policy

The server SHALL support destroying persistent hosted MCP server workspaces whose `last_used_at` is older than a configured idle window, reusing the existing removal path (provider destroy followed by row deletion). The policy SHALL default to disabled, and while disabled no persistent MCP server SHALL be reclaimed by the policy.

#### Scenario: Disabled policy reclaims nothing
- **GIVEN** the idle reclamation policy is disabled
- **AND** a persistent MCP server has not been used for longer than any configured window
- **WHEN** the cleanup cycle runs
- **THEN** the server SHALL NOT be reclaimed

#### Scenario: Enabled policy reclaims an idle server
- **GIVEN** the idle reclamation policy is enabled with a window of N days
- **AND** a persistent MCP server's `last_used_at` is older than N days
- **WHEN** the cleanup cycle runs
- **THEN** the server's container SHALL be destroyed
- **AND** its row SHALL be deleted only after the container destroy succeeds, with a failed destroy leaving the row in place for a later retry

#### Scenario: A recently used server is kept
- **GIVEN** the idle reclamation policy is enabled with a window of N days
- **AND** a persistent MCP server's `last_used_at` is within N days
- **WHEN** the cleanup cycle runs
- **THEN** the server SHALL NOT be reclaimed

#### Scenario: A server never called since creation is judged by its creation time
- **GIVEN** the idle reclamation policy is enabled
- **AND** a persistent MCP server was created more than the window ago and has never received a call, so `last_used_at` equals its creation time
- **WHEN** the cleanup cycle runs
- **THEN** the server SHALL be eligible for reclamation

#### Scenario: In-flight lifecycle states are never reclaimed
- **GIVEN** a persistent MCP server is in a creating or stopping state
- **WHEN** the cleanup cycle runs
- **THEN** the server SHALL NOT be reclaimed

#### Scenario: Explicit deletion is unaffected by the policy
- **GIVEN** a persistent MCP server is explicitly deleted by an operator
- **WHEN** deletion runs
- **THEN** the existing destroy-and-delete behaviour SHALL apply regardless of the idle policy

#### Scenario: A failed destroy does not abort the pass
- **GIVEN** several idle persistent MCP servers are eligible
- **AND** destroying one of them fails
- **WHEN** the cleanup cycle runs
- **THEN** the remaining eligible servers SHALL still be processed
- **AND** the failure SHALL be logged

### Requirement: Idle reclamation SHALL run on the existing cleanup cycle

Idle reclamation SHALL run as part of the existing cleanup cycle, gated on agent sandboxes being enabled and on the policy being enabled. No new scheduler SHALL be required.

#### Scenario: Policy runs alongside expiry and reconciliation
- **GIVEN** sandboxes are enabled and the idle policy is enabled
- **WHEN** a cleanup cycle executes
- **THEN** the cycle SHALL evaluate idle persistent MCP servers in addition to expiring ephemeral workspaces and reconciling orphans

#### Scenario: Policy is skipped when sandboxes are disabled
- **GIVEN** agent sandboxes are disabled
- **WHEN** cleanup cycles run
- **THEN** no persistent MCP server SHALL be reclaimed

### Requirement: Reclamation decisions SHALL be observable

Each reclamation and each skip SHALL be logged with the server identifier and its idle age, each skip SHALL additionally record why it was skipped, and the pass SHALL report counts of reclaimed, skipped, and failed servers.

#### Scenario: Reclaimed server is logged with its idle age
- **GIVEN** an idle persistent MCP server is reclaimed
- **WHEN** the pass completes
- **THEN** a log entry SHALL identify the server and its idle age
- **AND** the pass summary SHALL report reclaimed, skipped, and failed counts

### Requirement: Operators SHALL be able to identify idle MCP servers before reclamation

Existing operator surfaces SHALL expose the information needed to identify an idle hosted MCP server without enabling the policy.

#### Scenario: Idle age is visible via the hosted MCP listing
- **GIVEN** a hosted MCP server has not been used recently
- **WHEN** an operator retrieves the hosted MCP server listing
- **THEN** the listing SHALL expose the server's last-used information sufficient to determine its idle age
