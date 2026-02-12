# Change: Port Remaining NestJS E2E Tests to Go

## Why

The Go server migration is complete with 455 passing E2E tests, but only ~25 test files exist compared to ~94 test files in NestJS. Many specialized test scenarios (chat streaming, security scopes, search variants, RLS isolation, document edge cases) are not yet ported to Go. Without full test parity, we risk:

1. **Regression gaps**: Edge cases tested in NestJS may break undetected in Go
2. **Security coverage**: Scope enforcement and RLS isolation tests ensure multi-tenant security
3. **Performance confidence**: Search and streaming tests validate latency requirements
4. **API compatibility**: Contract tests alone don't cover all behavioral scenarios

## What Changes

- Port ~69 missing NestJS E2E test files to Go E2E tests
- Consolidate related tests into logical Go test suites (fewer files, same coverage)
- Add streaming SSE tests using Go's httptest and SSE parsing
- Add security scope matrix tests
- Add RLS isolation tests
- Add document pagination/dedup edge case tests
- Add search mode variant tests (lexical, vector, hybrid)

### Test Categories to Port

| Category             | NestJS Files | Priority | Notes                                     |
| -------------------- | ------------ | -------- | ----------------------------------------- |
| Chat streaming       | 8 files      | High     | SSE streaming, error handling, ordering   |
| Chat features        | 9 files      | High     | Citations, authorization, MCP integration |
| Security/scopes      | 4 files      | High     | Critical for multi-tenant security        |
| Search variants      | 9 files      | High     | Lexical, vector, hybrid modes             |
| Documents edge cases | 10 files     | Medium   | Pagination, dedup, isolation              |
| Graph advanced       | 7 files      | Medium   | Traversal, branching, history             |
| RLS/isolation        | 3 files      | High     | Multi-tenant data isolation               |
| Ingestion            | 4 files      | Medium   | Batch upload, error paths                 |
| Extraction           | 3 files      | Low      | May need ADK-Go specific tests            |
| Infrastructure       | 12 files     | Low      | OpenAPI, cleanup, performance             |

## Impact

- **Affected specs**: `testing`
- **Affected code**: `apps/server-go/tests/e2e/`
- **Estimated effort**: 2-3 weeks
- **No breaking changes**: Adding tests only
