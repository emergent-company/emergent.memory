# Design: Adopt Huma for OpenAPI Generation

## Context

The Go server uses Echo for HTTP routing. We need OpenAPI generation to:

1. Document the API for consumers
2. Enable eventual NestJS deprecation
3. Improve developer experience with typed handlers

## Goals

- Generate OpenAPI 3.1 spec from Go code
- Maintain functional equivalence with NestJS-generated spec
- Minimize migration effort (~4100 lines of handler code)
- Support incremental adoption

## Non-Goals

- Replace Echo framework entirely
- Generate client SDKs (future work)
- Exact byte-for-byte spec matching with NestJS

## Decisions

### Decision 1: Use Huma with humaecho adapter

**Choice**: Huma with `humaecho` adapter

**Alternatives considered**:

1. **swaggo** - Comment-based annotations (`// @Summary`, `// @Param`)
   - Pro: No handler signature changes
   - Con: Comments can drift from code, verbose, less type-safe
2. **ogen** - Generate handlers from OpenAPI spec
   - Pro: Spec-first approach
   - Con: Requires maintaining spec manually, doesn't fit existing codebase
3. **oapi-codegen** - Generate types from OpenAPI spec
   - Pro: Strong typing from spec
   - Con: Same issues as ogen, spec maintenance burden

**Rationale**: Huma provides the best balance:

- Works with existing Echo router (no framework replacement)
- Type-safe handlers with automatic validation
- OpenAPI generated from code (single source of truth)
- Proven in production (huma-blueprints-api reference)

### Decision 2: Incremental migration strategy

**Choice**: Migrate domain-by-domain, not big-bang

**Rationale**:

- Huma and raw Echo handlers can coexist on same router
- Allows testing each domain before moving to next
- Reduces risk of breaking changes

### Decision 3: Keep streaming endpoints as raw Echo

**Choice**: SSE and WebSocket endpoints stay as Echo handlers

**Rationale**:

- Huma focuses on request/response APIs
- Streaming has different patterns (chunked responses, long-lived connections)
- Only affects `chat` and `mcp` domains

### Decision 4: Domain file structure

**Choice**: Add `endpoints.go` and `dto.go` per domain

```
domain/documents/
├── handler.go      # Handler methods (modified signatures)
├── endpoints.go    # NEW: Huma operation registration
├── dto.go          # NEW: Request/response structs with Huma tags
├── service.go      # Unchanged
├── store.go        # Unchanged
└── module.go       # Wire endpoints registration
```

**Rationale**:

- Separates concerns (registration vs implementation)
- Matches huma-blueprints-api pattern
- DTOs with tags are reusable and self-documenting

## Migration Pattern

### Before (Echo)

```go
// handler.go
func (h *Handler) List(c echo.Context) error {
    user := auth.GetUser(c)
    limit, _ := strconv.Atoi(c.QueryParam("limit"))
    // manual validation...
    result, err := h.svc.List(ctx, params)
    return c.JSON(200, result)
}

// module.go
e.GET("/api/v2/documents", h.List, authMiddleware)
```

### After (Huma)

```go
// dto.go
type ListDocumentsRequest struct {
    Limit  int    `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Maximum results to return"`
    Cursor string `query:"cursor" doc:"Pagination cursor"`
}

type ListDocumentsResponse struct {
    Body struct {
        Documents  []Document `json:"documents"`
        NextCursor *string    `json:"nextCursor,omitempty"`
    }
}

// handler.go
func (h *Handler) List(ctx context.Context, req *ListDocumentsRequest) (*ListDocumentsResponse, error) {
    user := auth.UserFromContext(ctx)
    result, err := h.svc.List(ctx, ListParams{
        Limit:  req.Limit,
        Cursor: req.Cursor,
    })
    // ...
}

// endpoints.go
func RegisterEndpoints(h *Handler, api huma.API, security []map[string][]string) {
    huma.Register(api, huma.Operation{
        OperationID: "list-documents",
        Method:      http.MethodGet,
        Path:        "/api/v2/documents",
        Summary:     "List documents in project",
        Tags:        []string{"Documents"},
        Security:    security,
    }, h.List)
}
```

## Risks & Mitigations

| Risk                                  | Impact | Mitigation                                         |
| ------------------------------------- | ------ | -------------------------------------------------- |
| Handler signature changes break tests | Medium | E2E tests use HTTP, not direct handler calls       |
| OpenAPI spec drift from NestJS        | Low    | Functional equivalence check, not exact match      |
| Performance overhead                  | Low    | Huma adds minimal overhead, benchmark if concerned |
| Learning curve                        | Low    | Pattern is simple, document in AGENT.md            |

## Open Questions

1. **Auth context propagation** - How does Huma pass auth info to handlers?

   - Answer: Use `context.Context` with custom middleware that extracts auth and stores in context

2. **Error response format** - Does Huma's RFC 9457 format match our current errors?
   - Need to verify, may need custom error transformer

## References

- [Huma Documentation](https://huma.rocks/)
- [humaecho adapter](https://github.com/danielgtaylor/huma/tree/main/adapters/humaecho)
- [huma-blueprints-api reference](file:///root/huma-blueprints-api/)
