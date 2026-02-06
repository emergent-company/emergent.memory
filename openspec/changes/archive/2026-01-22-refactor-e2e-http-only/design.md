# Design: Refactor E2E Tests to HTTP-Only Mode

## Context

The Go backend (`apps/server-go`) has a comprehensive e2e test suite with 455 tests across 31 files. The test infrastructure in `internal/testutil/` supports two modes:

1. **In-process mode** (default): Spins up isolated test database, provides direct DB access
2. **External server mode** (`TEST_SERVER_URL` env var): Uses HTTP-only calls to running server

Currently, 8 test files use direct database operations, making them incompatible with external server mode.

### Stakeholders

- Backend developers writing/maintaining tests
- CI/CD pipelines running tests
- QA running tests against staging environments

## Goals / Non-Goals

### Goals

- All e2e tests SHOULD be runnable against an external server
- Tests that cannot be HTTP-only SHOULD be clearly marked and skipped in external mode
- HTTP helper methods SHOULD be sufficient for common test fixtures
- Test patterns SHOULD reflect real client-API interactions

### Non-Goals

- Not changing the test infrastructure architecture
- Not adding new API endpoints just for testing (unless they have production value)
- Not converting unit/integration tests to e2e tests

## Decisions

### Decision 1: Use skip pattern for internal-only tests

**What**: Tests that genuinely require internal access (scheduler internals, worker state manipulation) will use `SkipIfExternalServer()` rather than being removed or rewritten.

**Why**: Some tests validate internal behavior that cannot be observed via API. Removing them would reduce test coverage. Skipping them in external mode preserves their value while allowing the rest of the suite to run.

**Alternatives considered**:

- Remove internal tests: Loses valuable coverage
- Add internal admin APIs: Over-engineering for test purposes
- Keep tests broken in external mode: Poor developer experience

### Decision 2: Job workflow testing via document lifecycle

**What**: Instead of creating jobs directly in the database, tests will upload documents/trigger operations via API and wait for job completion.

**Why**: This tests the real user workflow. Jobs are an implementation detail; users trigger them via document uploads, extractions, etc.

**Implementation**:

```go
// Instead of:
s.DB().NewInsert().Model(&job).Exec(ctx)

// Use:
doc := s.Client.UploadDocument(ctx, projectID, file)
s.Client.WaitForDocumentProcessed(ctx, doc.ID, 30*time.Second)
```

### Decision 3: Polling-based job completion checks

**What**: Add `WaitForJobCompletion()` helper that polls job status endpoint with configurable timeout.

**Why**: Async job processing means tests need to wait for completion. Polling is simple and works in all environments.

**Implementation**:

```go
func (c *HTTPClient) WaitForJobCompletion(ctx context.Context, jobID string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        status, err := c.GetJobStatus(ctx, jobID)
        if err != nil {
            return err
        }
        if status.State == "completed" || status.State == "failed" {
            return nil
        }
        time.Sleep(500 * time.Millisecond)
    }
    return fmt.Errorf("job %s did not complete within %v", jobID, timeout)
}
```

### Decision 4: Preserve test isolation

**What**: Each test should create its own fixtures via API, not rely on shared state or direct DB seeding.

**Why**: Test isolation is critical for reliable, parallelizable tests. Direct DB seeding can create hidden dependencies.

## Risks / Trade-offs

| Risk                                     | Mitigation                                                                    |
| ---------------------------------------- | ----------------------------------------------------------------------------- |
| Tests become slower due to polling       | Use reasonable poll intervals (500ms); most jobs complete quickly in test env |
| Some behaviors untestable via API        | Accept skip for truly internal tests; document coverage gaps                  |
| HTTP helpers become complex              | Keep helpers focused; each helper does one thing                              |
| Missing API endpoints for test scenarios | Evaluate if endpoint has production value before adding                       |

## Migration Plan

1. **Phase 1**: Add infrastructure (skip helper, HTTP helpers) - no test changes
2. **Phase 2**: Refactor tests one file at a time, running suite after each
3. **Phase 3**: Validate external server mode works end-to-end
4. **Rollback**: Changes are additive; can revert individual file changes if issues arise

## Open Questions

1. **Q**: Should we add a job management API for test observability?
   **A**: Only if it has production debugging value. Otherwise, use document status APIs.

2. **Q**: What's an acceptable number of skipped tests in external mode?
   **A**: Aim for <5% skipped. Document each skip with clear reasoning.

3. **Q**: Should external mode tests run in CI?
   **A**: Yes, as a separate job testing against a deployed dev environment.
