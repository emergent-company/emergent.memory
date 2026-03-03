# API Client

`EmergentAPIClient` is a `@MainActor`-isolated `ObservableObject` that serves as the single entry point to the Emergent REST API from Swift.

**Source:** `Emergent/Core/EmergentAPIClient.swift` in `emergent-company/emergent.memory.mac`

## Authentication

The client auto-detects the token type:

| Key format | Header sent |
|------------|-------------|
| `emt_*` prefix | `Authorization: Bearer <key>` |
| Any other value | `X-API-Key: <key>` |

---

## Configuration

### `configure(serverURL:apiKey:)`

```swift
func configure(serverURL: URL, apiKey: String)
```

Stores the base URL and API key. Must be called before any other method. Typically called once at app launch.

| Parameter | Type | Description |
|-----------|------|-------------|
| `serverURL` | `URL` | Base URL of the Emergent server (no trailing path) |
| `apiKey` | `String` | API key or `emt_*` token |

---

## Projects

### `fetchProjects()`

```swift
func fetchProjects() async throws -> [Project]
```

Lists all projects accessible to the authenticated user.

- **Endpoint:** `GET /api/projects`
- **Returns:** Array of `Project`

### `fetchProjectStats(projectID:)`

```swift
func fetchProjectStats(projectID: String) async throws -> ProjectStats
```

Returns usage statistics for a single project.

- **Endpoint:** `GET /api/projects/{projectID}/stats`
- **Returns:** `ProjectStats`

| Parameter | Type | Description |
|-----------|------|-------------|
| `projectID` | `String` | Project UUID |

---

## Traces

### `fetchTraces(projectID:limit:)`

```swift
func fetchTraces(projectID: String, limit: Int = 50) async throws -> [Trace]
```

Returns recent LLM call traces for a project.

- **Endpoint:** `GET /api/orgs/{orgID}/projects/{projectID}/traces?limit={limit}`
- **Returns:** Array of `Trace`

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `projectID` | `String` | — | Project UUID |
| `limit` | `Int` | `50` | Maximum number of traces to return |

---

## Graph Objects

### `searchObjects(projectID:query:limit:)`

```swift
func searchObjects(projectID: String, query: String, limit: Int = 20) async throws -> [GraphObject]
```

Performs a semantic or lexical search over graph objects in a project.

- **Endpoint:** `POST /api/orgs/{orgID}/projects/{projectID}/graph/search`
- **Returns:** Array of `GraphObject`

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `projectID` | `String` | — | Project UUID |
| `query` | `String` | — | Search query string |
| `limit` | `Int` | `20` | Maximum results |

### `fetchObject(id:)`

```swift
func fetchObject(id: String) async throws -> GraphObject
```

Fetches a single graph object by its ID.

- **Endpoint:** `GET /api/orgs/{orgID}/projects/{projectID}/graph/objects/{id}`
- **Returns:** `GraphObject`

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | `String` | Graph object ID (canonical or version ID) |

---

## Documents

### `searchDocuments(projectID:query:limit:)`

```swift
func searchDocuments(projectID: String, query: String, limit: Int = 20) async throws -> [Document]
```

Searches documents in a project by content or metadata.

- **Endpoint:** `POST /api/orgs/{orgID}/projects/{projectID}/documents/search`
- **Returns:** Array of `Document`

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `projectID` | `String` | — | Project UUID |
| `query` | `String` | — | Search query string |
| `limit` | `Int` | `20` | Maximum results |

### `executeQuery(projectID:query:)`

```swift
func executeQuery(projectID: String, query: String) async throws -> QueryResult
```

Executes a structured graph query against a project.

- **Endpoint:** `POST /api/orgs/{orgID}/projects/{projectID}/graph/query`
- **Returns:** `QueryResult`

| Parameter | Type | Description |
|-----------|------|-------------|
| `projectID` | `String` | Project UUID |
| `query` | `String` | Query expression |

---

## Workers & Diagnostics

### `fetchWorkers()`

```swift
func fetchWorkers() async throws -> [Worker]
```

Returns the current state of all embedding/processing workers.

- **Endpoint:** `GET /api/workers`
- **Returns:** Array of `Worker`

### `fetchDiagnostics()`

```swift
func fetchDiagnostics() async throws -> ServerDiagnostics
```

Returns server diagnostics including version info and component health.

- **Endpoint:** `GET /api/diagnostics`
- **Returns:** `ServerDiagnostics`

---

## Agents

### `fetchAgents(projectID:)`

```swift
func fetchAgents(projectID: String) async throws -> [Agent]
```

Lists all agents configured for a project.

- **Endpoint:** `GET /api/orgs/{orgID}/projects/{projectID}/agents`
- **Returns:** Array of `Agent`

| Parameter | Type | Description |
|-----------|------|-------------|
| `projectID` | `String` | Project UUID |

### `updateAgent(_:)`

```swift
func updateAgent(_ agent: Agent) async throws -> Agent
```

Updates an existing agent's configuration.

- **Endpoint:** `PUT /api/orgs/{orgID}/projects/{projectID}/agents/{agentID}`
- **Returns:** Updated `Agent`

| Parameter | Type | Description |
|-----------|------|-------------|
| `agent` | `Agent` | Agent with updated fields; must contain a valid `id` |

---

## Embedding

### `fetchEmbeddingStatus()`

```swift
func fetchEmbeddingStatus() async throws -> EmbeddingStatus
```

Returns the current embedding pipeline status (queue depth, worker states).

- **Endpoint:** `GET /api/embedding/status`
- **Returns:** `EmbeddingStatus`

### `fetchEmbeddingPolicies(projectID:)`

```swift
func fetchEmbeddingPolicies(projectID: String) async throws -> [EmbeddingPolicy]
```

Lists all embedding policies configured for a project.

- **Endpoint:** `GET /api/orgs/{orgID}/projects/{projectID}/embedding/policies`
- **Returns:** Array of `EmbeddingPolicy`

| Parameter | Type | Description |
|-----------|------|-------------|
| `projectID` | `String` | Project UUID |

---

## MCP Servers

### `fetchMCPServers(projectID:)`

```swift
func fetchMCPServers(projectID: String) async throws -> [MCPServer]
```

Lists all registered MCP servers for a project.

- **Endpoint:** `GET /api/orgs/{orgID}/projects/{projectID}/mcp/servers`
- **Returns:** Array of `MCPServer`

| Parameter | Type | Description |
|-----------|------|-------------|
| `projectID` | `String` | Project UUID |

---

## User

### `fetchUserProfile()`

```swift
func fetchUserProfile() async throws -> UserProfile
```

Returns the profile of the currently authenticated user.

- **Endpoint:** `GET /api/users/me`
- **Returns:** `UserProfile`

### `fetchAccountStats()`

```swift
func fetchAccountStats() async throws -> AccountStats
```

Returns account-level statistics (total objects, documents, projects, etc.).

- **Endpoint:** `GET /api/account/stats`
- **Returns:** `AccountStats`

---

## Error handling

All methods throw `EmergentAPIError`. See [Errors](errors.md) for the full enum and handling patterns.

```swift
do {
    let projects = try await EmergentAPIClient.shared.fetchProjects()
} catch EmergentAPIError.notConfigured {
    // call configure(serverURL:apiKey:) first
} catch EmergentAPIError.unauthorized {
    // invalid or expired API key
} catch EmergentAPIError.serverError(let code, let message) {
    print("Server error \(code): \(message)")
} catch {
    // network or decoding error
}
```
