## ADDED Requirements

### Requirement: Embedding Policies CRUD

The system SHALL provide CRUD endpoints for managing embedding policies at `/api/v2/graph/embedding-policies`.

#### Scenario: List embedding policies

- **WHEN** user requests GET /api/v2/graph/embedding-policies with valid auth and project ID
- **THEN** system returns array of embedding policies for the project

#### Scenario: Create embedding policy

- **WHEN** user sends POST /api/v2/graph/embedding-policies with valid policy definition
- **THEN** system creates the policy and returns it with generated ID

#### Scenario: Update embedding policy

- **WHEN** user sends PATCH /api/v2/graph/embedding-policies/:id with updates
- **THEN** system updates the policy and returns the updated version

#### Scenario: Delete embedding policy

- **WHEN** user sends DELETE /api/v2/graph/embedding-policies/:id
- **THEN** system soft-deletes the policy and returns 204 No Content

### Requirement: Branch CRUD

The system SHALL provide CRUD endpoints for managing graph branches at `/api/v2/graph/branches`.

#### Scenario: List branches

- **WHEN** user requests GET /api/v2/graph/branches with valid auth and project ID
- **THEN** system returns array of branches for the project

#### Scenario: Create branch

- **WHEN** user sends POST /api/v2/graph/branches with name and optional base branch
- **THEN** system creates the branch and returns it with generated ID

#### Scenario: Get branch by ID

- **WHEN** user requests GET /api/v2/graph/branches/:id
- **THEN** system returns the branch details

#### Scenario: Delete branch

- **WHEN** user sends DELETE /api/v2/graph/branches/:id
- **THEN** system deletes the branch and returns 204 No Content

### Requirement: Search Debug Mode

The system SHALL support a `debug=true` query parameter on search endpoints that returns timing and match statistics.

#### Scenario: Debug mode returns timing metrics

- **WHEN** user requests search with ?debug=true
- **THEN** response includes debug object with fts_time_ms, vector_time_ms, fusion_time_ms, total_time_ms

#### Scenario: Debug mode returns match counts

- **WHEN** user requests search with ?debug=true
- **THEN** response includes debug object with fts_matches and vector_matches counts

#### Scenario: Debug mode is optional

- **WHEN** user requests search without debug parameter
- **THEN** response does not include debug object (backward compatible)
