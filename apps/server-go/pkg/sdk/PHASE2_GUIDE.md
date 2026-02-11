# SDK Phase 2 Implementation Guide

## Status: Phase 1 Complete ✅

**Completed:**

- ✅ Core SDK infrastructure
- ✅ API key authentication
- ✅ Documents, Chunks, Search service clients
- ✅ Error handling
- ✅ README and CHANGELOG

**Location:** `apps/server-go/pkg/sdk/`

## Phase 2 Requirements

### 1. OAuth Device Flow Authentication

**Files to create:**

- `pkg/sdk/auth/oauth.go` - OAuth provider implementation
- `pkg/sdk/auth/credentials.go` - Credential storage/loading
- `pkg/sdk/auth/oidc.go` - OIDC discovery

**Reference:** `tools/emergent-cli/internal/auth/` (device_flow.go, discovery.go, credentials.go)

**Key components:**

```go
type OAuthProvider struct {
    oidcConfig *OIDCConfig
    credentials *Credentials
    clientID string
    credsPath string
}

// Implements auth.Provider interface
func (p *OAuthProvider) Authenticate(req *http.Request) error
func (p *OAuthProvider) Refresh(ctx context.Context) error
```

### 2. Graph Service Client

**Files to create:**

- `pkg/sdk/graph/objects.go` - Graph objects (Create, Get, List, Update, Delete)
- `pkg/sdk/graph/relationships.go` - Relationships (Create, Get, List, Delete)
- `pkg/sdk/graph/search.go` - Graph search
- `pkg/sdk/graph/types.go` - DTOs

**OpenAPI paths:** `/api/graph/objects`, `/api/graph/relationships`, `/api/graph/search`

### 3. Chat Service with Streaming

**Files to create:**

- `pkg/sdk/chat/client.go` - Chat operations (List conversations, Send message)
- `pkg/sdk/chat/stream.go` - SSE streaming handler
- `pkg/sdk/chat/types.go` - Chat DTOs

**Key feature:** SSE event parsing for real-time streaming

**OpenAPI paths:** `/api/conversations`, `/api/conversations/{id}/messages`

### 4. Update Main SDK

**File to update:** `pkg/sdk/sdk.go`

```go
// Implement NewWithDeviceFlow
func NewWithDeviceFlow(cfg Config) (*Client, error) {
    oidcConfig, err := DiscoverOIDC(cfg.ServerURL)
    // ... device flow
    authProvider := NewOAuthProvider(oidcConfig, cfg.Auth.ClientID, cfg.Auth.CredsPath)
    // ... initialize client with OAuth
}

// Add Graph and Chat to Client struct
type Client struct {
    // ... existing fields
    Graph *graph.Client
    Chat  *chat.Client
}
```

### 5. Documentation Updates

**Files to update:**

- `pkg/sdk/README.md` - Add OAuth, Graph, Chat examples
- `pkg/sdk/CHANGELOG.md` - Mark Phase 2 complete

## Implementation Order

1. **OAuth first** (foundational for all authenticated calls)
   - Copy/adapt CLI auth package
   - Test device flow works
2. **Graph service** (simpler than streaming)
   - Objects CRUD
   - Relationships CRUD
   - Search
3. **Chat with streaming** (most complex)

   - Basic chat operations
   - SSE parsing
   - Stream lifecycle management

4. **Integration**
   - Update main Client
   - Add examples
   - Update docs

## Next Command

To continue implementation, run:

```bash
# Create OAuth provider
# Reference: tools/emergent-cli/internal/auth/
# Target: apps/server-go/pkg/sdk/auth/oauth.go

# Then Graph service
# Reference: apps/server-go/docs/swagger/swagger.json (graph endpoints)
# Target: apps/server-go/pkg/sdk/graph/

# Then Chat with streaming
# Reference: SSE implementation in server
# Target: apps/server-go/pkg/sdk/chat/
```

## Estimated Effort

- OAuth: 2-3 hours (adapt from CLI)
- Graph: 3-4 hours (3 sub-clients)
- Chat: 3-4 hours (SSE streaming complexity)
- Integration + docs: 1-2 hours

**Total:** ~10-13 hours for Phase 2 complete

## Success Criteria

- [ ] OAuth device flow functional
- [ ] Can create/retrieve graph objects and relationships
- [ ] Can perform graph search
- [ ] Chat streaming works end-to-end
- [ ] All packages build without errors
- [ ] README updated with Phase 2 examples
- [ ] CHANGELOG reflects Phase 2 completion
