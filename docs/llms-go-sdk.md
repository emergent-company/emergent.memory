# Emergent Go SDK — LLM Reference

## Module

```
github.com/emergent-company/emergent/apps/server-go/pkg/sdk
```

Install (supports both `@latest` and path-qualified version tags):

```bash
# Latest version
go get github.com/emergent-company/emergent/apps/server-go/pkg/sdk@latest

# Specific version (monorepo path-qualified tag)
go get github.com/emergent-company/emergent/apps/server-go/pkg/sdk@apps/server-go/pkg/sdk/v0.1.0
```

---

## Client struct

```go
type Client struct {
    // Context-scoped service clients (send X-Org-ID / X-Project-ID headers)
    Documents        *documents.Client
    Chunks           *chunks.Client
    Search           *search.Client
    Graph            *graph.Client
    Chat             *chat.Client
    Projects         *projects.Client
    Orgs             *orgs.Client
    Users            *users.Client
    APITokens        *apitokens.Client
    MCP              *mcp.Client
    MCPRegistry      *mcpregistry.Client
    Branches         *branches.Client
    UserActivity     *useractivity.Client
    TypeRegistry     *typeregistry.Client
    Notifications    *notifications.Client
    Tasks            *tasks.Client
    Monitoring       *monitoring.Client
    Agents           *agents.Client
    AgentDefinitions *agentdefinitions.Client
    DataSources      *datasources.Client
    DiscoveryJobs    *discoveryjobs.Client
    EmbeddingPolicy  *embeddingpolicies.Client
    Integrations     *integrations.Client
    TemplatePacks    *templatepacks.Client
    Chunking         *chunking.Client

    // Non-context service clients (no org/project headers)
    Health     *health.Client
    Superadmin *superadmin.Client
    APIDocs    *apidocs.Client
    Provider   *provider.Client
}
```

---

## Config and AuthConfig

```go
type Config struct {
    ServerURL  string       // Required. Base URL, no trailing slash.
    Auth       AuthConfig
    OrgID      string       // Optional default org ID
    ProjectID  string       // Optional default project ID
    HTTPClient *http.Client // Optional; defaults to 30s timeout
}

type AuthConfig struct {
    Mode      string // "apikey", "apitoken", or "oauth"
    APIKey    string // API key or emt_* token
    CredsPath string // OAuth credential file path
    ClientID  string // OAuth client ID
}
```

---

## Constructor functions

```go
func New(cfg Config) (*Client, error)
func NewWithDeviceFlow(cfg Config) (*Client, error)
```

---

## Authentication modes

### API key (X-API-Key header)
```go
client, err := sdk.New(sdk.Config{
    ServerURL: "https://your-server",
    Auth: sdk.AuthConfig{Mode: "apikey", APIKey: "your-key"},
})
```

### API token (emt_* prefix → Bearer header, auto-detected)
```go
client, err := sdk.New(sdk.Config{
    ServerURL: "https://your-server",
    Auth: sdk.AuthConfig{Mode: "apikey", APIKey: "emt_abc123..."},
    // auth.IsAPIToken detects emt_ prefix and switches to Bearer automatically
})
```

### OAuth device flow
```go
client, err := sdk.NewWithDeviceFlow(sdk.Config{
    ServerURL: "https://your-server",
    Auth: sdk.AuthConfig{
        Mode:      "oauth",
        ClientID:  "emergent-sdk",
        CredsPath: "~/.emergent/credentials.json",
    },
})
```

---

## SetContext

```go
func (c *Client) SetContext(orgID, projectID string)
```

Sets the default org and project for all context-scoped clients. Thread-safe. Must be called before using any context-scoped client if OrgID/ProjectID were not set in Config.

Note: `MCP.SetContext` takes only `projectID` (not orgID).

---

## Do and Close

```go
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error)
func (c *Client) Close()
```

`Do` applies auth headers, org/project context headers, and executes the request.
`Close` releases idle HTTP connections. Don't use the client after Close.

---

## errors package

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/errors"

