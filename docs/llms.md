# Emergent SDK Reference for LLMs

Emergent is an AI memory and knowledge graph platform. It stores objects and relationships in a graph, chunks and embeds documents, and exposes a REST API for queries, agent orchestration, and LLM tracing.

There are two client SDKs:
- **Go SDK** — full-featured server-side client (`github.com/emergent-company/emergent/apps/server-go/pkg/sdk`)
- **Swift SDK** — lightweight Mac app client (`emergent-company/emergent.memory.mac`)

Full docs site: https://emergent-company.github.io/emergent/

---

## Go SDK

Full reference: [docs/llms-go-sdk.md](llms-go-sdk.md)

**Module:** `github.com/emergent-company/emergent/apps/server-go/pkg/sdk`

**Install:**
```bash
go get github.com/emergent-company/emergent/apps/server-go/pkg/sdk@latest
```

**Quick start:**
```go
client, err := sdk.New(sdk.Config{
    ServerURL: "https://your-server",
    Auth: sdk.AuthConfig{Mode: "apikey", APIKey: "emt_your_token"},
    OrgID:     "org-uuid",
    ProjectID: "project-uuid",
})
client.SetContext(orgID, projectID)  // update context at runtime
```

**30 service clients** on `*Client`:

Context-scoped (25): `Documents`, `Chunks`, `Search`, `Graph`, `Chat`, `Projects`, `Orgs`, `Users`, `APITokens`, `MCP`, `MCPRegistry`, `Branches`, `UserActivity`, `TypeRegistry`, `Notifications`, `Tasks`, `Monitoring`, `Agents`, `AgentDefinitions`, `DataSources`, `DiscoveryJobs`, `EmbeddingPolicy`, `Integrations`, `TemplatePacks`, `Chunking`

Non-context (4): `Health`, `Superadmin`, `APIDocs`, `Provider`

**Auth modes:** `apikey` (X-API-Key), `apitoken` (Bearer, for `emt_*` tokens — auto-detected), `oauth` (device flow via `NewWithDeviceFlow`)

**Error predicates:** `errors.IsNotFound`, `errors.IsForbidden`, `errors.IsUnauthorized`, `errors.IsBadRequest`, `errors.ParseErrorResponse`

**Dual-ID graph model:** Every graph object has `id` (version ID, changes on update) and `canonicalID` (entity ID, stable). Use `canonicalID` for persistent references. `graphutil.UniqueByEntity` deduplicates by canonical ID.

---

## Swift SDK

Full reference: [docs/llms-swift-sdk.md](llms-swift-sdk.md)

**Source files:**
- `Emergent/Core/EmergentAPIClient.swift`
- `Emergent/Core/Models.swift`

**Status:** Embedded in the Emergent Mac app. Standalone Swift Package planned (emergent-company/emergent#49).

**Quick start:**
```swift
EmergentAPIClient.shared.configure(
    serverURL: URL(string: "https://your-server")!,
    apiKey: "emt_your_token"
)
let projects = try await EmergentAPIClient.shared.fetchProjects()
```

**17 methods:** `configure`, `fetchProjects`, `fetchProjectStats`, `fetchAccountStats`, `fetchTraces`, `searchObjects`, `fetchObject`, `searchDocuments`, `executeQuery`, `fetchWorkers`, `fetchDiagnostics`, `fetchAgents`, `updateAgent`, `fetchEmbeddingStatus`, `fetchEmbeddingPolicies`, `fetchMCPServers`, `fetchUserProfile`

**Auth:** `emt_*` tokens → `Authorization: Bearer`; other keys → `X-API-Key`

**Error type:** `EmergentAPIError` — cases: `notConfigured`, `invalidURL`, `unauthorized`, `notFound`, `serverError(statusCode:message:)`, `httpError(statusCode:)`, `network(Error)`, `decodingFailed(Error)`

**Key model types:** `Project`, `ProjectStats`, `AccountStats`, `Trace`, `LLMCall`, `Worker`, `GraphObject`, `AnyCodable`, `Agent`, `MCPServer`, `UserProfile`, `Document`, `EmbeddingStatus`, `EmbeddingPolicy`, `QueryResult`, `ServerDiagnostics`
