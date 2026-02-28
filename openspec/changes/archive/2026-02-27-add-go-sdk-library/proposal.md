# Change: Add Go SDK Library for Emergent API

## Why

External Go applications need a simple, idiomatic way to interact with the Emergent API without manually crafting HTTP requests and handling authentication. Currently:

- Developers must build custom HTTP clients and handle auth themselves
- No type-safe client exists for external use
- The emergent-cli's internal client (`tools/emergent-cli/internal/client`) cannot be imported externally
- Duplicate auth logic exists across tools (CLI, tests)

This blocks:

- Third-party integrations
- Custom automation tools
- Enterprise workflows that need programmatic access
- External services that want to embed Emergent functionality

**Foundation Complete**: The server now has comprehensive OpenAPI/Swagger documentation:

- ✅ **195 endpoints** fully documented across 33 domain modules
- ✅ **17,289 lines** of OpenAPI 2.0 specification (`apps/server-go/docs/swagger/swagger.json`)
- ✅ **100% coverage** - All handlers annotated with complete request/response schemas
- ✅ **Auto-generated** on every build with validation

## What Changes

Create a comprehensive, production-ready Go SDK library at `apps/server-go/pkg/sdk` that:

1. **Covers ALL 195 API endpoints**: Documents, chunks, graph, search, chat, agents, data sources, projects, orgs, users, API tokens, health, MCP, etc.
2. **Leverages complete OpenAPI spec**: SDK design informed by 100% documented API surface
3. **Handles authentication**: Supports both standalone (API key) and full deployment (OAuth/Zitadel) modes
4. **Provides type-safe interfaces**: Strong typing for all request/response DTOs derived from OpenAPI schemas
5. **Manages sessions**: Automatic token refresh, credential caching, multi-tenant context
6. **Supports streaming**: SSE for chat streaming, agent runs, search debug mode
7. **Enables testing**: Mock-friendly interfaces, test utilities derived from OpenAPI examples
8. **Is well-documented**: Comprehensive examples, godoc, quickstart guide

**BREAKING**: None - this is a new additive library.

## Impact

### Affected specs

- **NEW**: `go-sdk` - Comprehensive Go client library specification

### Affected code

- **NEW**: `apps/server-go/pkg/sdk/` - SDK package
- **UPDATE**: `tools/emergent-cli/internal/client/client.go` - Refactor to use SDK
- **UPDATE**: `tests/api/client/` - Refactor test client to use SDK
- **NEW**: `docs/SDK_GUIDE.md` - SDK usage documentation
- **NEW**: `examples/go-sdk/` - Example applications
- **REFERENCE**: `apps/server-go/docs/swagger/swagger.json` - Source of truth for API contracts

### Dependencies

- Existing: `github.com/uptrace/bun` (for database types)
- Existing: OAuth OIDC libraries (device flow)
- Existing: Complete OpenAPI specification (17,289 lines)
- No new external dependencies required

### Migration path

1. SDK published as importable package `github.com/emergent-company/emergent/apps/server-go/pkg/sdk`
2. CLI and test clients migrated to use SDK internally
3. Existing code paths remain compatible
4. External users can immediately start using the SDK
5. OpenAPI spec serves as source of truth for SDK type generation

### Testing

- Unit tests for all SDK components (~40 files)
- Integration tests against real server (195 endpoints)
- Example applications as live tests
- Mock server for offline testing (derived from OpenAPI spec)
- Type compatibility tests comparing SDK DTOs with OpenAPI schemas
