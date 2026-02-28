# Go SDK Library - Design Document

## Context

The Emergent platform provides a comprehensive REST API for knowledge management, but lacks an official Go client library. Currently:

- **CLI** has internal client at `tools/emergent-cli/internal/client` (not importable)
- **Tests** have custom HTTP helpers at `tests/api/client` (tightly coupled to tests)
- **External users** must manually construct HTTP requests

**API Documentation Complete**: The server now has comprehensive OpenAPI 2.0 documentation:

- **195 endpoints** across 33 domain modules (agents, chat, chunks, data sources, documents, email, extraction, graph, health, mcp, organization, project, scheduler, search, storage, user)
- **17,289 lines** of machine-readable specification at `apps/server-go/docs/swagger/swagger.json`
- **100% handler coverage** - Every endpoint annotated with complete request/response schemas, parameters, and error codes
- **Auto-generated on build** - Specification stays in sync with code changes
- **Validation enforced** - Pre-commit hooks prevent unannotated endpoints

Both authentication modes must be supported:

- **Standalone**: Simple API key via `X-API-Key` header (Docker deployments)
- **Full**: OAuth 2.0 with Zitadel (device flow + token refresh)

## Goals

- Provide a **production-ready** Go SDK for all 195 Emergent API endpoints
- Leverage the **complete OpenAPI specification** as source of truth for API contracts
- Support **both authentication modes** seamlessly
- Enable **type-safe** interactions with strong Go typing derived from OpenAPI schemas
- Make **common tasks simple** (e.g., uploading documents, searching, chatting)
- Support **advanced features** (streaming, pagination, multi-tenancy)
- Be **testable** with mock-friendly interfaces generated from OpenAPI examples

## Non-Goals

- Generate SDK from OpenAPI spec (manual implementation for better Go ergonomics, but informed by spec)
- Support other languages (this is Go-specific)
- Replace the CLI (CLI will use SDK internally)
- Modify server-side API (pure client implementation)
- Regenerate types on every OpenAPI change (manually sync when needed)

## Architecture

### Package Structure

```
apps/server-go/pkg/sdk/
├── sdk.go                    # Main Client + config
├── auth/
│   ├── auth.go               # Auth interface + factory
│   ├── apikey.go             # API key provider
│   ├── oauth.go              # OAuth provider (device flow + refresh)
│   └── credentials.go        # Credential storage/loading
├── documents/
│   ├── client.go             # Documents service
│   └── types.go              # Document DTOs
├── chunks/
│   ├── client.go             # Chunks service
│   └── types.go              # Chunk DTOs
├── graph/
│   ├── objects.go            # Graph objects service
│   ├── relationships.go      # Graph relationships service
│   ├── search.go             # Graph search service
│   └── types.go              # Graph DTOs
├── search/
│   ├── client.go             # Unified search service
│   └── types.go              # Search DTOs
├── chat/
│   ├── client.go             # Chat service
│   ├── stream.go             # SSE streaming handler
│   └── types.go              # Chat DTOs
├── projects/
│   ├── client.go             # Projects service
│   └── types.go              # Project DTOs
├── orgs/
│   ├── client.go             # Organizations service
│   └── types.go              # Organization DTOs
├── users/
│   ├── client.go             # Users service
│   └── types.go              # User DTOs
├── apitokens/
│   ├── client.go             # API tokens service
│   └── types.go              # API token DTOs
├── health/
│   ├── client.go             # Health check service
│   └── types.go              # Health DTOs
├── mcp/
│   ├── client.go             # MCP service
│   └── types.go              # MCP DTOs
├── errors/
│   └── errors.go             # SDK error types
└── testutil/
    ├── mock.go               # Mock server utilities
    └── fixtures.go           # Test data helpers
```

### Core Client Design

```go
// SDK main client
type Client struct {
    auth      auth.Provider
    base      string
    orgID     string
    projectID string
    http      *http.Client

    Documents     *documents.Client
    Chunks        *chunks.Client
    Graph         *graph.Client
    Search        *search.Client
    Chat          *chat.Client
    Projects      *projects.Client
    Orgs          *orgs.Client
    Users         *users.Client
    APITokens     *apitokens.Client
    Health        *health.Client
    MCP           *mcp.Client
}

// Config for SDK initialization
type Config struct {
    ServerURL string
    Auth      AuthConfig
    OrgID     string    // Optional: default org
    ProjectID string    // Optional: default project
}

type AuthConfig struct {
    Mode         string  // "apikey" or "oauth"
    APIKey       string  // For standalone mode
    CredsPath    string  // For OAuth credential storage
    ClientID     string  // For OAuth mode
}
```

### Authentication Strategy

#### 1. API Key Mode (Standalone)

Simple and stateless:

