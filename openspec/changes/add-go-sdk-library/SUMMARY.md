# Go SDK Library - Change Proposal Summary

**Change ID**: `add-go-sdk-library`  
**Status**: ✅ Validated (strict mode)  
**Scope**: Full API coverage with dual authentication support

## Overview

This proposal creates a comprehensive, production-ready Go SDK library for the Emergent API that can be imported and used by external Go applications.

## Key Features

### 1. Complete API Coverage

- **Documents** - Upload, CRUD, pagination
- **Chunks** - List with filters, search integration
- **Graph** - Objects, relationships, versioning, search
- **Search** - Unified search with fusion strategies, debug mode
- **Chat** - Conversations with SSE streaming support
- **Projects & Orgs** - Multi-tenant resource management
- **Users & API Tokens** - Identity and access management
- **Health & MCP** - System monitoring and protocol integration

### 2. Dual Authentication Support

**Standalone Mode (API Key)**

```go
client, _ := sdk.New(sdk.Config{
    ServerURL: "http://localhost:9090",
    Auth: sdk.AuthConfig{
        Mode:   "apikey",
        APIKey: "emt_abc123...",
    },
})
```

**Full Deployment (OAuth)**

```go
// First time: device flow
client, _ := sdk.NewWithDeviceFlow(sdk.Config{
    ServerURL: "https://api.emergent-company.ai",
    Auth: sdk.AuthConfig{
        Mode:      "oauth",
        ClientID:  "emergent-sdk",
        CredsPath: "~/.emergent/credentials.json",
    },
})
// Prints device code, polls until user authenticates

// Subsequent: auto-load credentials
client, _ := sdk.New(sdk.Config{...})
// Auto-refreshes tokens when expired
```

### 3. Type-Safe Operations

```go
// Documents
docs, err := client.Documents.List(ctx)
doc, err := client.Documents.Get(ctx, "doc_123")
id, err := client.Documents.Upload(ctx, file, metadata)

// Graph
obj, err := client.Graph.Objects.Create(ctx, object)
rel, err := client.Graph.Relationships.Create(ctx, fromID, toID, "relates_to")
results, err := client.Graph.Search.Search(ctx, query, sdk.WithDebug(true))

// Chat with streaming
stream, err := client.Chat.SendMessageStream(ctx, convID, message)
for event := range stream.Events() {
    fmt.Print(event.Token)
}
```

### 4. Multi-Tenant Context

```go
// Set defaults
client.SetContext("org_123", "proj_456")

// Override per request
docs, _ := client.Documents.List(ctx,
    sdk.WithOrg("org_789"),
    sdk.WithProject("proj_012"))
```

### 5. Testing Utilities

```go
// Mock server
mock := testutil.NewMockServer()
client := sdk.New(sdk.Config{ServerURL: mock.URL()})

// Fixtures
doc := testutil.ExampleDocument()
project := testutil.ExampleProject()
```

## Package Structure

```
apps/server-go/pkg/sdk/
├── sdk.go                # Main Client
├── auth/                 # Auth providers
├── documents/            # Documents service
├── chunks/               # Chunks service
├── graph/                # Graph services
├── search/               # Search service
├── chat/                 # Chat + streaming
├── projects/             # Projects service
├── orgs/                 # Organizations service
├── users/                # Users service
├── apitokens/            # API tokens service
├── health/               # Health checks
├── mcp/                  # MCP integration
├── errors/               # Error types
└── testutil/             # Testing utilities
```

## Implementation Plan

### Phase 1: Core SDK (Week 1)

- Auth providers (API key + OAuth with device flow)
- Documents, Chunks, Search clients
- Error handling, pagination
- Comprehensive tests

### Phase 2: Graph & Chat (Week 2)

- Graph objects, relationships, search
- Chat with SSE streaming
- Integration tests

### Phase 3: Management APIs (Week 3)

- Projects, Orgs, Users, API Tokens
- Health, MCP
- Admin examples

### Phase 4: Integration & Release (Week 4)

- Migrate emergent-cli to use SDK
- Migrate test client to use SDK
- Full documentation
- v1.0.0 release

## Files Created

```
openspec/changes/add-go-sdk-library/
├── proposal.md              ✅ Why, what, impact
├── design.md                ✅ Architecture, decisions, risks
├── tasks.md                 ✅ 23 sections, 170+ tasks
└── specs/
    └── go-sdk/
        └── spec.md          ✅ 16 requirements, 50+ scenarios
```

## Validation

```bash
$ npx openspec validate add-go-sdk-library --strict
Change 'add-go-sdk-library' is valid
```

✅ **All OpenSpec requirements met**

## Next Steps

1. **Review proposal** - Read design.md for architectural decisions
2. **Provide feedback** - Any requirements missing? Different approach needed?
3. **Approve** - Once approved, implementation can begin
4. **Track progress** - Use tasks.md as implementation checklist

## Key Decisions

1. **Manual implementation** (not OpenAPI codegen) - Better Go idioms, cleaner API
2. **Separate service clients** - `client.Documents.List()` pattern for organization
3. **Auth provider interface** - Clean separation, testable, extensible
4. **SDK-specific DTOs** - Independent from server internals
5. **Iterator pattern** - Memory-efficient pagination with `Next()` / `HasNext()`
6. **Context propagation** - All operations accept `context.Context`

## Benefits

- **External integrations** - Third parties can build on Emergent
- **Reduced duplication** - CLI and tests share SDK code
- **Type safety** - Compile-time validation of API usage
- **Better DX** - Comprehensive examples, godoc, quickstart
- **Testing support** - Mock server for offline development
- **Production-ready** - Error handling, retries, token refresh

## Questions?

- **Versioning**: Independent semantic versioning recommended
- **Retries**: Yes, with exponential backoff for 5xx errors
- **Rate limiting**: No automatic handling, surface 429 errors
- **Telemetry**: Phase 2 - OpenTelemetry as opt-in

---

**Import path**: `github.com/emergent-company/emergent/apps/server-go/pkg/sdk`  
**Target version**: v1.0.0  
**Estimated effort**: 4 weeks (170+ tasks across 23 sections)
