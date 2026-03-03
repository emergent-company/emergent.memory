## 1. Infrastructure Setup

- [x] 1.1 Create branch `sdk-documentation` from `main`
- [x] 1.2 Create `docs/site/` directory (MkDocs source root, separate from internal `docs/`)
- [x] 1.3 Create `docs/requirements.txt` with `mkdocs-material==9.5.50` (and `mkdocs-autorefs` if cross-page linking is needed)
- [x] 1.4 Add `site/` to `.gitignore` (MkDocs build output directory)
- [x] 1.5 Create `mkdocs.yml` at repo root with: `site_name`, `docs_dir: docs/site`, `theme: material`, navigation tabs (Home / Go SDK / Swift SDK), and plugins: search, content-tabs, copy-code, navigation-expand, navigation-sections
- [x] 1.6 Verify `mkdocs build --strict` passes with the empty scaffold (placeholder `index.md` files)

## 2. Site Home Page

- [x] 2.1 Create `docs/site/index.md` — landing page introducing Emergent, the Go SDK, and the Swift SDK with links to each quickstart
- [x] 2.2 Add one-time setup note for repo admins: enable GitHub Pages at Settings → Pages → Source: `gh-pages` branch

## 3. Go SDK Narrative Guides

- [x] 3.1 Create `docs/site/go-sdk/index.md` — Go SDK overview: module path (`github.com/emergent-company/emergent/apps/server-go/pkg/sdk`), version, feature summary, link to GitHub Pages, note about sub-module `go get` tag
- [x] 3.2 Create `docs/site/go-sdk/authentication.md` — all three auth modes: API key (`X-API-Key` header), API token (`emt_*` prefix, auto-detected by `IsAPIToken`), and OAuth device flow; full `sdk.Config` code snippet for each
- [x] 3.3 Create `docs/site/go-sdk/multi-tenancy.md` — `SetContext(orgID, projectID)` explanation; table of 26 context-scoped vs 4 non-context clients (Health, Superadmin, APIDocs, Provider)
- [x] 3.4 Create `docs/site/go-sdk/error-handling.md` — `errors.Error` type; all five predicates (`IsNotFound`, `IsForbidden`, `IsUnauthorized`, `IsBadRequest`, `ParseErrorResponse`); complete branching example with `sdkerrors`
- [x] 3.5 Create `docs/site/go-sdk/streaming.md` — `client.Chat.StreamChat` full example: create stream, iterate `stream.Events()`, handle `token`/`done`/`error` types, defer `stream.Close()`
- [x] 3.6 Create `docs/site/go-sdk/graph-id-model.md` — dual-ID model explanation: `ID`/`VersionID` (mutable, changes on update) vs `CanonicalID`/`EntityID` (stable); v0.8.0 deprecation note for old names; link to `docs/graph/id-model.md`
- [x] 3.7 Create `docs/site/go-sdk/changelog.md` — embed or include the `CHANGELOG.md` content from `apps/server-go/pkg/sdk/CHANGELOG.md` (or link to it on GitHub)

## 4. Go SDK Reference — Supporting Packages

- [x] 4.1 Create `docs/site/go-sdk/reference/auth.md` — `Provider` interface, `APIKeyProvider`, `APITokenProvider`, `OAuthProvider`, `Credentials` struct, `LoadCredentials`, `SaveCredentials`, `DiscoverOIDC`, `IsAPIToken`
- [x] 4.2 Create `docs/site/go-sdk/reference/errors.md` — `Error` type fields; `IsNotFound`, `IsForbidden`, `IsUnauthorized`, `IsBadRequest`, `ParseErrorResponse` signatures and usage
- [x] 4.3 Create `docs/site/go-sdk/reference/graphutil.md` — `IDSet`, `ObjectIndex`, `UniqueByEntity`; when to use each in the context of the dual-ID graph model
- [x] 4.4 Create `docs/site/go-sdk/reference/testutil.md` — `MockServer`, `AssertHeader`, `AssertMethod`, `AssertJSONBody`, `JSONResponse`; brief usage note for writing SDK tests

## 5. Go SDK Reference — Context-Scoped Service Clients (26 pages)