type Error struct {
    StatusCode int                    `json:"status_code"`
    Code       string                 `json:"code"`
    Message    string                 `json:"message"`
    Details    map[string]interface{} `json:"details,omitempty"`
}

func IsNotFound(err error) bool
func IsForbidden(err error) bool
func IsUnauthorized(err error) bool
func IsBadRequest(err error) bool
func ParseErrorResponse(resp *http.Response) error
```

Usage:
```go
if errors.IsNotFound(err) { /* 404 */ }
if errors.IsUnauthorized(err) { /* 401 */ }
if errors.IsForbidden(err) { /* 403 */ }
if errors.IsBadRequest(err) { /* 400 */ }
```

---

## auth package

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/auth"

type Provider interface {
    Authenticate(req *http.Request) error
}

type Credentials struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresAt    time.Time `json:"expires_at"`
    UserEmail    string    `json:"user_email,omitempty"`
    IssuerURL    string    `json:"issuer_url,omitempty"`
}

func NewAPIKeyProvider(key string) Provider         // X-API-Key header
func NewAPITokenProvider(token string) Provider     // Authorization: Bearer header
func NewOAuthProvider(...) *OAuthProvider
func IsAPIToken(key string) bool                    // returns true if key has emt_ prefix
func LoadCredentials(path string) (*Credentials, error)
func SaveCredentials(path string, creds *Credentials) error
func DiscoverOIDC(serverURL string) (*OIDCConfig, error)
```

---

## graphutil package

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graphutil"

type IDSet          // set of object IDs; methods: Add, Has, Remove, Slice
type ObjectIndex    // map[canonicalID]GraphObject for deduplication
func UniqueByEntity(objects []GraphObject) []GraphObject
```

---

## testutil package

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/testutil"

type MockServer      // httptest.Server wrapper with assertion helpers
func AssertHeader(t *testing.T, r *http.Request, key, expected string)
func AssertMethod(t *testing.T, r *http.Request, expected string)
func AssertJSONBody(t *testing.T, r *http.Request, expected interface{})
func JSONResponse(t *testing.T, w http.ResponseWriter, data interface{})
```

---

## Per-client method reference

### Documents (`client.Documents`)
- `List(ctx, opts) ([]Document, error)` — list documents in project
- `Get(ctx, id) (*Document, error)` — get single document by ID
- `Upload(ctx, name, reader, opts) (*Document, error)` — upload file
- `Download(ctx, id) (io.ReadCloser, error)` — download document content
- `Delete(ctx, id) error` — delete document
- `BatchDelete(ctx, ids) error` — delete multiple documents

### Chunks (`client.Chunks`)
- `List(ctx, opts) ([]Chunk, error)` — list text chunks
- `Get(ctx, id) (*Chunk, error)` — get single chunk
- `Search(ctx, query, opts) ([]Chunk, error)` — search chunks by content
- `Delete(ctx, id) error` — delete a chunk

### Search (`client.Search`)
- `Search(ctx, req) (*SearchResponse, error)` — unified search (lexical/semantic/hybrid)

### Graph (`client.Graph`)
- `ListObjects(ctx, opts) ([]GraphObject, error)` — list graph objects
- `GetObject(ctx, id) (*GraphObject, error)` — fetch object by ID or canonical ID
- `CreateObject(ctx, req) (*GraphObject, error)` — create object
- `UpdateObject(ctx, id, req) (*GraphObject, error)` — update object (increments version ID)
- `DeleteObject(ctx, id) error` — delete object
- `ListRelationships(ctx, opts) ([]Relationship, error)` — list edges
- `CreateRelationship(ctx, req) (*Relationship, error)` — create edge
- `UpdateRelationship(ctx, id, req) (*Relationship, error)` — update edge
- `DeleteRelationship(ctx, id) error` — delete edge
- `Search(ctx, req) (*SearchResponse, error)` — search objects
- `Traverse(ctx, req) (*TraverseResponse, error)` — graph traversal
- `Analytics(ctx, req) (*AnalyticsResponse, error)` — graph analytics

