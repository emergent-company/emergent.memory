## Context

The Go server migration replaced NestJS with a Go implementation. While 455 E2E tests pass in Go, the test coverage is consolidated into 25 test files compared to 94 granular test files in NestJS. This document outlines technical decisions for porting the remaining test scenarios.

## Goals / Non-Goals

### Goals

- Achieve feature parity in E2E test coverage between NestJS and Go
- Maintain or improve test execution speed
- Follow existing Go test patterns (`testutil.TestServer`, `suite.Suite`)
- Consolidate related tests into logical test suites

### Non-Goals

- 1:1 file mapping (NestJS had overly granular test files)
- Porting unit tests (focus on E2E API tests only)
- Porting integration tests that test NestJS-specific internals
- Performance benchmarking (covered separately in `BENCHMARK_RESULTS.md`)

## Decisions

### Decision 1: Consolidate tests by domain, not by scenario

**What**: Instead of creating 94 separate Go test files, consolidate tests into ~15-20 domain-focused test suites.

**Why**:

- Go test suites share setup/teardown, reducing test execution time
- Easier navigation and maintenance
- Follows existing Go server patterns

**Mapping Example**:

```
NestJS (8 files):                    Go (1 file):
chat.streaming-sse.e2e.spec.ts    →  chat_streaming_test.go
chat.streaming-post.e2e.spec.ts   →  (ChatStreamingTestSuite)
chat.streaming-get.e2e.spec.ts    →
chat.streaming-error.e2e.spec.ts  →
chat.streaming-ordering.e2e.spec.ts →
...
```

### Decision 2: Create SSE test helper for streaming tests

**What**: Create `internal/testutil/sse.go` with helpers for parsing Server-Sent Events in tests.

**Why**: Multiple chat streaming tests need to parse SSE responses. A shared helper avoids duplication.

**Implementation**:

```go
// internal/testutil/sse.go
type SSEEvent struct {
    Event string
    Data  string
    ID    string
}

func ParseSSEResponse(body io.Reader) ([]SSEEvent, error)
func (s *TestServer) GetSSE(path string, opts ...RequestOption) (*httptest.ResponseRecorder, []SSEEvent)
```

### Decision 3: Use table-driven tests for scope matrix

**What**: Port `security.scopes-matrix.e2e.spec.ts` using Go's table-driven test pattern.

**Why**: The scope matrix tests many endpoint/scope combinations. Table-driven tests are idiomatic Go and easier to maintain.

**Example**:

```go
func (s *SecurityScopesTestSuite) TestScopeEnforcement() {
    tests := []struct {
        name     string
        endpoint string
        method   string
        scope    string
        want     int
    }{
        {"docs read requires kb:read", "/api/v2/documents", "GET", "kb:read", 200},
        {"docs read denied without scope", "/api/v2/documents", "GET", "", 403},
        // ... more cases
    }
    for _, tt := range tests {
        s.Run(tt.name, func() {
            // test logic
        })
    }
}
```

### Decision 4: Test isolation via separate test databases

**What**: Each test suite creates an isolated test database using `testutil.SetupTestDB()`.

**Why**: Existing pattern in Go tests. Ensures tests don't interfere with each other.

**Note**: Some NestJS tests used shared fixtures. Go tests should be self-contained.

### Decision 5: Skip NestJS-specific tests

**What**: Do not port tests that are NestJS-specific:

- `rls-migration-013.e2e.spec.ts` (TypeORM migration test)
- Tests relying on NestJS Testing Module internals
- Tests for deprecated features

**Why**: These tests validate NestJS internals, not API behavior.

## File Organization

```
apps/server-go/tests/e2e/
├── auth_test.go                    # Existing + auth error tests
├── chat_test.go                    # Existing + CRUD, lifecycle tests
├── chat_streaming_test.go          # NEW: All streaming tests
├── chat_citations_test.go          # NEW: Citation tests
├── documents_test.go               # Existing + pagination, edge cases
├── documents_dedup_test.go         # NEW: Dedup/duplicate detection
├── graph_test.go                   # Existing + traversal, history
├── graph_search_test.go            # NEW: Graph search variants
├── search_test.go                  # Existing + edge cases
├── search_modes_test.go            # NEW: Hybrid/lexical/vector modes
├── security_scopes_test.go         # NEW: Scope enforcement matrix
├── tenant_isolation_test.go        # NEW: RLS, cross-project isolation
├── ingestion_test.go               # NEW: Batch upload, error paths
├── project_members_test.go         # NEW: Member management
├── superadmin_test.go              # NEW: Superadmin endpoints
├── cleanup_test.go                 # NEW: Cascade delete verification
├── ... (existing files)
```

## Risks / Trade-offs

| Risk                                         | Mitigation                                                      |
| -------------------------------------------- | --------------------------------------------------------------- |
| Tests take longer to port than estimated     | Prioritize high-value tests (security, streaming) first         |
| Some NestJS tests have implicit dependencies | Review each test for hidden state; make Go tests self-contained |
| SSE parsing edge cases                       | Use well-tested SSE library or comprehensive helper tests       |
| Test database setup overhead                 | Reuse existing `testutil.SetupTestDB()` which is optimized      |

## Migration Plan

### Phase 1: Security & Authorization (Week 1)

1. Port security scope tests
2. Port RLS/isolation tests
3. Verify multi-tenant security

### Phase 2: Chat Streaming (Week 1-2)

1. Create SSE test helper
2. Port all streaming tests
3. Port citation tests

### Phase 3: Search & Documents (Week 2)

1. Port search mode variants
2. Port document edge cases
3. Port graph search tests

### Phase 4: Infrastructure & Cleanup (Week 2-3)

1. Port remaining tests
2. Verify full test suite passes
3. Update documentation

## Open Questions

1. **Should we maintain backward compatibility with NestJS test names in CI reports?**

   - Recommendation: No, use Go-idiomatic test names

2. **Should extraction tests wait for ADK-Go maturity?**

   - Recommendation: Port basic extraction tests, mark AI-dependent tests as integration tests

3. **How to handle flaky streaming tests?**
   - Recommendation: Use timeouts and retry logic in SSE helper
