# Swift SDK

The Emergent Swift layer is a lightweight, opinionated HTTP client built into the **Emergent Mac app** (`emergent-company/emergent.memory.mac`). It is not yet distributed as a standalone Swift Package — that work is tracked in [emergent-company/emergent#49](https://github.com/emergent-company/emergent/issues/49).

## Source files

| File | Location |
|------|----------|
| `EmergentAPIClient.swift` | `Emergent/Core/EmergentAPIClient.swift` |
| `Models.swift` | `Emergent/Core/Models.swift` |

Both files live in the Mac app repo: **`emergent-company/emergent.memory.mac`**.

## What it covers

`EmergentAPIClient` is a `@MainActor`-isolated `ObservableObject` that wraps the Emergent REST API for use inside SwiftUI. It handles:

- **Configuration** — server URL + API key stored once, shared globally
- **Authentication** — auto-detects `emt_*` tokens (Bearer) vs plain API keys (`X-API-Key`)
- **Projects & stats** — list projects, project stats, account-level stats
- **Traces & objects** — fetch traces, search and fetch graph objects
- **Documents** — search documents, execute graph queries
- **Workers & diagnostics** — agent worker pool status, server diagnostics
- **Agents** — list and update agents per project
- **Embedding** — embedding status, embedding policies per project
- **MCP servers** — list MCP servers per project
- **User profile** — fetch the current authenticated user

## Status

| Layer | Status |
|-------|--------|
| `EmergentAPIClient` (Mac app) | Stable, in production |
| `EmergentSwiftSDK/` (Swift Package) | Planned stub — not yet implemented |

See the [Roadmap](roadmap.md) for the planned Swift Package design.

## Quick start

```swift
// Configure once at app launch
EmergentAPIClient.shared.configure(
    serverURL: URL(string: "https://your-server")!,
    apiKey: "emt_your_api_key"
)

// Fetch projects (async, from any async context)
let projects = try await EmergentAPIClient.shared.fetchProjects()
```

## Pages in this section

- [API Client](api-client.md) — all 17 methods with signatures and endpoints
- [Models](models.md) — all public types with property tables
- [Errors](errors.md) — `EmergentAPIError` enum and `ConnectionState`
- [Roadmap](roadmap.md) — planned `EmergentSwiftSDK` Swift Package design
