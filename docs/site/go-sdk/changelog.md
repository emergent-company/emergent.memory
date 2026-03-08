# Changelog

The Emergent Go SDK follows [Semantic Versioning](https://semver.org/) and [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

Source: [`apps/server/pkg/sdk/CHANGELOG.md`](https://github.com/emergent-company/emergent.memory/blob/main/apps/server/pkg/sdk/CHANGELOG.md)

---

## [0.8.0] — Unreleased

### Added

**Graph ID Model Reform (Issues #43–#47):**

- **`graphutil` package** — New `pkg/sdk/graph/graphutil/` with `IDSet`, `ObjectIndex`, and `UniqueByEntity` helpers for canonical-aware ID comparison, O(1) lookup by either ID variant, and query result deduplication
- **`GetByAnyID` method** — Semantic alias for `GetObject` that makes caller intent explicit when the ID could be either version-specific or entity-stable
- **`HasRelationship` method** — Boolean check for relationship existence by type, src, and dst (accepts either ID form)
- **`VersionID` / `EntityID` fields** — Added to `GraphObject` and `GraphRelationship` SDK types alongside deprecated `ID` / `CanonicalID`; custom `UnmarshalJSON` cross-populates all four fields regardless of which names the server sends
- **Dual JSON field names** — Server DTOs now emit both `id`/`canonical_id` and `version_id`/`entity_id` via custom `MarshalJSON`

### Deprecated

- `GraphObject.ID` — Use `VersionID` instead
- `GraphObject.CanonicalID` — Use `EntityID` instead
- `GraphRelationship.ID` — Use `VersionID` instead
- `GraphRelationship.CanonicalID` — Use `EntityID` instead

---

## [0.7.0] — 2026-02-12

### Added

**Capability Gaps:**

- **Template Pack Creation** — Full CRUD for template packs via `TemplatePacks` client
- **Type Schema Registration** — Register and retrieve type schemas via `TypeRegistry` client
- **Property-level Filtering** — JSONB `PropertyFilter` with 9 operators (`eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `startsWith`, `exists`) for `ListObjects`
- **Inverse Relationship Auto-creation** — `InverseType` field on relationships; server auto-creates inverse when set
- **Bulk Object/Relationship Creation** — `BulkCreateObjects` and `BulkCreateRelationships` (max 100 items, partial-success semantics)

**SDK Enhancements:**

- **ListTags filtering** — `ListTagsOptions` with `Type`, `Prefix`, `Limit` fields
- **Custom HTTP client** — `Config.HTTPClient` field for custom `*http.Client`

### Fixed

- **SetContext race condition** — Added `sync.RWMutex` to parent `Client` and all 19 sub-clients
- **ListTags response wrapping** — Server now returns `{"tags": [...]}` instead of bare array
- **Search pagination offset** — Added `Offset` support to FTS, Vector, and Hybrid search
- **SearchWithNeighbors score loss** — `PrimaryResults` now returns `SearchWithNeighborsResultItem` with both Object and Score

### Changed

- `ListTags` signature changed from `ListTags(ctx)` to `ListTags(ctx, *ListTagsOptions)` — pass `nil` for previous behavior
- `SearchWithNeighborsResponse.PrimaryResults` type changed to `[]*SearchWithNeighborsResultItem`
- `MCP.SetContext` signature changed to `SetContext(projectID string)` (orgID removed)

---

## [0.4.12] — 2026-02-11

First public release. 11 service clients, dual authentication, 43 test cases, 4 example programs.

- Module path corrected to `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk`
- Documents, Chunks, Search, Graph, Chat, Projects, Orgs, Users, APITokens, Health, MCP clients
- OAuth device flow authentication
- SSE streaming for chat