```go
client, err := sdk.New(sdk.Config{
    ServerURL: "http://localhost:9090",
    Auth: sdk.AuthConfig{
        Mode:   "apikey",
        APIKey: "emt_abc123...",
    },
})
```

Backend receives: `X-API-Key: emt_abc123...`

#### 2. OAuth Mode (Full Deployment)

Interactive device flow:

```go
// First-time: device flow login
client, err := sdk.NewWithDeviceFlow(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth: sdk.AuthConfig{
        Mode:      "oauth",
        ClientID:  "emergent-sdk",
        CredsPath: "~/.emergent/credentials.json",
    },
})
// Prints: "Visit https://zitadel.com/device and enter code: ABCD-1234"
// Polls until user completes auth
// Stores credentials at CredsPath

// Subsequent calls: auto-load credentials
client, err := sdk.New(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth: sdk.AuthConfig{
        Mode:      "oauth",
        CredsPath: "~/.emergent/credentials.json",
    },
})
// Auto-refreshes tokens when expired
```

Backend receives: `Authorization: Bearer eyJhbGc...`

### Multi-Tenancy Context

All API calls require org/project context:

```go
// Option 1: Set defaults on client
client.SetContext("org_123", "proj_456")

// Option 2: Override per call
docs, err := client.Documents.List(ctx, sdk.WithOrg("org_789"), sdk.WithProject("proj_012"))

// Backend receives:
// X-Org-ID: org_789
// X-Project-ID: proj_012
```

### Error Handling

Structured errors matching server responses:

```go
type Error struct {
    StatusCode int
    Code       string
    Message    string
    Details    map[string]interface{}
}

func (e *Error) Error() string {
    return fmt.Sprintf("[%d] %s: %s", e.StatusCode, e.Code, e.Message)
}

// Usage:
_, err := client.Documents.Get(ctx, "invalid-id")
if sdkErr, ok := err.(*sdk.Error); ok {
    if sdkErr.Code == "not_found" {
        // Handle missing document
    }
}
```

### Streaming Support

Chat uses SSE streaming:

```go
stream, err := client.Chat.SendMessageStream(ctx, "conv_123", "What is the capital of France?")
if err != nil {
    log.Fatal(err)
}
defer stream.Close()

for event := range stream.Events() {
    switch event.Type {
    case "token":
        fmt.Print(event.Token)
    case "done":
        fmt.Println("\nResponse complete")
    case "error":
        log.Printf("Error: %s", event.Error)
    }
}
```

## Decisions

### Decision 1: Manual Implementation vs Code Generation

**Choice**: Manual implementation with strong Go idioms, informed by OpenAPI specification

**Rationale**:

- OpenAPI codegen produces verbose, unidiomatic Go code
- Manual allows better error handling, streaming support, helpers
- **Complete OpenAPI spec (17,289 lines, 195 endpoints) serves as source of truth** for:
  - API contract verification
  - Type structure design
  - Request/response validation
  - Test case generation
  - Documentation accuracy
- Can optimize for common patterns (e.g., `Get`, `List`, `Create`, `Update`, `Delete`)
- Manually sync types when OpenAPI schema changes (periodic review)

**Trade-off**: More initial effort, but better developer experience and maintainability

**Implementation Strategy**:

1. Reference OpenAPI schemas when designing SDK types
2. Validate SDK requests/responses match OpenAPI contracts
3. Generate test fixtures from OpenAPI examples
4. Add compatibility tests comparing SDK types with OpenAPI definitions

### Decision 2: Separate Service Clients vs Monolithic

**Choice**: Separate service clients under main `Client` struct

**Rationale**:

- Better organization: `client.Documents.List()` vs `client.ListDocuments()`
- Easier testing: mock individual services
- Cleaner namespacing for types
- Follows patterns from AWS SDK, Google Cloud SDK

**Trade-off**: Slightly more code, but much better ergonomics

### Decision 3: Auth Provider Interface vs Concrete Types

**Choice**: `auth.Provider` interface with factory pattern

**Rationale**:

- Enables testing with mock auth
- Clean separation: auth logic separate from HTTP logic
- Easy to add new auth methods later (service accounts, JWT, etc.)

```go
type Provider interface {
    Authenticate(req *http.Request) error
    Refresh(ctx context.Context) error
}
```

### Decision 4: Re-use DTOs vs SDK-Specific Types

**Choice**: SDK-specific types that match OpenAPI schemas, not server internal DTOs

**Rationale**:

- Server DTOs are internal (not exported package)
- SDK needs to be independent (different import path)
- **OpenAPI specification provides complete type definitions** for all 195 endpoints
- Allows SDK-specific helpers (e.g., `ToJSON()`, `Validate()`)
- Can optimize for client use cases (e.g., pagination helpers)
- OpenAPI schemas include validation rules, constraints, examples
- Type compatibility can be verified programmatically

