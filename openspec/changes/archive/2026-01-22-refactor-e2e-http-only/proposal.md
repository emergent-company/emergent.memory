# Change: Refactor E2E Tests to HTTP-Only Mode

## Why

The Go backend e2e tests currently support two modes: in-process (with direct database access) and external server (HTTP-only). However, 8 test files still contain direct database operations (`NewInsert`, `NewSelect`, `NewRaw`) that only work in in-process mode. This creates several problems:

1. **Testing reality gap**: Tests with direct DB access don't reflect how production clients interact with the API
2. **Environment coupling**: These tests cannot run against a deployed staging/production server
3. **Hidden dependencies**: Direct DB manipulation can mask API design issues and missing endpoints
4. **Maintenance burden**: Two different testing patterns increase cognitive load

## What Changes

- **Refactor 8 test files** to use HTTP-only calls instead of direct database operations
- **Add missing HTTP helper methods** to `testutil/httpclient.go` for creating test fixtures via API
- **Add job management API endpoints** (if missing) to support testing async job workflows
- **Update test documentation** to clarify HTTP-only testing as the standard pattern
- **Skip or isolate** tests that genuinely require internal access (e.g., testing scheduler internals)

### Files Requiring Refactoring

| Test File                        | Direct DB Operations        | Proposed Solution                            |
| -------------------------------- | --------------------------- | -------------------------------------------- |
| `chunk_embedding_jobs_test.go`   | Creates jobs directly in DB | Use job creation API or HTTP helpers         |
| `chunk_embedding_worker_test.go` | Manipulates job state       | Add job status API or skip in external mode  |
| `datasource_deadletter_test.go`  | Inserts deadletter records  | Use error simulation via API                 |
| `document_parsing_jobs_test.go`  | Creates parsing jobs        | Use document upload API (triggers jobs)      |
| `graph_embedding_jobs_test.go`   | Creates graph jobs directly | Use graph creation API                       |
| `graph_embedding_worker_test.go` | Manipulates worker state    | Add worker status API or skip                |
| `object_extraction_jobs_test.go` | Creates extraction jobs     | Use extraction API                           |
| `scheduler_test.go`              | Tests internal scheduler    | Keep as internal test, skip in external mode |

## Impact

- **Affected specs**: `testing`
- **Affected code**:
  - `apps/server-go/tests/e2e/*.go` (8 files)
  - `apps/server-go/internal/testutil/httpclient.go`
  - Potentially new API endpoints for job management
- **Risk**: Low - tests already pass, this improves their portability
- **Benefit**: Tests can run against any environment (local, staging, production)
