# Emergent Swift SDK — LLM Reference

## Source

Repository: `emergent-company/emergent.memory.mac`

| File | Path |
|------|------|
| `EmergentAPIClient.swift` | `Emergent/Core/EmergentAPIClient.swift` |
| `Models.swift` | `Emergent/Core/Models.swift` |

Status: Embedded in the Mac app. A standalone `EmergentSwiftSDK` Swift Package is planned (see issue emergent-company/emergent#49) but not yet available.

---

## EmergentAPIClient

`@MainActor` `ObservableObject`. Single shared instance via `EmergentAPIClient.shared`.

### Authentication

`emt_*` tokens → `Authorization: Bearer <token>`
All other keys → `X-API-Key: <key>`

---

## Method signatures

```swift
// Configuration
func configure(serverURL: URL, apiKey: String)

// Projects
func fetchProjects() async throws -> [Project]
func fetchProjectStats(projectID: String) async throws -> ProjectStats
func fetchAccountStats() async throws -> AccountStats

// Traces
func fetchTraces(projectID: String, limit: Int = 50) async throws -> [Trace]

// Graph objects
func searchObjects(projectID: String, query: String, limit: Int = 20) async throws -> [GraphObject]
func fetchObject(id: String) async throws -> GraphObject

// Documents
func searchDocuments(projectID: String, query: String, limit: Int = 20) async throws -> [Document]
func executeQuery(projectID: String, query: String) async throws -> QueryResult

// Workers & diagnostics
func fetchWorkers() async throws -> [Worker]
func fetchDiagnostics() async throws -> ServerDiagnostics

// Agents
func fetchAgents(projectID: String) async throws -> [Agent]
func updateAgent(_ agent: Agent) async throws -> Agent

// Embedding
func fetchEmbeddingStatus() async throws -> EmbeddingStatus
func fetchEmbeddingPolicies(projectID: String) async throws -> [EmbeddingPolicy]

// MCP
func fetchMCPServers(projectID: String) async throws -> [MCPServer]

// User
func fetchUserProfile() async throws -> UserProfile
```

---

## API endpoints

| Method | Endpoint |
|--------|----------|
| `fetchProjects()` | `GET /api/projects` |
| `fetchProjectStats(projectID:)` | `GET /api/projects/{id}/stats` |
| `fetchAccountStats()` | `GET /api/account/stats` |
| `fetchTraces(projectID:limit:)` | `GET /api/orgs/{orgID}/projects/{projectID}/traces?limit={n}` |
| `searchObjects(projectID:query:limit:)` | `POST /api/orgs/{orgID}/projects/{projectID}/graph/search` |
| `fetchObject(id:)` | `GET /api/orgs/{orgID}/projects/{projectID}/graph/objects/{id}` |
| `searchDocuments(projectID:query:limit:)` | `POST /api/orgs/{orgID}/projects/{projectID}/documents/search` |
| `executeQuery(projectID:query:)` | `POST /api/orgs/{orgID}/projects/{projectID}/graph/query` |
| `fetchWorkers()` | `GET /api/workers` |
| `fetchDiagnostics()` | `GET /api/diagnostics` |
| `fetchAgents(projectID:)` | `GET /api/orgs/{orgID}/projects/{projectID}/agents` |
| `updateAgent(_:)` | `PUT /api/orgs/{orgID}/projects/{projectID}/agents/{id}` |
| `fetchEmbeddingStatus()` | `GET /api/embedding/status` |
| `fetchEmbeddingPolicies(projectID:)` | `GET /api/orgs/{orgID}/projects/{projectID}/embedding/policies` |
| `fetchMCPServers(projectID:)` | `GET /api/orgs/{orgID}/projects/{projectID}/mcp/servers` |
| `fetchUserProfile()` | `GET /api/users/me` |

---

## Model types

### Project
`id: String`, `name: String`, `description: String?`, `orgID: String`, `createdAt: Date?`, `updatedAt: Date?`

### ProjectStats
`objectCount: Int`, `documentCount: Int`, `chunkCount: Int`, `embeddingCount: Int`

### AccountStats
`projectCount: Int`, `objectCount: Int`, `documentCount: Int`

### Trace
`id: String`, `projectID: String`, `model: String`, `inputTokens: Int`, `outputTokens: Int`, `createdAt: Date?`, `llmCalls: [LLMCall]?`

### LLMCall
`id: String`, `model: String`, `prompt: String?`, `completion: String?`, `inputTokens: Int`, `outputTokens: Int`, `duration: Double?`

### Worker
`id: String`, `status: WorkerStatus`, `currentJobID: String?`, `processedCount: Int`

### WorkerStatus (enum)
`.idle`, `.processing`, `.error`, `.stopped`

### GraphObject
`id: String` (version ID, changes on update), `canonicalID: String` (stable entity ID), `type: String`, `name: String?`, `properties: [String: AnyCodable]?`, `projectID: String`, `createdAt: Date?`, `updatedAt: Date?`

### AnyCodable
Type-erased `Codable` wrapper for arbitrary JSON. Wraps `Bool`, `Int`, `Double`, `String`, `[Any]`, `[String: Any]`.

### Agent
`id: String`, `name: String`, `projectID: String`, `enabled: Bool`, `schedule: String?`, `config: [String: AnyCodable]?`

### MCPServer
`id: String`, `name: String`, `url: String`, `projectID: String`, `tools: [MCPTool]?`

### MCPTool
`name: String`, `description: String?`, `inputSchema: AnyCodable?`

### UserProfile
`id: String`, `email: String`, `name: String?`, `avatarURL: String?`, `role: String?`

### Document
`id: String`, `name: String`, `projectID: String`, `mimeType: String?`, `size: Int?`, `createdAt: Date?`

### ServerDiagnostics
`version: String`, `uptime: Double?`, `components: [String: String]?`

### EmbeddingStatus
`queueDepth: Int`, `workers: [EmbeddingWorkerState]`, `config: EmbeddingConfig?`

### EmbeddingWorkerState
`id: String`, `status: String`, `processedCount: Int`

### EmbeddingConfig
`model: String`, `dimensions: Int`, `provider: String?`

### EmbeddingPolicy
`id: String`, `projectID: String`, `objectType: String`, `enabled: Bool`, `fields: [String]?`

### QueryResult
`items: [QueryResultItem]`, `meta: QueryResultMeta?`

### QueryResultItem
`id: String`, `type: String`, `score: Double?`, `properties: [String: AnyCodable]?`

### QueryResultMeta
`totalCount: Int?`, `executionTimeMs: Double?`

---

## EmergentAPIError

```swift
enum EmergentAPIError: Error, LocalizedError {
    case notConfigured                              // configure() not yet called
    case invalidURL                                 // malformed URL
    case unauthorized                               // HTTP 401
    case notFound                                   // HTTP 404
    case serverError(statusCode: Int, message: String)  // HTTP 5xx
    case httpError(statusCode: Int)                 // other HTTP error
    case network(Error)                             // URLSession error
    case decodingFailed(Error)                      // JSON decode failure
}
```

---

## ConnectionState

```swift
enum ConnectionState: Equatable {
    case unknown        // initial, not yet determined
    case connected      // reachable, health check OK
    case disconnected   // unreachable
    case error(String)  // error with human-readable message
}
```

---

## Docs site

Full reference: https://emergent-company.github.io/emergent/swift-sdk/