### Chat (`client.Chat`)
- `ListConversations(ctx) ([]Conversation, error)` — list conversations
- `CreateConversation(ctx, req) (*Conversation, error)` — new conversation
- `GetConversation(ctx, id) (*Conversation, error)` — get conversation
- `DeleteConversation(ctx, id) error` — delete conversation
- `StreamChat(ctx, req) (*Stream, error)` — open SSE stream; iterate with `stream.Events()`

### Projects (`client.Projects`)
- `List(ctx) ([]Project, error)` — list all accessible projects
- `Get(ctx, id) (*Project, error)` — get project by ID
- `Create(ctx, req) (*Project, error)` — create project
- `Update(ctx, id, req) (*Project, error)` — update project
- `Delete(ctx, id) error` — delete project
- `ListMembers(ctx, id) ([]ProjectMember, error)` — list members
- `AddMember(ctx, id, req) (*ProjectMember, error)` — add member
- `RemoveMember(ctx, id, userID) error` — remove member

### Orgs (`client.Orgs`)
- `List(ctx) ([]Org, error)` — list organizations
- `Get(ctx, id) (*Org, error)` — get org
- `Create(ctx, req) (*Org, error)` — create org
- `Update(ctx, id, req) (*Org, error)` — update org
- `Delete(ctx, id) error` — delete org

### Users (`client.Users`)
- `GetMe(ctx) (*User, error)` — get current user profile
- `Update(ctx, id, req) (*User, error)` — update user

### APITokens (`client.APITokens`)
- `List(ctx) ([]APIToken, error)` — list API tokens
- `Create(ctx, req) (*APIToken, error)` — create token; returns secret once
- `Delete(ctx, id) error` — revoke token

### MCP (`client.MCP`)
- `SetContext(projectID string)` — set project context (only projectID, no orgID)
- `CallTool(ctx, req) (*CallToolResponse, error)` — invoke MCP tool via JSON-RPC

### MCPRegistry (`client.MCPRegistry`)
- `List(ctx) ([]MCPServer, error)` — list registered MCP servers
- `Get(ctx, id) (*MCPServer, error)` — get server by ID
- `Register(ctx, req) (*MCPServer, error)` — register new server
- `Deregister(ctx, id) error` — remove server

### Branches (`client.Branches`)
- `List(ctx) ([]Branch, error)` — list branches
- `Get(ctx, id) (*Branch, error)` — get branch
- `Create(ctx, req) (*Branch, error)` — create branch
- `Delete(ctx, id) error` — delete branch
- `Merge(ctx, id, req) (*Branch, error)` — merge branch into target

### UserActivity (`client.UserActivity`)
- `Record(ctx, req) error` — record a user activity event
- `GetRecent(ctx, limit) ([]Activity, error)` — recent activity
- `GetRecentByType(ctx, actType, limit) ([]Activity, error)` — filter by type
- `DeleteAll(ctx) error` — clear all activity
- `DeleteByResource(ctx, resourceID) error` — clear activity for a resource

### TypeRegistry (`client.TypeRegistry`)
- `List(ctx, projectID) ([]TypeDef, error)` — list type definitions
- `Get(ctx, projectID, name) (*TypeDef, error)` — get type by name
- `Create(ctx, projectID, req) (*TypeDef, error)` — create type
- `Update(ctx, projectID, name, req) (*TypeDef, error)` — update type
- `Delete(ctx, projectID, name) error` — delete type

### Notifications (`client.Notifications`)
- `GetStats(ctx) (*NotificationStats, error)` — counts by type
- `GetCounts(ctx) (*NotificationCounts, error)` — unread counts
- `List(ctx, opts) ([]Notification, error)` — list notifications
- `MarkRead(ctx, id) error` — mark single notification read
- `MarkAllRead(ctx) error` — mark all read
- `Dismiss(ctx, id) error` — dismiss notification

