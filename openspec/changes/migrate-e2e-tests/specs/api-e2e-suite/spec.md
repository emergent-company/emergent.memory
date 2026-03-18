## ADDED Requirements

### Requirement: API e2e package exists in e2e repo
The `emergent.memory.e2e` repository SHALL contain a `tests/api/` package that provides comprehensive HTTP API end-to-end tests for all server domains.

#### Scenario: Package structure
- **WHEN** a developer clones `emergent.memory.e2e`
- **THEN** `tests/api/` exists with `testmain_test.go`, `helpers_test.go`, and one `*_test.go` file per server domain

#### Scenario: TestMain loads environment
- **WHEN** tests run
- **THEN** `TestMain` calls `framework.LoadDotEnv()` before `m.Run()` so `.env` files are loaded

---

### Requirement: Tests use RunLog for structured logging
Every test function in `tests/api/` SHALL create a `RunLog` at the start and defer its close.

#### Scenario: RunLog lifecycle
- **WHEN** a test function starts
- **THEN** it calls `rl := newRunLog(t)` and `defer rl.Close()` as the first two statements

#### Scenario: Sections logged
- **WHEN** a test performs a meaningful step (create resource, call API, verify response)
- **THEN** it calls `rl.Section("step name")` before that step

---

### Requirement: Tests run against an external server only
All tests in `tests/api/` SHALL use `framework.ServerURL()` and `framework.E2ETestToken()` to target the configured external server. No in-process server or direct DB access is permitted.

#### Scenario: Server URL from environment
- **WHEN** `MEMORY_TEST_SERVER` is set
- **THEN** `framework.ServerURL()` returns that value and all HTTP calls target it

#### Scenario: Tests skip when server is down
- **WHEN** the server is unreachable
- **THEN** `framework.SkipIfServerDown(t, rl)` skips the test with a clear message

---

### Requirement: Fixture isolation via API create and cleanup
Each test SHALL create its own org and project via HTTP API and delete them in `t.Cleanup()`. No shared global state between tests.

#### Scenario: Ephemeral project per test
- **WHEN** a test starts
- **THEN** it creates a project named `e2e-api-<domain>-<unix-ms>` via `POST /api/projects`

#### Scenario: Cleanup on test exit
- **WHEN** a test registers `t.Cleanup(func() { deleteOrg(t, orgID) })`
- **THEN** the org (and its projects) are deleted via `DELETE /api/orgs/:id` after the test, regardless of pass/fail

---

### Requirement: HTTP calls use net/http with auth helpers
Tests SHALL make HTTP calls using `net/http` with the helpers from `helpers_test.go`. No testify suites, no BaseSuite, no in-process httptest.

#### Scenario: Authenticated API call
- **WHEN** a test calls `doJSON(t, "POST", serverURL()+"/api/graph/objects", token, projectID, body)`
- **THEN** the request is sent with `Authorization: Bearer <token>` and `X-Project-ID: <projectID>` headers

#### Scenario: Response assertion
- **WHEN** the API returns a response
- **THEN** the test asserts `resp.StatusCode` and unmarshals the JSON body for further assertions

---

### Requirement: Scope-based auth tests use pre-registered test tokens
Tests that verify authorization MUST use the pre-registered standalone test tokens: `"e2e-test-user"` (all scopes), `"no-scope"`, `"read-only"`, `"graph-read"`, `"with-scope"`.

#### Scenario: Unauthorized request returns 401 or 403
- **WHEN** a request is made with `"no-scope"` token to a protected endpoint
- **THEN** the response status is 401 or 403

#### Scenario: Scoped request succeeds
- **WHEN** a request is made with `"e2e-test-user"` token
- **THEN** the response status is 2xx

---

### Requirement: One test file per server domain
The `tests/api/` package SHALL have one `*_test.go` file per migrated domain, named to match the domain (e.g., `graph_test.go`, `documents_test.go`).

#### Scenario: Domain file exists
- **WHEN** a server domain has been migrated
- **THEN** `tests/api/<domain>_test.go` exists in the e2e repo

#### Scenario: Original file removed from main repo
- **WHEN** a domain's e2e tests pass in CI in the e2e repo
- **THEN** the corresponding `apps/server/tests/e2e/<domain>_test.go` is deleted from the main repo

---

### Requirement: CI job runs tests/api/ in e2e repo
The `emergent.memory.e2e` repository CI SHALL include a job that runs `go test ./tests/api/...` against a live server.

#### Scenario: CI job triggers on push
- **WHEN** a commit is pushed to main or a PR targets main
- **THEN** the `api` CI job runs

#### Scenario: CI job uses Docker Compose server
- **WHEN** the CI job runs
- **THEN** it starts the Memory server via Docker Compose (same as existing CLI tests) before running `tests/api/`

---

### Requirement: testutil removed from main repo after full migration
After all 6 migration phases complete, `apps/server/tests/e2e/` and `apps/server/internal/testutil/` SHALL be deleted from `emergent.memory` and the `test-e2e` CI job removed.

#### Scenario: Clean main repo
- **WHEN** all phases are complete and all tests are green in the e2e repo
- **THEN** `apps/server/tests/e2e/` does not exist in `emergent.memory`
- **THEN** `apps/server/internal/testutil/` does not exist in `emergent.memory`
- **THEN** the `test-e2e` job is absent from `server.yml`
