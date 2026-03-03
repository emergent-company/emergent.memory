# Models

All public types are defined in `Emergent/Core/Models.swift` in `emergent-company/emergent.memory.mac`.

---

## Project

Represents an Emergent project.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Project UUID |
| `name` | `String` | Display name |
| `description` | `String?` | Optional description |
| `orgID` | `String` | Parent organization UUID |
| `createdAt` | `Date?` | Creation timestamp |
| `updatedAt` | `Date?` | Last update timestamp |

---

## ProjectStats

Usage statistics for a project.

| Property | Type | Description |
|----------|------|-------------|
| `objectCount` | `Int` | Number of graph objects |
| `documentCount` | `Int` | Number of documents |
| `chunkCount` | `Int` | Number of text chunks |
| `embeddingCount` | `Int` | Number of embeddings |

---

## AccountStats

Account-level aggregated statistics.

| Property | Type | Description |
|----------|------|-------------|
| `projectCount` | `Int` | Total number of projects |
| `objectCount` | `Int` | Total graph objects across all projects |
| `documentCount` | `Int` | Total documents across all projects |

---

## Trace

A top-level LLM call trace record.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Trace UUID |
| `projectID` | `String` | Owning project UUID |
| `model` | `String` | LLM model identifier |
| `inputTokens` | `Int` | Tokens in the prompt |
| `outputTokens` | `Int` | Tokens in the completion |
| `createdAt` | `Date?` | Trace timestamp |
| `llmCalls` | `[LLMCall]?` | Nested LLM call details (when fetched with detail) |

---

## LLMCall

A single LLM call within a trace.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Call UUID |
| `model` | `String` | Model identifier |
| `prompt` | `String?` | Input prompt text |
| `completion` | `String?` | Output completion text |
| `inputTokens` | `Int` | Prompt token count |
| `outputTokens` | `Int` | Completion token count |
| `duration` | `Double?` | Latency in seconds |

---

## Worker

An embedding/processing worker.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Worker UUID |
| `status` | `WorkerStatus` | Current worker state |
| `currentJobID` | `String?` | ID of the job being processed, if any |
| `processedCount` | `Int` | Total jobs processed by this worker |

---

## WorkerStatus

```swift
enum WorkerStatus: String, Codable {
    case idle
    case processing
    case error
    case stopped
}
```

---

## GraphObject

A node in the Emergent knowledge graph.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Version ID (changes on update) |
| `canonicalID` | `String` | Stable entity ID |
| `type` | `String` | Object type name |
| `name` | `String?` | Display name |
| `properties` | `[String: AnyCodable]?` | Arbitrary key-value properties |
| `projectID` | `String` | Owning project UUID |
| `createdAt` | `Date?` | Creation timestamp |
| `updatedAt` | `Date?` | Last update timestamp |

!!! note "Dual-ID model"
    `id` is the *version ID* — it changes whenever the object is updated. `canonicalID` is the *entity ID* — it is stable for the lifetime of the object. Use `canonicalID` for persistent references (bookmarks, links), and `id` when you need to target a specific version.

---

## AnyCodable

A type-erased `Codable` wrapper used for arbitrary JSON values in `GraphObject.properties`.

```swift
struct AnyCodable: Codable {
    let value: Any
}
```

Supports encoding/decoding of `Bool`, `Int`, `Double`, `String`, `[Any]`, and `[String: Any]`.

---

## Agent

A background agent configuration.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Agent UUID |
| `name` | `String` | Display name |
| `projectID` | `String` | Owning project UUID |
| `enabled` | `Bool` | Whether the agent is active |
| `schedule` | `String?` | Cron expression or interval string |
| `config` | `[String: AnyCodable]?` | Agent-specific configuration |

---

## MCPServer

A registered MCP (Model Context Protocol) server.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Server UUID |
| `name` | `String` | Display name |
| `url` | `String` | Server endpoint URL |
| `projectID` | `String` | Owning project UUID |
| `tools` | `[MCPTool]?` | Available tools exposed by this server |

---

## MCPTool

A tool exposed by an MCP server.

| Property | Type | Description |
|----------|------|-------------|
| `name` | `String` | Tool name |
| `description` | `String?` | Human-readable description |
| `inputSchema` | `AnyCodable?` | JSON Schema for tool inputs |

---

## UserProfile

The authenticated user's profile.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | User UUID |
| `email` | `String` | Email address |
| `name` | `String?` | Display name |
| `avatarURL` | `String?` | Profile image URL |
| `role` | `String?` | Global role (e.g., `admin`, `user`) |

---

## Document

A document stored in Emergent.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Document UUID |
| `name` | `String` | File or display name |
| `projectID` | `String` | Owning project UUID |
| `mimeType` | `String?` | MIME type |
| `size` | `Int?` | File size in bytes |
| `createdAt` | `Date?` | Upload timestamp |

---

## ServerDiagnostics

Server health and version information.

| Property | Type | Description |
|----------|------|-------------|
| `version` | `String` | Server version string |
| `uptime` | `Double?` | Server uptime in seconds |
| `components` | `[String: String]?` | Component name → status map |

---

## EmbeddingStatus

Current state of the embedding pipeline.

| Property | Type | Description |
|----------|------|-------------|
| `queueDepth` | `Int` | Number of items waiting to be embedded |
| `workers` | `[EmbeddingWorkerState]` | Per-worker status |
| `config` | `EmbeddingConfig?` | Active embedding configuration |

---

## EmbeddingWorkerState

State of a single embedding worker.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Worker identifier |
| `status` | `String` | Status string (e.g., `idle`, `processing`) |
| `processedCount` | `Int` | Items processed since startup |

---

## EmbeddingConfig

Active embedding model configuration.

| Property | Type | Description |
|----------|------|-------------|
| `model` | `String` | Embedding model identifier |
| `dimensions` | `Int` | Vector dimensions |
| `provider` | `String?` | Provider name (e.g., `google`, `vertex`) |

---

## EmbeddingPolicy

An embedding policy for a project.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Policy UUID |
| `projectID` | `String` | Owning project UUID |
| `objectType` | `String` | Object type this policy applies to |
| `enabled` | `Bool` | Whether the policy is active |
| `fields` | `[String]?` | Fields to embed |

---

## QueryResult

The result of a graph query execution.

| Property | Type | Description |
|----------|------|-------------|
| `items` | `[QueryResultItem]` | Matched items |
| `meta` | `QueryResultMeta?` | Query metadata |

---

## QueryResultItem

A single item in a query result.

| Property | Type | Description |
|----------|------|-------------|
| `id` | `String` | Object ID |
| `type` | `String` | Object type |
| `score` | `Double?` | Relevance score (for semantic queries) |
| `properties` | `[String: AnyCodable]?` | Object properties |

---

## QueryResultMeta

Metadata about a query execution.

| Property | Type | Description |
|----------|------|-------------|
| `totalCount` | `Int?` | Total matching items (before limit) |
| `executionTimeMs` | `Double?` | Query execution time in milliseconds |