### Tasks (`client.Tasks`)
Review/resolution workflow (accept/reject items), not background jobs.
- `List(ctx, opts) ([]Task, error)` — list pending tasks
- `GetCounts(ctx) (*TaskCounts, error)` — count by status
- `ListAll(ctx, opts) ([]Task, error)` — list all tasks regardless of status
- `GetAllCounts(ctx) (*TaskCounts, error)` — counts across all statuses
- `GetByID(ctx, id) (*Task, error)` — get single task
- `Resolve(ctx, id, req) (*Task, error)` — accept or reject a task item
- `Cancel(ctx, id) error` — cancel a task

### Monitoring (`client.Monitoring`)
Tracks extraction jobs specifically.
- `ListExtractionJobs(ctx, opts) ([]ExtractionJob, error)` — list jobs
- `GetExtractionJobDetail(ctx, id) (*ExtractionJobDetail, error)` — full job detail
- `GetExtractionJobLogs(ctx, id) ([]LogEntry, error)` — job log lines
- `GetExtractionJobLLMCalls(ctx, id) ([]LLMCall, error)` — LLM calls within job

### Agents (`client.Agents`)
- `List(ctx) ([]Agent, error)` — list agents
- `Get(ctx, id) (*Agent, error)` — get agent
- `Create(ctx, req) (*Agent, error)` — create agent
- `Update(ctx, id, req) (*Agent, error)` — update agent
- `Delete(ctx, id) error` — delete agent
- `Run(ctx, id) (*AgentRun, error)` — trigger agent execution

### AgentDefinitions (`client.AgentDefinitions`)
- `List(ctx) ([]AgentDefinition, error)` — list definitions
- `Get(ctx, id) (*AgentDefinition, error)` — get by ID
- `Create(ctx, req) (*AgentDefinition, error)` — create
- `Update(ctx, id, req) (*AgentDefinition, error)` — update
- `Delete(ctx, id) error` — delete

### DataSources (`client.DataSources`)
- `List(ctx) ([]DataSource, error)` — list data sources
- `Get(ctx, id) (*DataSource, error)` — get by ID
- `Create(ctx, req) (*DataSource, error)` — create
- `Update(ctx, id, req) (*DataSource, error)` — update
- `Delete(ctx, id) error` — delete
- `TriggerSync(ctx, id) error` — trigger immediate sync

### DiscoveryJobs (`client.DiscoveryJobs`)
- `Create(ctx, req) (*DiscoveryJob, error)` — start type discovery
- `Get(ctx, id) (*DiscoveryJob, error)` — get job status
- `List(ctx, opts) ([]DiscoveryJob, error)` — list jobs
- `Cancel(ctx, id) error` — cancel running job
- `FinalizeDiscovery(ctx, id, req) (*DiscoveryJob, error)` — finalize results

### EmbeddingPolicy (`client.EmbeddingPolicy`)
- `List(ctx) ([]EmbeddingPolicy, error)` — list policies
- `GetByID(ctx, id) (*EmbeddingPolicy, error)` — get by ID
- `Create(ctx, req) (*EmbeddingPolicy, error)` — create policy
- `Update(ctx, id, req) (*EmbeddingPolicy, error)` — update policy
- `Delete(ctx, id) error` — delete policy

### Integrations (`client.Integrations`)
- `ListAvailable(ctx) ([]IntegrationSpec, error)` — list available integration types
- `List(ctx) ([]Integration, error)` — list configured integrations
- `Get(ctx, id) (*Integration, error)` — get by ID
- `GetPublic(ctx, id) (*Integration, error)` — get public info (no secrets)
- `Create(ctx, req) (*Integration, error)` — create
- `Update(ctx, id, req) (*Integration, error)` — update
- `Delete(ctx, id) error` — delete
- `TestConnection(ctx, id) (*TestResult, error)` — test connectivity
- `TriggerSync(ctx, id) error` — trigger sync

### TemplatePacks (`client.TemplatePacks`)
Project-scoped:
- `GetCompiledTypes(ctx) (*CompiledTypes, error)` — types compiled from assigned packs
- `GetAvailablePacks(ctx) ([]TemplatePack, error)` — packs available for assignment
- `GetInstalledPacks(ctx) ([]PackAssignment, error)` — assigned packs
- `AssignPack(ctx, req) (*PackAssignment, error)` — assign pack to project
- `UpdateAssignment(ctx, id, req) (*PackAssignment, error)` — update assignment options
- `DeleteAssignment(ctx, id) error` — remove pack assignment

