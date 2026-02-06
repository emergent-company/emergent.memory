## ADDED Requirements

### Requirement: HTTP-Only E2E Test Mode

The Go backend e2e test infrastructure SHALL support running all tests in HTTP-only mode against an external server, without requiring direct database access.

#### Scenario: Run tests against external server

- **WHEN** `TEST_SERVER_URL` environment variable is set
- **THEN** all tests use HTTP calls exclusively
- **AND** no direct database operations are performed

#### Scenario: Skip internal-only tests

- **WHEN** running in external server mode
- **AND** a test requires internal access (scheduler, worker state)
- **THEN** the test is skipped with `SkipIfExternalServer()` helper
- **AND** skip reason is logged

#### Scenario: Job completion via polling

- **WHEN** a test triggers an async operation (document upload, extraction)
- **THEN** the test uses `WaitForJobCompletion()` or similar helper
- **AND** polls the status API until completion or timeout

### Requirement: HTTP Test Helper Methods

The test utility package SHALL provide HTTP helper methods for common test operations that trigger backend functionality.

#### Scenario: Create test fixtures via API

- **WHEN** a test needs to create documents, projects, or other entities
- **THEN** helper methods like `CreateDocument()`, `CreateProject()` are available
- **AND** these helpers use HTTP endpoints, not direct DB access

#### Scenario: Wait for async completion

- **WHEN** a test triggers an async job
- **THEN** `WaitForJobCompletion()` helper polls until job reaches terminal state
- **AND** returns error if timeout exceeded

#### Scenario: Check entity status

- **WHEN** a test needs to verify entity state
- **THEN** helper methods like `GetDocumentStatus()`, `GetJobStatus()` are available
- **AND** these return structured response data

## MODIFIED Requirements

### Requirement: E2E Test Patterns

The testing infrastructure SHALL define standardized end-to-end test patterns using real database and authentication, with support for both in-process and external server modes.

#### Scenario: E2E test with database (in-process mode)

- **WHEN** running end-to-end tests without `TEST_SERVER_URL`
- **THEN** use `BaseSuite` helper to set up real Postgres database with test isolation

#### Scenario: E2E test with external server

- **WHEN** running end-to-end tests with `TEST_SERVER_URL` set
- **THEN** use HTTP client to interact with the external server
- **AND** tests create all fixtures via API calls

#### Scenario: E2E test with authentication

- **WHEN** running authenticated e2e tests
- **THEN** use `AuthHeader()` or `SetupAuth()` helper with appropriate scopes

#### Scenario: E2E test cleanup

- **WHEN** e2e tests complete
- **THEN** test isolation ensures no cross-test pollution
- **AND** external mode tests rely on API-created fixtures that are project-scoped
