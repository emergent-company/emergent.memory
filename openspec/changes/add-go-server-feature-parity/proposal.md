# Change: Add Go Server Feature Parity with NestJS

## Why

During the NestJS-to-Go E2E test porting effort, we identified several feature gaps where the Go server lacks functionality that exists in NestJS. While the Go server covers all production-critical paths, these gaps limit certain workflows and administrative capabilities.

This proposal addresses the **actionable** gaps - features that can be implemented without external infrastructure (LLM, embeddings) and provide clear user value.

## What Changes

### High Priority (Batch Operations)

- **Batch file upload** - Go currently supports single file upload only; NestJS supports batch ingestion

### Medium Priority (API Parity)

- **Embedding policies API** - CRUD endpoints for `/api/v2/graph/embedding-policies`
- **Branch CRUD API** - CRUD endpoints for `/api/v2/graph/branches`
- **Search debug mode** - Add `?debug=true` parameter to return timing/stats metadata

### Lower Priority (Admin Features)

- **Superadmin API** - Full `/api/superadmin/*` endpoints (users, orgs, projects, email-jobs, view-as)
- **Agents API** - `/api/admin/agents/*` endpoints for reaction agent batch triggers

### Deferred (Infrastructure/Design Decisions)

- **ETag caching** - HTTP conditional requests (304 Not Modified) - needs design decision
- **External sources import API** - Complex feature, may need separate proposal

### Explicitly Out of Scope

- **Langfuse tracing** - Not planned for Go implementation; observability handled via structured logging and SigNoz

## Impact

- **Affected specs**: `document-management`, `entity-extraction`, `database-access`
- **Affected code**:
  - `apps/server-go/domain/documents/` (batch upload)
  - `apps/server-go/domain/graph/` (branches, embedding policies, debug mode)
  - `apps/server-go/domain/superadmin/` (new domain)
  - `apps/server-go/domain/agents/` (new domain)
- **Database**: No schema changes required (tables already exist)
- **Breaking changes**: None - all additive

## Success Criteria

1. All new endpoints have E2E tests
2. API responses match NestJS format for client compatibility
3. Scope enforcement consistent with existing patterns
4. Documentation updated in AGENT.md

## Out of Scope

- Features that require external infrastructure (LLM, Vertex AI embeddings)
- Features already skipped/disabled in NestJS
- Performance optimizations
- **Langfuse tracing** - Go uses structured logging + SigNoz for observability, not Langfuse
- **OpenAPI annotations in Go** - The root `openapi.yaml` (15k lines) is generated from NestJS and remains the canonical spec. Go endpoints match NestJS API contracts. Adding swaggo annotations would be significant effort with limited benefit since NestJS spec is already complete.