Global pack CRUD:
- `CreatePack(ctx, req) (*TemplatePack, error)` — create new pack
- `GetPack(ctx, id) (*TemplatePack, error)` — get pack by ID
- `DeletePack(ctx, id) error` — delete pack

### Chunking (`client.Chunking`)
- `RechunkDocument(ctx, req) (*RechunkResult, error)` — re-chunk a document with current strategy

### Health (`client.Health`)
No auth required for health/ready/healthz endpoints.
- `Health(ctx) (*HealthResponse, error)` — basic health
- `APIHealth(ctx) (*HealthResponse, error)` — API-level health
- `Ready(ctx) (bool, error)` — readiness check (503 decoded normally, not an error)
- `IsReady(ctx) bool` — convenience wrapper for load balancers
- `Healthz(ctx) error` — Kubernetes-style liveness
- `Debug(ctx) (*DebugInfo, error)` — debug info (requires auth)
- `JobMetrics(ctx, jobType) (*JobMetricsResponse, error)` — job metrics (requires auth)
- `SchedulerStatus(ctx) (*SchedulerStatus, error)` — scheduler status (requires auth)

### Superadmin (`client.Superadmin`)
Requires superadmin role. Covers bulk admin operations.
- Users: `ListUsers`, `DeleteUser`, `GetUserStats`
- Organizations: `ListOrgs`, `DeleteOrg`
- Projects: `ListProjects`, `DeleteProject`
- Email jobs: `ListEmailJobs`, `DeleteEmailJob`, `RetryEmailJob`
- Embedding jobs: `ListEmbeddingJobs`, `CancelEmbeddingJob`, `RetryEmbeddingJob`
- Extraction jobs: `ListExtractionJobs`, `CancelExtractionJob`, `RetryExtractionJob`
- Document parsing jobs: `ListDocParsingJobs`, `CancelDocParsingJob`, `RetryDocParsingJob`
- Sync jobs: `ListSyncJobs`, `CancelSyncJob`, `RetrySyncJob`

### APIDocs (`client.APIDocs`)
Accesses built-in documentation endpoints (not OpenAPI/Swagger).
- `List(ctx) ([]DocEntry, error)` — list all docs entries (`GET /api/docs`)
- `Get(ctx, slug) (*DocEntry, error)` — get by slug (`GET /api/docs/:slug`)
- `ListCategories(ctx) ([]DocCategory, error)` — list categories (`GET /api/docs/categories`)

### Provider (`client.Provider`)
LLM credential management and model catalog.
- `GetGoogleAICredentials(ctx) (*GoogleAICreds, error)` — get Google AI credentials
- `SetGoogleAICredentials(ctx, req) error` — set Google AI API key
- `GetVertexAICredentials(ctx) (*VertexAICreds, error)` — get Vertex AI credentials
- `SetVertexAICredentials(ctx, req) error` — set Vertex AI credentials
- `ListModels(ctx) ([]Model, error)` — list available models in catalog
- `GetModel(ctx, id) (*Model, error)` — get model by ID
- `GetProjectPolicy(ctx, projectID) (*ProviderPolicy, error)` — get project's provider policy
- `SetProjectPolicy(ctx, projectID, req) error` — set project provider policy
- `GetUsage(ctx, req) (*UsageReport, error)` — usage report

---

## Dual-ID graph model

Every graph object has two IDs:

| Field | Also called | Stability | Use for |
|-------|-------------|-----------|---------|
| `ID` | `VersionID` | Changes on every update | Targeting a specific version |
| `CanonicalID` | `EntityID` | Stable for object's lifetime | Persistent references (links, bookmarks) |

Use `canonicalID` when storing references. Use `id` (version ID) only when you need to target an exact version. `graphutil.UniqueByEntity` deduplicates a slice by canonical ID, keeping the latest version.

---

## Docs site

Full reference and guides: https://emergent-company.github.io/emergent/