- [x] 5.1 Create `docs/site/go-sdk/reference/graph.md` — all exported types and methods from `graph/client.go` (ListObjects, GetObject, CreateObject, UpdateObject, DeleteObject, ListRelationships, CreateRelationship, UpdateRelationship, DeleteRelationship, Search, Traverse, Analytics, Branches, etc.); dual-ID model callout
- [x] 5.2 Create `docs/site/go-sdk/reference/documents.md` — List, Get, Upload, Download, Delete, Batch operations; `ListOptions`, `UploadOptions` types
- [x] 5.3 Create `docs/site/go-sdk/reference/search.md` — unified search (lexical, semantic, hybrid); `SearchRequest`, `SearchResponse` types; strategy options
- [x] 5.4 Create `docs/site/go-sdk/reference/chat.md` — ListConversations, CreateConversation, GetConversation, DeleteConversation, StreamChat; `StreamRequest`, `StreamEvent` types; SSE event types
- [x] 5.5 Create `docs/site/go-sdk/reference/chunks.md` — List, Get, Search, Delete; `ListOptions`, `Chunk` type
- [x] 5.6 Create `docs/site/go-sdk/reference/chunking.md` — re-chunk documents with current strategy; `RechunkRequest` type
- [x] 5.7 Create `docs/site/go-sdk/reference/agents.md` — List, Get, Create, Update, Delete, Run background agents; exported types
- [x] 5.8 Create `docs/site/go-sdk/reference/agentdefinitions.md` — agent definition CRUD; exported types
- [x] 5.9 Create `docs/site/go-sdk/reference/branches.md` — graph branch management; Create, List, Get, Delete, Merge; exported types
- [x] 5.10 Create `docs/site/go-sdk/reference/projects.md` — Project CRUD, member management; `Project`, `CreateProjectRequest`, `ProjectMember` types
- [x] 5.11 Create `docs/site/go-sdk/reference/orgs.md` — Organization CRUD; `Org`, `CreateOrgRequest` types
- [x] 5.12 Create `docs/site/go-sdk/reference/users.md` — user profile Get, Update; `User`, `UpdateUserRequest` types
- [x] 5.13 Create `docs/site/go-sdk/reference/apitokens.md` — API token Create, List, Delete; `APIToken`, `CreateAPITokenRequest` types
- [x] 5.14 Create `docs/site/go-sdk/reference/mcp.md` — MCP JSON-RPC client; `CallToolRequest`, `CallToolResponse` types
- [x] 5.15 Create `docs/site/go-sdk/reference/mcpregistry.md` — MCP server registry List, Get, Register, Deregister; exported types
- [x] 5.16 Create `docs/site/go-sdk/reference/datasources.md` — data source integrations List, Get, Create, Update, Delete, trigger sync; exported types
- [x] 5.17 Create `docs/site/go-sdk/reference/discoveryjobs.md` — type discovery workflow Create, Get, List, Cancel; exported types
- [x] 5.18 Create `docs/site/go-sdk/reference/embeddingpolicies.md` — embedding policy Get, Update; `EmbeddingPolicy` type
- [x] 5.19 Create `docs/site/go-sdk/reference/integrations.md` — third-party integrations List, Get, Create, Delete; exported types
- [x] 5.20 Create `docs/site/go-sdk/reference/templatepacks.md` — template pack assignment List, Assign, Unassign; `TemplatePack`, type listing; exported types
- [x] 5.21 Create `docs/site/go-sdk/reference/typeregistry.md` — project type definitions List, Get, Create, Update, Delete; exported types
- [x] 5.22 Create `docs/site/go-sdk/reference/notifications.md` — notification List, Get, MarkRead, Delete; exported types
- [x] 5.23 Create `docs/site/go-sdk/reference/tasks.md` — background task tracking List, Get, Cancel; `Task`, `TaskStatus` types
- [x] 5.24 Create `docs/site/go-sdk/reference/monitoring.md` — extraction job monitoring List, Get; exported types
- [x] 5.25 Create `docs/site/go-sdk/reference/useractivity.md` — user activity tracking List; exported types
- [x] 5.26 Create `docs/site/go-sdk/reference/chunking.md` — (confirm distinct from 5.6; merge or keep separate based on source) — SKIP: duplicate of 5.6, file already exists

## 6. Go SDK Reference — Non-Context Service Clients (4 pages)

- [x] 6.1 Create `docs/site/go-sdk/reference/health.md` — Health, Ready, Debug, JobMetrics; `HealthResponse`, `JobMetricsResponse` types; load-balancer readiness note
- [x] 6.2 Create `docs/site/go-sdk/reference/superadmin.md` — administrative operations; note requires superadmin role; exported types
- [x] 6.3 Create `docs/site/go-sdk/reference/apidocs.md` — built-in API docs client; link to `/openapi.json` and `/docs` (Swagger UI) endpoints
- [x] 6.4 Create `docs/site/go-sdk/reference/provider.md` — provider/model configuration; exported types

## 7. Swift SDK Documentation

