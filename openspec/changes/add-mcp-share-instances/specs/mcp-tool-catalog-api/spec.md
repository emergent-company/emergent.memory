## Purpose

Exposes the catalog of memory tools that can be granted to an MCP share instance, so a management UI can present and validate the tools an admin selects.

## ADDED Requirements

### Requirement: List includable memory tools

The system SHALL expose `GET /api/projects/:projectId/mcp/tools`, returning the memory tools that can be included in a share instance for that project. Each entry MUST include the tool name, a human-readable description, the scope required to call it, and a category for grouping. Tools marked agent-only MUST be excluded.

#### Scenario: Catalog returned for a project admin

- **WHEN** a project admin requests the tool catalog
- **THEN** the response lists includable tools with name, description, required scope, and category

#### Scenario: Agent-only tools omitted

- **WHEN** the full tool set includes tools marked agent-only
- **THEN** the catalog omits those tools

#### Scenario: Catalog reflects project tools

- **WHEN** a project has project-enriched tools (for example agents or relay-sourced tools) available
- **THEN** the catalog includes those non-agent-only tools alongside the static ones

### Requirement: Catalog access is restricted

The tool catalog SHALL be readable only by a project admin of the requested project and MUST NOT be exposed to unauthenticated callers.

#### Scenario: Non-admin denied

- **WHEN** a user without project admin rights requests the catalog
- **THEN** the system returns HTTP 403 Forbidden

#### Scenario: Unauthenticated denied

- **WHEN** a caller without a valid credential requests the catalog
- **THEN** the system returns an unauthorized response and no catalog

### Requirement: Catalog ordering is deterministic

The catalog SHALL be returned in a stable, deterministic order (category then tool name) so clients and tests can rely on it.

#### Scenario: Stable ordering

- **WHEN** the catalog is requested twice with unchanged underlying tools
- **THEN** both responses have identical ordering

### Requirement: Catalog errors are explicit

When the project cannot be resolved or the tool source is unavailable, the endpoint SHALL return a clear error status and MUST NOT return a partial or empty catalog that looks valid.

#### Scenario: Unknown project

- **WHEN** the catalog is requested for a project the caller cannot access
- **THEN** the system returns a not-found or forbidden response, not an empty tool list
