## Context

The Go server has replaced NestJS as the primary backend. During E2E test porting, we identified feature gaps where Go lacks functionality present in NestJS. This design documents technical decisions for implementing the missing features.

**Reference**: See `openspec/changes/port-nestjs-e2e-tests-to-go/COVERAGE_REPORT.md` for detailed gap analysis.

## Goals / Non-Goals

### Goals

- Achieve API parity for actionable features (no external infrastructure required)
- Maintain consistency with existing Go patterns (fx modules, Echo handlers, Bun ORM)
- All new endpoints have E2E test coverage
- Response formats match NestJS for client compatibility

### Non-Goals

- Features requiring LLM/embedding infrastructure (tested separately when infra available)
- Langfuse integration (Go uses SigNoz for observability)
- OpenAPI generation (separate tooling)
- Performance optimization (current implementation sufficient)

## Decisions

### 1. Batch Upload: Multipart Form with Array Response

**Decision**: Use `multipart/form-data` with multiple `file` fields, return array of results.

```go
// Request: POST /api/v2/documents/upload/batch
// Content-Type: multipart/form-data
// file[0], file[1], file[2], ...

// Response
{
  "results": [
    {"index": 0, "id": "doc-123", "status": "created"},
    {"index": 1, "id": null, "status": "error", "error": "file too large"},
    {"index": 2, "id": "doc-456", "status": "created"}
  ],
  "summary": {"total": 3, "created": 2, "failed": 1}
}
```

**Rationale**: Matches NestJS behavior, allows partial success, client can correlate by index.

**Limits**: Max 10 files per batch, 50MB total size (configurable via env).

### 2. New Domains: Follow Existing Module Pattern

**Decision**: Create new fx modules following existing pattern in `domain/`.

```
domain/
├── embeddingpolicies/    # New
│   ├── entity.go
│   ├── store.go
│   ├── service.go
│   ├── handler.go
│   ├── routes.go
│   └── module.go
├── branches/             # New
│   └── ...
├── superadmin/           # New
│   └── ...
└── agents/               # New (admin)
    └── ...
```

**Rationale**: Consistency with existing codebase, easy to understand and test.

### 3. Embedding Policies: Use Existing Table

**Decision**: Map to existing `kb.embedding_policies` table, no schema changes.

```go
type EmbeddingPolicy struct {
    bun.BaseModel `bun:"table:kb.embedding_policies,alias:ep"`

    ID          string    `bun:"id,pk"`
    ProjectID   string    `bun:"project_id"`
    Name        string    `bun:"name"`
    ObjectType  string    `bun:"object_type"`
    FieldPaths  []string  `bun:"field_paths,array"`
    Model       string    `bun:"model"`
    Enabled     bool      `bun:"enabled"`
    CreatedAt   time.Time `bun:"created_at"`
    UpdatedAt   time.Time `bun:"updated_at"`
}
```

**Scope**: `embeddings:manage` (new scope, or reuse existing if available)

### 4. Branches: Use Existing Table

**Decision**: Map to existing `kb.branches` table.

```go
type Branch struct {
    bun.BaseModel `bun:"table:kb.branches,alias:b"`

    ID          string    `bun:"id,pk"`
    ProjectID   string    `bun:"project_id"`
    Name        string    `bun:"name"`
    Description string    `bun:"description"`
    BaseBranch  *string   `bun:"base_branch_id"`
    CreatedAt   time.Time `bun:"created_at"`
    UpdatedAt   time.Time `bun:"updated_at"`
}
```

**Scope**: `graph:write` (existing scope)

### 5. Search Debug Mode: Optional Response Extension

**Decision**: Add `debug` query param, return timing in optional `debug` field.

```go
type SearchResponse struct {
    Results []SearchResult `json:"results"`
    Debug   *DebugInfo     `json:"debug,omitempty"`  // Only when ?debug=true
}

type DebugInfo struct {
    FTSTimeMs     int `json:"fts_time_ms"`
    VectorTimeMs  int `json:"vector_time_ms"`
    FusionTimeMs  int `json:"fusion_time_ms"`
    TotalTimeMs   int `json:"total_time_ms"`
    FTSMatches    int `json:"fts_matches"`
    VectorMatches int `json:"vector_matches"`
}
```

**Rationale**: Non-breaking change, useful for debugging, matches NestJS behavior.

### 6. Superadmin: Separate Auth Check

**Decision**: Create `RequireSuperadmin()` middleware that checks for superadmin role in token claims.

```go
func (m *Middleware) RequireSuperadmin() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            user := GetUser(c)
            if !user.IsSuperadmin {
                return apperror.ErrForbidden.WithMessage("superadmin access required")
            }
            return next(c)
        }
    }
}
```

**Routes**: `/api/superadmin/*` - all require superadmin role

### 7. Agents API: Admin Routes

**Decision**: Place under `/api/admin/agents/*` with admin-level auth.

**Note**: This is for batch triggering reaction agents, not full agent management.

## Risks / Trade-offs

| Risk                      | Mitigation                                          |
| ------------------------- | --------------------------------------------------- |
| Batch upload memory usage | Streaming file handling, enforce size limits        |
| Superadmin abuse          | Audit logging for all superadmin actions            |
| Scope proliferation       | Reuse existing scopes where possible                |
| Test complexity           | Follow existing test patterns, use testutil helpers |

## Migration Plan

No migration required - all additive changes:

1. New endpoints don't affect existing functionality
2. Database tables already exist
3. Clients can adopt new endpoints incrementally

## Open Questions

1. **Batch upload limits**: What are appropriate defaults for max files and total size?

   - Proposed: 10 files, 50MB total (same as NestJS)

2. **Embedding policy scope**: Create new `embeddings:manage` or reuse existing?

   - Need to check what scopes NestJS uses

3. **Agents API scope**: What admin scope is required?
   - Need to check NestJS implementation