- [x] 7.1 Create `docs/site/swift-sdk/index.md` — overview of the Swift Core layer; status note that `EmergentSwiftSDK/` is a planned stub pending `emergent-company/emergent#49`; source file locations (`Emergent/Core/EmergentAPIClient.swift`, `Emergent/Core/Models.swift` in `emergent-company/emergent.memory.mac`)
- [x] 7.2 Create `docs/site/swift-sdk/api-client.md` — all 15 `EmergentAPIClient` methods with signatures, parameters, return types, and the API endpoint each calls: `configure(serverURL:apiKey:)`, `fetchProjects()`, `fetchTraces(projectID:)`, `searchObjects(projectID:query:limit:)`, `fetchObject(projectID:objectID:)`, `searchDocuments(projectID:query:)`, `executeQuery(projectID:query:limit:)`, `fetchWorkers()`, `fetchDiagnostics()`, `fetchAgents()`, `updateAgent(_:)`, `fetchMCPServers()`, `fetchUserProfile()`, `fetchAccountStats()`
- [x] 7.3 Create `docs/site/swift-sdk/models.md` — all 16 public types with property tables: `Project`, `ProjectStats`, `AccountStats`, `Trace`, `TraceDetail`, `LLMCall`, `Worker`, `WorkerStatus`, `GraphObject`, `AnyCodable`, `Agent`, `MCPServer`, `MCPTool`, `UserProfile`, `Document`, `ServerDiagnostics`, `QueryResult`, `QueryResultItem`, `QueryResultMeta`
- [x] 7.4 Create `docs/site/swift-sdk/errors.md` — `EmergentAPIError` enum all cases: `notConfigured`, `invalidURL`, `unauthorized`, `notFound`, `serverError(statusCode:message:)`, `httpError(statusCode:)`, `network(Error)`, `decodingFailed(Error)`; `ConnectionState` enum: `.unknown`, `.connected`, `.disconnected`, `.error(String)`
- [x] 7.5 Create `docs/site/swift-sdk/roadmap.md` — formal `EmergentSwiftSDK` Swift Package design (CGO bridge to `libemergent.a`); link to upstream issue; timeline note; `EmergentSwiftSDK/` directory stub location in Mac repo

## 8. LLM Reference Files

- [x] 8.1 Create `docs/llms-go-sdk.md` — Go SDK LLM reference: module path, install command, `Client` struct field table (all 30 service clients), `Config`/`AuthConfig` structs, all three auth modes, `SetContext` signature, `Do`/`Close`, `errors` predicates, `auth` package types, per-client method name lists with one-line descriptions, dual-ID graph model explanation
- [x] 8.2 Create `docs/llms-swift-sdk.md` — Swift SDK LLM reference: repo, source files, all 15 `EmergentAPIClient` method signatures with parameter names and return types, all 16 public model types with property lists, all `EmergentAPIError` cases, `ConnectionState` enum, status note about `EmergentSwiftSDK/` stub
- [x] 8.3 Create `docs/llms.md` — combined LLM reference: brief Emergent platform description, `## Go SDK` section (links to `llms-go-sdk.md` + key highlights), `## Swift SDK` section (links to `llms-swift-sdk.md` + key highlights); suitable as single-file context injection

## 9. mkdocs.yml Navigation Wiring

- [x] 9.1 Add all Go SDK guide pages to `mkdocs.yml` nav under "Go SDK" tab
- [x] 9.2 Add all 33 Go SDK reference pages to `mkdocs.yml` nav under "Go SDK → Reference" group (4 supporting + 4 non-context + 25 context-scoped)
- [x] 9.3 Add all 5 Swift SDK pages to `mkdocs.yml` nav under "Swift SDK" tab
- [x] 9.4 Add cross-reference from `apidocs.md` to the live `/openapi.json` and `/docs` (Swagger UI) endpoints per the `api-documentation` spec
- [x] 9.5 Run `mkdocs build --strict` and confirm zero warnings or errors

## 10. CI Workflow

- [x] 10.1 Create `.github/workflows/docs.yml` with: trigger on push to `main` (paths: `docs/**`, `mkdocs.yml`) and on pull_request (same paths); `permissions: contents: write`
- [x] 10.2 Add `build` job: checkout, setup Python, `pip install -r docs/requirements.txt`, `mkdocs build --strict`
- [x] 10.3 Add deploy step (runs only on `push` to `main`, not PRs): `mkdocs gh-deploy --force`
- [x] 10.4 Verify workflow YAML is valid (lint with `actionlint` or manual review)

## 11. README Update

- [x] 11.1 Add a documentation badge or prominent link near the top of `apps/server-go/pkg/sdk/README.md` pointing to the GitHub Pages site URL (`https://emergent-company.github.io/emergent/`)
- [x] 11.2 Add a one-liner note in the README directing readers to the docs site for per-package reference and guides

## 12. Verification

- [ ] 12.1 Run `mkdocs serve` locally and manually navigate: home page, Go SDK quickstart, at least 3 reference pages, Swift SDK overview
- [x] 12.2 Run `mkdocs build --strict` locally — confirm zero errors
- [ ] 12.3 Open PR against `main`; confirm `docs.yml` CI build-check passes (build-only, no deploy)
- [x] 12.4 Confirm all 33 Go SDK reference pages are present under `docs/site/go-sdk/reference/`
- [x] 12.5 Confirm all 3 LLM files exist at `docs/llms.md`, `docs/llms-go-sdk.md`, `docs/llms-swift-sdk.md` and contain no YAML front matter or MkDocs directives
- [ ] 12.6 After merge to `main`: confirm `mkdocs gh-deploy` step completes in CI and `gh-pages` branch is created/updated
- [ ] 12.7 (Manual, one-time) Enable GitHub Pages in repo Settings → Pages → Source: `gh-pages` branch; confirm site is live at `https://emergent-company.github.io/emergent/`