**Trade-off**: Some duplication, but complete independence and API contract validation

**Implementation Strategy**:

1. Design SDK types to match OpenAPI `definitions` section
2. Add validation tags from OpenAPI constraints
3. Include godoc from OpenAPI descriptions
4. Periodically verify types match current OpenAPI spec

### Decision 5: Pagination Strategy

**Choice**: Iterator pattern with `Next()` / `HasNext()`

```go
iter := client.Documents.ListIter(ctx, &documents.ListOptions{Limit: 50})
for iter.Next() {
    doc := iter.Document()
    fmt.Println(doc.Title)
}
if err := iter.Err(); err != nil {
    log.Fatal(err)
}
```

**Rationale**:

- Familiar Go pattern (like `sql.Rows`)
- Hides cursor complexity
- Memory-efficient (doesn't load all pages)

### Decision 6: Context Propagation

**Choice**: Require `context.Context` as first parameter for all API calls

**Rationale**:

- Standard Go idiom
- Enables cancellation, timeouts, tracing
- Server already uses context

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

docs, err := client.Documents.List(ctx)
```

## Risks / Trade-offs

| Risk                         | Mitigation                                                                            |
| ---------------------------- | ------------------------------------------------------------------------------------- |
| **API changes break SDK**    | Version SDK alongside server, document compatibility matrix, validate against OpenAPI |
| **Auth complexity**          | Comprehensive tests, clear error messages, examples for both modes                    |
| **Maintenance burden**       | Start with most-used endpoints, add based on demand, automate type validation         |
| **Type drift from server**   | Regular sync checks with OpenAPI spec, automated compatibility tests                  |
| **OpenAPI spec incomplete**  | Already 100% coverage (195/195 endpoints), validation enforced by pre-commit          |
| **Breaking OpenAPI changes** | Monitor `swagger.json` in version control, add tests comparing SDK with spec          |

## OpenAPI Specification Integration

### Source of Truth

The **complete OpenAPI 2.0 specification** (`apps/server-go/docs/swagger/swagger.json`) serves as the authoritative API contract:

- **17,289 lines** of machine-readable specification
- **195 endpoints** across 33 domain modules
- **100% coverage** - All handlers annotated
- **Auto-generated** on every build
- **Validated** by pre-commit hooks

### SDK Development Workflow

1. **Design SDK types** based on OpenAPI `definitions` section:

   ```go
   // From swagger.json "definitions" section
   type DocumentDTO struct {
       ID          string    `json:"id"`
       Title       string    `json:"title"`
       Content     string    `json:"content"`
       CreatedAt   time.Time `json:"created_at"`
       // ... matches OpenAPI schema
   }
   ```

2. **Validate requests match OpenAPI parameters**:

   - Path parameters
   - Query parameters
   - Header requirements (X-Org-ID, X-Project-ID)
   - Request body schemas

3. **Generate test fixtures from OpenAPI examples**:

   - Use `examples` from OpenAPI annotations
   - Create mock server responses matching OpenAPI schemas
   - Validate SDK serialization/deserialization

4. **Add compatibility tests**:
   ```go
   func TestSDKTypesMatchOpenAPI(t *testing.T) {
       spec := loadOpenAPISpec()
       // Compare SDK types with OpenAPI definitions
       // Fail if types diverge from spec
   }
   ```

### Keeping SDK in Sync

- **Monitor `swagger.json` changes** in version control
- **Run compatibility tests** on every SDK change
- **Document breaking changes** in SDK CHANGELOG
- **Version SDK independently** but link to server version compatibility

## Migration Plan

### Phase 1: Core SDK (Week 1)

- Implement auth providers (API key + OAuth)
- Implement Documents, Chunks, Search clients
- Add comprehensive tests
- Write quickstart guide

### Phase 2: Graph & Chat (Week 2)

- Implement Graph (objects, relationships, search)
- Implement Chat with streaming
- Add integration tests
- Add examples

### Phase 3: Management APIs (Week 3)

- Implement Projects, Orgs, Users, API Tokens
- Implement Health, MCP
- Add admin examples

### Phase 4: Refactor Existing Code (Week 4)

- Migrate emergent-cli to use SDK
- Migrate test client to use SDK
- Update documentation
- Release v1.0.0

## Open Questions

1. **Versioning**: Should SDK version match server version, or independent?
   - **Recommendation**: Independent semantic versioning, document compatibility matrix
2. **Retry logic**: Should SDK handle retries automatically?
   - **Recommendation**: Yes, with configurable backoff for 5xx errors
3. **Rate limiting**: Should SDK handle rate limits?
   - **Recommendation**: No - let server return 429, SDK just surfaces error
4. **Telemetry**: Should SDK include optional telemetry (metrics, tracing)?
   - **Recommendation**: Phase 2 - add OpenTelemetry integration as opt-in
