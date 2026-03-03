## ADDED Requirements

### Requirement: Go SDK site section exists
The documentation site SHALL contain a dedicated Go SDK section covering all 30 service clients, the auth package, the errors package, and supporting sub-packages (graphutil, testutil).

#### Scenario: All 30 service clients have reference pages
- **WHEN** a developer navigates to the Go SDK section of the documentation site
- **THEN** they find one dedicated markdown page per service client (graph, documents, search, chat, chunks, chunking, agents, agentdefinitions, branches, projects, orgs, users, apitokens, mcp, mcpregistry, datasources, discoveryjobs, embeddingpolicies, integrations, templatepacks, typeregistry, notifications, tasks, monitoring, useractivity, superadmin, health, apidocs, provider)
- **AND** each page lists all exported request/response types and method signatures for that client

#### Scenario: Auth package is documented
- **WHEN** a developer navigates to the Go SDK auth reference
- **THEN** they find documentation for `auth.Provider` interface, `APIKeyProvider`, `APITokenProvider`, `OAuthProvider`, `Credentials`, `LoadCredentials`, `SaveCredentials`, `DiscoverOIDC`, and `IsAPIToken`

#### Scenario: Errors package is documented
- **WHEN** a developer navigates to the Go SDK errors reference
- **THEN** they find documentation for the `Error` type and all predicate functions: `IsNotFound`, `IsForbidden`, `IsUnauthorized`, `IsBadRequest`, `ParseErrorResponse`

#### Scenario: Graph utility sub-package is documented
- **WHEN** a developer navigates to the graph/graphutil reference
- **THEN** they find documentation for `IDSet`, `ObjectIndex`, and `UniqueByEntity` with usage context for the dual-ID model

### Requirement: Go SDK narrative guides are included
The documentation site SHALL include narrative guides that explain how to use the SDK for common workflows, not just API reference.

#### Scenario: Quickstart guide exists
- **WHEN** a developer opens the Go SDK documentation for the first time
- **THEN** they find a Quickstart page that shows: installation via `go get`, client initialization with API key authentication, and a complete working example (list documents or check health) in under 20 lines of code

#### Scenario: Authentication guide covers all three modes
- **WHEN** a developer reads the authentication guide
- **THEN** they find coverage of API key mode (`X-API-Key` header), API token mode (`emt_*` prefix bearer token, auto-detected), and OAuth device flow (interactive, stores credentials, auto-refresh)
- **AND** each mode includes a complete code snippet showing `sdk.Config` setup

#### Scenario: Multi-tenancy context guide exists
- **WHEN** a developer reads the multi-tenancy guide
- **THEN** they find an explanation of `SetContext(orgID, projectID)` and which of the 30 clients are context-scoped (26) vs non-context (4: Health, Superadmin, APIDocs, Provider)

#### Scenario: Error handling guide exists
- **WHEN** a developer reads the error handling guide
- **THEN** they find the structured error type documented with all five predicate functions and an example using `if sdkerrors.IsNotFound(err)` branching

#### Scenario: Streaming/SSE guide exists
- **WHEN** a developer reads the streaming guide
- **THEN** they find a complete example of using `client.Chat.StreamChat` with `stream.Events()`, handling `token`, `done`, and `error` event types, and calling `stream.Close()`

#### Scenario: Dual-ID graph model guide exists
- **WHEN** a developer reads the graph ID model guide
- **THEN** they find an explanation of the two ID pairs: `ID`/`VersionID` (changes on update) and `CanonicalID`/`EntityID` (stable across versions)
- **AND** they find guidance on when to use each pair and the deprecation note for old names in v0.8.0

### Requirement: Go SDK version history is linked
The documentation site SHALL link to the SDK changelog so developers can track breaking changes and new features across versions.

#### Scenario: Changelog is accessible from the docs site
- **WHEN** a developer navigates to the Go SDK section
- **THEN** they find a link to or embedded content from `CHANGELOG.md` covering Phase 1 through v0.8.0
