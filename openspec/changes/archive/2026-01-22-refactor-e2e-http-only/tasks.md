# Tasks: Refactor E2E Tests to HTTP-Only Mode

## Summary

**Final Approach:** Instead of adding skip logic, we restructured tests into proper categories:

- `tests/e2e/` - HTTP API tests (23 files) - can run against external server
- `tests/integration/` - Service + DB tests (8 files) - always run in-process

## 1. Preparation

- [x] 1.1 Run all e2e tests to establish baseline (`nx run server-go:test-e2e`)
- [x] 1.2 Identify all direct DB operations in test files using grep for `NewInsert`, `NewSelect`, `NewRaw`, `s.DB()`
- [x] 1.3 Document which API endpoints exist for each operation that needs refactoring

## 2. Add Missing HTTP Helper Methods

- [x] 2.1 Review `testutil/httpclient.go` for existing helper methods
- [x] 2.2 Add `CreateJob()` helper for creating async jobs via API (Not needed - jobs created via document/extraction APIs)
- [x] 2.3 Add `GetJobStatus()` helper for checking job completion (Not needed - direct service tests moved to integration)
- [x] 2.4 Add `WaitForJobCompletion()` helper with polling/timeout (Not needed - direct service tests moved to integration)
- [x] 2.5 Add `CreateDeadletterEntry()` helper if API endpoint exists (Not needed - direct service tests moved to integration)
- [x] 2.6 Write unit tests for new helper methods (Not needed - no new helpers required)

## 3. Restructure Tests into Proper Categories

- [x] 3.1 Create `tests/integration/` directory for service + DB tests
- [x] 3.2 Move 8 internal service tests from `tests/e2e/` to `tests/integration/`
- [x] 3.3 Update package declarations from `package e2e` to `package integration`
- [x] 3.4 Add `test-integration` target to `project.json`
- [x] 3.5 Remove `SkipInExternalMode()` calls (no longer needed after move)

## 4. Files Moved to tests/integration/

| File                             | Service Tested                           | Status    |
| -------------------------------- | ---------------------------------------- | --------- |
| `scheduler_test.go`              | `scheduler.Scheduler`                    | [x] Moved |
| `datasource_deadletter_test.go`  | `datasource.JobsService`                 | [x] Moved |
| `document_parsing_jobs_test.go`  | `extraction.DocumentParsingJobsService`  | [x] Moved |
| `chunk_embedding_jobs_test.go`   | `extraction.ChunkEmbeddingJobsService`   | [x] Moved |
| `chunk_embedding_worker_test.go` | `extraction.ChunkEmbeddingWorker`        | [x] Moved |
| `graph_embedding_jobs_test.go`   | `extraction.GraphEmbeddingJobsService`   | [x] Moved |
| `graph_embedding_worker_test.go` | `extraction.GraphEmbeddingWorker`        | [x] Moved |
| `object_extraction_jobs_test.go` | `extraction.ObjectExtractionJobsService` | [x] Moved |

## 5. Validation

- [x] 5.1 Run integration tests (`nx run server-go:test-integration`) - All 8 suites pass (50.1s)
- [x] 5.2 Run e2e tests (`nx run server-go:test-e2e`) - 23 HTTP API test files remain
- [x] 5.3 Verify test structure is correct:
  - `tests/e2e/` - 23 files (HTTP API tests)
  - `tests/integration/` - 8 files (Service + DB tests)

## 6. Documentation

- [x] 6.1 Update `apps/server-go/AGENT.md` with test structure guidance
- [x] 6.2 Update `docs/testing/AI_AGENT_GUIDE.md` if needed

## 7. Cleanup

- [x] 7.1 Remove SkipInExternalMode calls from moved files
- [x] 7.2 Run linter (`go vet ./...` - golangci-lint not installed, minor warnings in unrelated cmd files)
- [x] 7.3 Final test run to confirm all passing (E2E: 55s PASS, Integration: 46s PASS)

## Test Commands

```bash
# Run all tests
nx run server-go:test

# Run HTTP API e2e tests only (can run against external server)
nx run server-go:test-e2e
TEST_SERVER_URL=http://localhost:3002 nx run server-go:test-e2e

# Run service integration tests only (always in-process)
nx run server-go:test-integration
```
