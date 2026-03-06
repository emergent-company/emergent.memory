# Go Server Migration Retrospective

## Project Summary

**Objective**: Port the NestJS/TypeScript backend server to Go for improved performance, reduced resource consumption, and faster cold starts.

**Timeline**: Multi-phase implementation following a strangler fig pattern

**Final Result**: 455 E2E tests passing, full feature parity with NestJS server

---

## What Went Well

### 1. Structured Migration Approach

The phased approach worked excellently:

- **Phase 0**: Research & Foundation - Established patterns before heavy coding
- **Phase 1-2**: Stateless APIs + Auth - Built foundation with simple modules first
- **Phase 3**: Data-Intensive Modules - Tackled graph/search complexity with solid patterns
- **Phase 4**: AI/LLM Integration - Successfully integrated Vertex AI, MCP
- **Phase 5**: Background Workers - PostgreSQL job queues matched NestJS patterns
- **Phase 6**: Cleanup & Cutover - Smooth migration of ownership

### 2. Technology Choices

| Technology         | Choice           | Outcome                                               |
| ------------------ | ---------------- | ----------------------------------------------------- |
| Web Framework      | Echo             | Excellent middleware system, fast routing             |
| ORM                | Bun              | Clean API, good pgvector support via custom types     |
| DI Framework       | uber/fx          | Lifecycle management, testability                     |
| Structured Logging | slog             | Native Go, structured output                          |
| Job Queues         | PostgreSQL-based | Matched existing NestJS patterns, simplified ops      |
| AI Extraction      | Google ADK-Go    | Native Go LLM orchestration, no Python sidecar needed |

### 3. Test-Driven Development

- E2E tests written alongside each module
- Contract tests ensured API compatibility
- Final count: **455 E2E tests** covering all endpoints and workers

### 4. Code Quality Metrics

| Metric        | NestJS              | Go             | Improvement              |
| ------------- | ------------------- | -------------- | ------------------------ |
| Source Files  | 659                 | 208            | 68% fewer files          |
| Lines of Code | ~109,000            | 59,573         | 45% less code            |
| Binary Size   | N/A (interpreted)   | 36 MB          | Single binary deployment |
| Dependencies  | 1,800+ npm packages | ~50 Go modules | Dramatically reduced     |

### 5. Architecture Patterns That Worked

- **Domain-driven structure**: `domain/{module}/` with handler, service, store, entity
- **fx.Module composition**: Clean dependency injection and lifecycle management
- **Middleware chains**: Reusable auth, RLS, logging middleware
- **Repository pattern**: Store layer abstracted database operations

---

## Challenges & Solutions

### 1. pgvector Integration with Bun

**Challenge**: Bun ORM didn't natively support pgvector's `vector` type.

**Solution**: Created custom `Vector` type in `internal/database/vector.go`:

```go
type Vector []float32

func (v *Vector) Scan(src any) error { ... }
func (v Vector) Value() (driver.Value, error) { ... }
```

### 2. Multi-tenant RLS (Row-Level Security)

**Challenge**: NestJS used TypeORM's query builder with automatic RLS filtering.

**Solution**: Created `RLSMiddleware` that sets `app.current_org_id` via `SET LOCAL`:

```go
func (m *RLSMiddleware) Apply(ctx context.Context, orgID string) context.Context {
    db.ExecContext(ctx, "SET LOCAL app.current_org_id = $1", orgID)
    return ctx
}
```

### 3. SSE Streaming for Chat

**Challenge**: Echo's response handling needed careful flushing for Server-Sent Events.

**Solution**: Created `SSEWriter` helper with proper flushing:

```go
type SSEWriter struct {
    w       http.ResponseWriter
    flusher http.Flusher
}

func (s *SSEWriter) WriteEvent(event, data string) error {
    fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
    s.flusher.Flush()
    return nil
}
```

### 4. Job Queue Concurrency

**Challenge**: Multiple workers processing the same jobs (race conditions).

**Solution**: Used `FOR UPDATE SKIP LOCKED` pattern:

```sql
SELECT * FROM kb.chunk_embedding_jobs
WHERE status = 'pending'
ORDER BY created_at
LIMIT 1
FOR UPDATE SKIP LOCKED
```

### 5. LangChain Alternative for Extraction

**Challenge**: LangChainGo was less mature than Python counterpart.

**Solution**: Discovered Google's Agent Development Kit (ADK-Go):

- Native Go support
- `SequentialAgent` for pipelines
- `LoopAgent` for retry logic
- `OutputSchema` for structured extraction

This eliminated the need for a Python sidecar entirely.

---

## Lessons Learned

### 1. Start Simple, Add Complexity Incrementally

Beginning with stateless health checks and settings APIs allowed us to establish patterns before tackling complex graph operations.

### 2. E2E Tests Are Essential for Migrations

The 455 E2E tests caught numerous edge cases and regressions. Contract tests ensured API compatibility between NestJS and Go implementations.

### 3. Go's Simplicity Is a Feature

The reduction from 659 files to 208 files wasn't just about Go's syntax - it reflected:

- No separate DTO/Entity/Model classes
- Explicit error handling (no try/catch overhead)
- Standard library covers most needs

### 4. fx.Module Pattern Scales Well

The uber/fx pattern of composing modules made testing easy and dependencies explicit. Each domain module could be tested in isolation.

### 5. PostgreSQL Job Queues > External Queues

Using PostgreSQL for job queues (matching NestJS patterns) was the right choice:

- No additional infrastructure
- Transactional guarantees
- Existing monitoring/tooling works

---

## Recommendations for Future Work

### 1. Performance Benchmarking

Tasks 3.1.8, 3.3.8, and 3.6.4 (benchmark comparisons) remain for manual testing under production load.

### 2. External Service Integration Tests

Tasks requiring production credentials (5.2.12, 5.3.8, 5.4.6) should be tested in staging environment.

### 3. CI/CD Pipeline Updates

Task 6.2.6 requires updating build pipelines:

- Add Go build stage
- Remove or gate NestJS build
- Update Docker images

### 4. Monitoring & Observability

Consider adding:

- Prometheus metrics endpoint
- Distributed tracing (OpenTelemetry)
- Custom Langfuse integration for LLM calls

### 5. Documentation Maintenance

Keep AGENT.md files updated as patterns evolve.

---

## Team Acknowledgments

This migration was a significant undertaking that required careful planning, iterative development, and thorough testing. The phased approach and comprehensive test suite were key to success.

---

## Appendix: Key Files Reference

| Purpose             | File                            |
| ------------------- | ------------------------------- |
| Server entry point  | `cmd/server/main.go`            |
| Migration CLI       | `cmd/migrate/main.go`           |
| Database connection | `internal/database/database.go` |
| Auth middleware     | `internal/auth/middleware.go`   |
| RLS middleware      | `internal/middleware/rls.go`    |
| Job queue base      | `internal/jobs/queue.go`        |
| ADK extraction      | `pkg/adk/`                      |
| Domain modules      | `domain/{module}/module.go`     |
| E2E tests           | `tests/e2e/`                    |
| Benchmark results   | `BENCHMARK_RESULTS.md`          |
| Migration docs      | `migrations/README.md`          |
