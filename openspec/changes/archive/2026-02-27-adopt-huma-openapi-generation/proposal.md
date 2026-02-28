# Change: Adopt Huma for OpenAPI Generation in Go Server

## Why

The Go server currently has no way to generate OpenAPI specs. The root `openapi.yaml` (15k lines) is generated from NestJS via `@nestjs/swagger` decorators. To remove NestJS entirely, we need Go to generate its own OpenAPI spec.

Huma is a lightweight framework that:

- Works **with** Echo via `humaecho` adapter (not a replacement)
- Auto-generates OpenAPI 3.1 from Go struct tags
- Provides declarative request/response types with built-in validation
- Is already proven in production (used by huma-blueprints-api reference project)

## What Changes

### Core Integration

- Add `github.com/danielgtaylor/huma/v2` dependency
- Create `humaecho` adapter wrapper in `internal/server/`
- Configure Bearer JWT security scheme matching current auth

### Handler Migration Pattern

Convert handlers from raw Echo to Huma's typed pattern:

**Before** (manual parsing):

```go
func (h *Handler) List(c echo.Context) error {
    limit, _ := strconv.Atoi(c.QueryParam("limit"))
    // ... manual validation
    return c.JSON(200, result)
}
```

**After** (declarative):

```go
type ListRequest struct {
    Limit int `query:"limit" default:"100" minimum:"1" maximum:"500" doc:"Max results"`
}
type ListResponse struct {
    Body struct {
        Results []Item `json:"results"`
    }
}
func (h *Handler) List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
    // req is already parsed and validated
}
```

### Domain Structure

Each domain gains an `endpoints.go` file:

```go
func RegisterEndpoints(handler Handler, api huma.API, security []map[string][]string) {
    huma.Register(api, huma.Operation{
        OperationID: "list-documents",
        Method:      http.MethodGet,
        Path:        "/api/documents",
        Summary:     "List documents",
        Tags:        []string{"Documents"},
        Security:    security,
    }, handler.List)
}
```

## Impact

- **Affected code**:
  - `apps/server-go/internal/server/server.go` - Add Huma adapter
  - `apps/server-go/domain/*/handler.go` - Convert to typed handlers (~21 domains)
  - `apps/server-go/domain/*/endpoints.go` - New files for operation registration
  - `apps/server-go/domain/*/dto.go` - Request/response structs with Huma tags
- **Dependencies**: Add `github.com/danielgtaylor/huma/v2`
- **Breaking changes**: None - internal refactor, API contracts unchanged
- **OpenAPI output**: `/openapi.json` will serve Go-generated spec

## Success Criteria

1. Go server serves OpenAPI spec at `/openapi.json`
2. Spec is functionally equivalent to NestJS-generated spec (same endpoints, params, response shapes)
3. All existing E2E tests continue to pass
4. New endpoints added with Huma pattern automatically appear in spec

## Dependencies

- Blocked by: `add-go-server-feature-parity` (complete feature gaps first)
- Enables: Full NestJS deprecation

## Out of Scope

- Exact field-for-field match with NestJS spec (functional equivalence is sufficient)
- Migration of streaming endpoints (SSE/WebSocket) - keep as raw Echo
- Client SDK generation (future work)
