## ADDED Requirements

### Requirement: Swift SDK site section exists
The documentation site SHALL contain a dedicated Swift SDK section documenting the `EmergentAPIClient` class, all 19 public model types from `Models.swift`, error types, and connection state types from the Mac app's `Emergent/Core/` layer.

#### Scenario: EmergentAPIClient methods are all documented
- **WHEN** a developer navigates to the Swift SDK API reference
- **THEN** they find documentation for all 14 public methods on `EmergentAPIClient`: `configure(serverURL:apiKey:)`, `fetchProjects()`, `fetchTraces(projectID:)`, `searchObjects(projectID:query:limit:)`, `fetchObject(projectID:objectID:)`, `searchDocuments(projectID:query:)`, `executeQuery(projectID:query:limit:)`, `fetchWorkers()`, `fetchDiagnostics()`, `fetchAgents()`, `updateAgent(_:)`, `fetchMCPServers()`, `fetchUserProfile()`, `fetchAccountStats()`
- **AND** each method entry shows its signature, parameters, return type, and a brief description of the endpoint it calls

#### Scenario: All 19 public model types are documented
- **WHEN** a developer navigates to the Swift SDK models reference
- **THEN** they find a documented entry for each public struct and enum: `Project`, `ProjectStats`, `AccountStats`, `Trace`, `TraceDetail`, `LLMCall`, `Worker`, `WorkerStatus`, `GraphObject`, `AnyCodable`, `Agent`, `MCPServer`, `MCPTool`, `UserProfile`, `Document`, `ServerDiagnostics`, `QueryResult`, `QueryResultItem`, `QueryResultMeta`
- **AND** each entry lists all properties with their Swift types

#### Scenario: EmergentAPIError cases are documented
- **WHEN** a developer reads the Swift SDK error reference
- **THEN** they find all error cases documented: `notConfigured`, `invalidURL`, `unauthorized`, `notFound`, `serverError(statusCode:message:)`, `httpError(statusCode:)`, `network(Error)`, `decodingFailed(Error)`

### Requirement: Swift SDK forward reference to formal SDK is included
The documentation site SHALL include a note explaining that `EmergentSwiftSDK/` is a planned formal public Swift package (CGO bridge to `libemergent.a`) that is not yet implemented, with a reference to the upstream dependency.

#### Scenario: Developer understands current vs planned Swift SDK status
- **WHEN** a developer reads the Swift SDK section introduction
- **THEN** they find a clearly labelled note stating the current documentation covers the internal `Emergent/Core/` API layer used by the Mac app
- **AND** the note explains that a formal `EmergentSwiftSDK` Swift Package (CGO bridge) is planned pending `emergent-company/emergent#49`
- **AND** the note clarifies that `EmergentSwiftSDK/` in the Mac app repo is currently an empty stub

### Requirement: Swift SDK source locations are referenced
The documentation site SHALL reference exact source file locations for the Swift SDK types so developers can navigate to the implementation.

#### Scenario: Source file paths are visible in the docs
- **WHEN** a developer reads a Swift SDK reference page
- **THEN** they see the source file path (`Emergent/Core/EmergentAPIClient.swift` or `Emergent/Core/Models.swift`) with the associated repository (`emergent-company/emergent.memory.mac`)
