## ADDED Requirements

### Requirement: Go E2E Test Parity with NestJS

The Go server E2E test suite SHALL provide equivalent test coverage to the NestJS E2E test suite for all API endpoints and behavioral scenarios.

#### Scenario: Security scope enforcement tests exist

- **WHEN** reviewing Go E2E tests
- **THEN** tests exist that validate scope enforcement for all protected endpoints (kb:read, kb:write, chat:use, etc.)

#### Scenario: Chat streaming tests exist

- **WHEN** reviewing Go E2E tests
- **THEN** tests exist that validate SSE streaming for chat endpoints including success, error, and ordering scenarios

#### Scenario: RLS isolation tests exist

- **WHEN** reviewing Go E2E tests
- **THEN** tests exist that validate cross-project and cross-tenant data isolation via RLS policies

#### Scenario: Search mode variant tests exist

- **WHEN** reviewing Go E2E tests
- **THEN** tests exist that validate lexical-only, vector-only, and hybrid search modes

### Requirement: SSE Test Helper

The Go test utilities SHALL provide helpers for testing Server-Sent Events (SSE) streaming responses.

#### Scenario: Parse SSE response

- **WHEN** a test needs to validate SSE streaming output
- **THEN** `testutil.ParseSSEResponse()` parses the response body into a slice of SSE events

#### Scenario: SSE event validation

- **WHEN** validating SSE events in tests
- **THEN** each event has accessible `Event`, `Data`, and `ID` fields for assertion

### Requirement: Consolidated Test Organization

Go E2E tests SHALL be organized into domain-focused test suites rather than scenario-specific files.

#### Scenario: Chat tests consolidated

- **WHEN** looking for chat-related tests
- **THEN** all chat tests are in `chat_test.go`, `chat_streaming_test.go`, or `chat_citations_test.go`

#### Scenario: Security tests consolidated

- **WHEN** looking for security-related tests
- **THEN** all scope enforcement and authorization tests are in `security_scopes_test.go` or `tenant_isolation_test.go`

#### Scenario: Search tests consolidated

- **WHEN** looking for search-related tests
- **THEN** all search mode and variant tests are in `search_test.go` or `search_modes_test.go`

## MODIFIED Requirements

### Requirement: E2E Test Patterns

The testing infrastructure SHALL define standardized end-to-end test patterns using real database and authentication.

#### Scenario: E2E test with database

- **WHEN** running end-to-end tests
- **THEN** use `testutil.SetupTestDB()` (Go) or `createE2EContext()` (NestJS) to set up real Postgres database with RLS policies and proper cleanup

#### Scenario: E2E test with authentication

- **WHEN** running authenticated e2e tests
- **THEN** use `testutil.WithAuth()` (Go) or `authHeader()` (NestJS) helper with appropriate scopes for the test scenario

#### Scenario: E2E test cleanup

- **WHEN** e2e tests complete
- **THEN** automatically clean up test data using `testDB.Close()` (Go) or context teardown methods (NestJS)

#### Scenario: Go E2E test with SSE streaming

- **WHEN** running e2e tests for SSE streaming endpoints
- **THEN** use `testutil.ParseSSEResponse()` to parse and validate streaming responses

#### Scenario: Go E2E test with table-driven tests

- **WHEN** testing multiple similar scenarios (e.g., scope matrix)
- **THEN** use Go's table-driven test pattern with `s.Run()` for subtests
