# Swift SDK

`EmergentKit` is the Swift client for the Memory platform. It ships as a Swift package in
[`swift/EmergentKit`](https://github.com/emergent-company/emergent.memory/tree/main/swift/EmergentKit)
and reaches the server through a Go C-bridge compiled into an XCFramework, so the request path is
the same Go client the server and the other SDKs use.

!!! note "Not published as a standalone package yet"
    There is no remote Swift Package URL and no released XCFramework artifact, so `EmergentKit` is
    currently consumed from inside this repository: you build the bridge, then add the package by
    local path. See [Installation](#installation).

## Requirements

| Requirement | Version |
|---|---|
| Platforms | iOS 15+, macOS 12+ |
| Swift | 5.9 toolchain or newer; the target builds with Swift 6 strict concurrency enabled |
| Go bridge | `EmergentGoCore.xcframework`, built from the server's `cmd/swiftbridge` package |
| Build host | macOS with Xcode and Go 1.24+ (the bridge is built with `CGO_ENABLED=1`) |

## Installation

Two steps: build the Go bridge into an XCFramework, then add the package as a local dependency.

```bash
# 1. Build the bridge (macOS host: needs Xcode and Go 1.24+)
task server:swift:xcframework
# ...or run the script directly
./apps/server/scripts/build-xcframework.sh

# 2. Package a release archive + checksum, for a remote binary target later
./apps/server/scripts/release-xcframework.sh --version 0.8.0
```

`task server:swift:xcframework` writes `apps/server/dist/swift/EmergentGoCore.xcframework` with
three slices — iOS device, iOS Simulator (arm64 + x86_64) and macOS (arm64 + x86_64) — each
carrying the Go-generated header and a `module.modulemap`. `Package.swift` declares that binary
target by local path and is the only place the bridge is referenced; a published package swaps it
for the `url:`/`checksum:` form that `release-xcframework.sh` prints.

Add the package to your app:

```swift
dependencies: [
    .package(path: "swift/EmergentKit"),
],
targets: [
    .target(name: "MyApp", dependencies: ["EmergentKit"]),
]
```

## Quick start

```swift
import EmergentKit

// Optional: route the Go runtime's log output into Apple's unified logging.
EmergentLogging.configure()

let client = try await EmergentClient(config: EmergentConfig(
    serverURL: "https://memory.emergent-company.ai",
    apiKey: "emt_your_key_here",
    orgID: "org_abc",
    projectID: "proj_xyz"
))

let health = try await client.health()
print(health.status)          // "healthy", "ok", …

let results = try await client.search(SearchRequest(query: "machine learning", limit: 5))
for hit in results.results {
    print(hit.type, hit.score, hit.content ?? "")
}
```

`EmergentClient` is an `actor`, so it is safe to share across tasks and awaits; you do not need to
serialise calls yourself.

## Configuration

| Field | Type | Default | Meaning |
|---|---|---|---|
| `serverURL` | `String` | — | Full URL of the Memory server, e.g. `https://memory.emergent-company.ai` |
| `authMode` | `String` | `"apikey"` | Authentication mode: `"apikey"` or `"apitoken"` |
| `apiKey` | `String` | — | The API key, or the `emt_*` API token for `authMode: "apitoken"` |
| `orgID` | `String?` | `nil` | Default organisation context |
| `projectID` | `String?` | `nil` | Default project context |

Context can also be set (or changed) after construction:

```swift
try await client.setContext(orgID: "org_abc", projectID: "proj_xyz")
```

## Operations

| Method | Returns | Notes |
|---|---|---|
| `setContext(orgID:projectID:)` | — | Sets the default org/project for later calls; throws if the server rejects them |
| `health()` | `HealthResponse` | Server status, uptime, version |
| `search(_:)` | `SearchResponse` | Hybrid semantic search over the graph and documents |
| `chat(_:)` | `ChatResponse` | Calls the streaming endpoint and collects the tokens into one reply |
| `listDocuments(_:)` | `ListDocumentsResponse` | Cursor-paginated document listing for the current project |

### Search

```swift
let request = SearchRequest(
    query: "database architecture decisions",
    limit: 20,                       // default 10
    resultTypes: "both",             // "graph", "text", or "both"
    fusionStrategy: "rrf"            // "weighted", "rrf", "interleave", …
)
let response = try await client.search(request)
```

Each `SearchResult` carries `type`, `score`, `rank` and whichever of `objectID`, `objectType`, `key`,
`documentID`, `chunkID`, `content` applies to that hit; `response.metadata` reports `total` and
`elapsedMs`.

### Chat

```swift
let first = try await client.chat(ChatRequest(message: "Summarise the last sync failures"))
print(first.content)

// Continue the same conversation by passing its id back.
let next = try await client.chat(ChatRequest(message: "Why did the third one fail?",
                                             conversationID: first.conversationID))
```

`chat` returns the **complete** answer: it consumes the server's stream internally and resumes once
the response is finished. There is no token-by-token callback in this version.

### Documents

```swift
var page = try await client.listDocuments(ListDocumentsRequest(limit: 50))
var all = page.documents

while let cursor = page.nextCursor {
    page = try await client.listDocuments(ListDocumentsRequest(limit: 50, cursor: cursor))
    all.append(contentsOf: page.documents)
}
```

## Error handling

Every failure is an `EmergentError`, mapped from the bridge's message:

| Case | Raised for |
|---|---|
| `.unauthorized(String)` | 401 / 403 |
| `.networkFailure(String)` | timeouts, DNS and connection failures |
| `.invalidRequest(String)` | request rejected client-side or with 400 |
| `.notFound(String)` | 404 |
| `.cancelled` | the task was cancelled |
| `.serverError(String)` | 5xx from the server |
| `.internal(String)` | a fault inside the SDK itself |
| `.unknown(String)` | a bridge error that maps to none of the above |

`EmergentError` is a `LocalizedError`, so `error.localizedDescription` is safe to show:

```swift
do {
    _ = try await client.search(SearchRequest(query: "…"))
} catch let error as EmergentError {
    switch error {
    case .unauthorized:   // re-authenticate
    case .notFound:       // nothing to show
    case .cancelled:      // task went away, not a failure
    default:              present(error.localizedDescription)
    }
}
```

## Logging

```swift
EmergentLogging.configure()
```

Call it once at startup, **before** creating a client. Go-side messages then appear in Xcode's
console and the system log under subsystem `ai.emergent.sdk`, category `GoRuntime`, at the level
the bridge reported.

## Threading

`EmergentClient` is an `actor`, and all request state stays inside it: calls are serialised per
client instance and results are safe to hand to `@MainActor` code. Creating a client is `async` (it
builds the Go-side handle), so do it once — for example in an app-level service — rather than per
view.

## Testing

Code that takes `EmergentService` (the protocol `EmergentClient` conforms to) can be tested without
the server, the XCFramework, or the client at all:

```swift
struct StubService: EmergentService {
    func setContext(orgID: String, projectID: String) async throws {}
    func health() async throws -> HealthResponse { HealthResponse(status: "healthy") }
    func search(_ request: SearchRequest) async throws -> SearchResponse {
        SearchResponse(results: [], metadata: nil)
    }
    func chat(_ request: ChatRequest) async throws -> ChatResponse {
        ChatResponse(conversationID: "conv_1", content: "…")
    }
    func listDocuments(_ request: ListDocumentsRequest) async throws -> ListDocumentsResponse {
        ListDocumentsResponse(documents: [], total: 0, nextCursor: nil)
    }
}
```

The package's own unit tests go a step further with an internal mock bridge, which exercises JSON
decoding and error mapping without a running server. Run them from the repository root — the task
builds the XCFramework first, since the test targets link against it:

```bash
task server:swift:test
```

The separate `EmergentKitIntegrationTests` target exercises the real Go → C → Swift path and skips
itself unless `EMERGENT_TEST_API_KEY` is set.

## Scope

The Swift surface is **read-only plus chat**: it lists documents, searches, and talks to the server,
but it does not create or update graph objects, upload documents, or manage agents. For write
operations use the [Go SDK](go-sdk/index.md), the [Python SDK](python-sdk.md), the
[CLI](cli.md), or the API directly.
