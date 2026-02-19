## Context

The graph API uses an append-only versioning model where every `UpdateObject` creates a new row with a fresh physical `ID` while preserving a stable `CanonicalID`. Internally, the server already handles canonical IDs correctly in most places:

- **Relationships already store canonical IDs**: `CreateRelationship` resolves endpoints to `srcObj.CanonicalID` / `dstObj.CanonicalID` before persisting (`service.go:1005-1012`).
- **Queries already dedup via HEAD filtering**: `ListObjects`, `FTSSearch`, and `ExpandGraph` all filter by `supersedes_id IS NULL`, which guarantees at most one row per canonical ID per branch.
- **All lookups dual-resolve**: `GetByID`, `ValidateEndpoints`, and `ExpandGraph` root fetch all use `(id IN (?) OR canonical_id IN (?))`.
- **ExpandGraph already uses canonical IDs**: BFS traversal is keyed by `CanonicalID` (`repository.go:1838`), and `ExpandNode.ID` is set to `CanonicalID` (`service.go:2277`).

The real problems are at the **SDK surface**:

1. The field named `ID` (suggesting "the identifier") is actually a version handle that goes stale after updates, while the stable identifier is named `CanonicalID` (suggesting "secondary/derived").
2. The SDK exposes both IDs without guidance, so consumers default to `ID` and hit subtle bugs.
3. No SDK helpers exist for common patterns (dedup, lookup by either ID, canonical-aware comparison).

### Key files

| Area               | File                         | Key lines                   |
| ------------------ | ---------------------------- | --------------------------- |
| Entity definitions | `domain/graph/entity.go`     | 10-108                      |
| DTOs               | `domain/graph/dto.go`        | 29-56, 160-215              |
| Repository         | `domain/graph/repository.go` | 210-308, 373-432, 1813-2028 |
| Service            | `domain/graph/service.go`    | 957-1144, 2206-2335         |
| SDK client types   | `pkg/sdk/graph/client.go`    | 56-107                      |
| SDK client methods | `pkg/sdk/graph/client.go`    | 849-1432                    |

## Goals / Non-Goals

**Goals:**

- Rename SDK fields so the stable identifier has the simpler, more prominent name
- Provide backward-compatible JSON aliases during a deprecation period
- Add SDK-level helper utilities for canonical ID handling
- Document the dual ID mental model prominently in the SDK
- Verify and maintain the server's existing canonical-correct behavior

**Non-Goals:**

- Changing the database column names (`id`, `canonical_id`) -- internal names are fine
- Redesigning the append-only versioning model itself
- Adding version history browsing (the `WithAllVersions` flag from the proposal is deferred -- the server already only returns HEAD versions via `supersedes_id IS NULL`, so there are no duplicates to dedup in practice)
- Backfilling existing relationship endpoint data -- relationships already store canonical IDs server-side

## Decisions

### Decision 1: Field rename strategy -- JSON aliases with Go struct rename

**Choice**: Rename Go struct fields (`VersionID`/`EntityID`) and use JSON struct tags to emit both old and new names during the transition period.

**Rationale**: The Go SDK is the primary consumer. Renaming struct fields forces a compile-time error when consumers upgrade, which is preferable to runtime surprises. JSON backward compatibility is maintained via custom marshal/unmarshal that emits both `id`/`version_id` and `canonical_id`/`entity_id`.

**Alternatives considered**:

- _Add new fields alongside old ones_: Creates confusion with two ways to access the same data. Consumers won't know which to use.
- _Major version bump with immediate removal_: Too disruptive. SpecMCP and other consumers need a migration window.

**Implementation approach**:

Server-side DTO (`dto.go`):

```go
type GraphObjectResponse struct {
    // New names (primary)
    VersionID   uuid.UUID `json:"version_id"`
    EntityID    uuid.UUID `json:"entity_id"`
    // ...
}
// Custom MarshalJSON emits both old and new names:
// {"id": "...", "version_id": "...", "canonical_id": "...", "entity_id": "..."}
```

SDK client (`client.go`):

```go
type GraphObject struct {
    VersionID   string `json:"version_id"`
    EntityID    string `json:"entity_id"`
    // Deprecated aliases populated from JSON
    ID          string `json:"id"`           // Deprecated: use VersionID
    CanonicalID string `json:"canonical_id"` // Deprecated: use EntityID
}
// Custom UnmarshalJSON: populate VersionID from "version_id" or "id",
// EntityID from "entity_id" or "canonical_id"
```

### Decision 2: Server-side changes are minimal -- mostly verification

**Choice**: The server's internal behavior is already canonical-correct. The main server changes are the DTO field rename and custom JSON marshaling. No store/repository changes needed.

**Rationale**: Code exploration revealed that:

- `CreateRelationship` already resolves to `srcObj.CanonicalID`/`dstObj.CanonicalID` before storing
- `ListObjects` / `FTSSearch` already filter `supersedes_id IS NULL` (HEAD only)
- `ExpandGraph` already keys BFS by `CanonicalID` and deduplicates via `visited` map
- All ID lookups already dual-resolve via `(id IN (?) OR canonical_id IN (?))`

The issues reported in #44 and #45 are already handled server-side. The bugs consumers experienced were caused by using stale `ID` values client-side (the naming problem, #43) rather than actual server-side defects.

**Alternatives considered**:

- _Add explicit dedup logic to queries_: Unnecessary -- `supersedes_id IS NULL` already guarantees uniqueness by canonical ID per branch.
- _Add `WithAllVersions` query parameter_: Deferred. No consumer has requested version history access, and the HEAD-only behavior is correct for all current use cases.

### Decision 3: SDK helpers as a separate `graphutil` package

**Choice**: Ship helper utilities in a new `pkg/sdk/graph/graphutil` sub-package rather than adding methods to the main `Client`.

**Rationale**: Helpers like `IDSet`, `ObjectIndex`, and `UniqueObjects` are pure data utilities that don't need HTTP client access. Keeping them in a sub-package:

- Avoids bloating the client interface
- Makes them usable without a client instance (e.g., in tests)
- Follows Go convention of small, focused packages

**Alternatives considered**:

- _Methods on Client_: `HasRelationship` and `GetByAnyID` require HTTP calls, so these stay on Client. But pure utilities like `IDSet` don't belong there.
- _Top-level `emergent` package_: Too broad; these are graph-specific.

**Proposed API**:

```go
package graphutil

// IDSet wraps both IDs for canonical-aware comparison
type IDSet struct { VersionID, EntityID string }
func NewIDSet(obj *graph.GraphObject) IDSet
func (s IDSet) Contains(id string) bool

// ObjectIndex provides O(1) lookup by either ID variant
type ObjectIndex struct { /* ... */ }
func NewObjectIndex(objects []*graph.GraphObject) *ObjectIndex
func (idx *ObjectIndex) Get(anyID string) *graph.GraphObject

// UniqueByEntity deduplicates objects by EntityID, keeping the latest
func UniqueByEntity(objects []*graph.GraphObject) []*graph.GraphObject
```

Client-level methods:

```go
// GetByAnyID resolves any ID (version or entity) to the current HEAD object.
// This already works today via GetObject since the server dual-resolves,
// but making it explicit improves discoverability.
func (c *Client) GetByAnyID(ctx context.Context, id string) (*GraphObject, error)

// HasRelationship checks existence with canonical-aware matching
func (c *Client) HasRelationship(ctx context.Context, relType, srcID, dstID string) (bool, error)
```

### Decision 4: Documentation lives in SDK godoc + standalone guide

**Choice**: Add field-level godoc on all ID fields, method-level warnings on `UpdateObject` and `CreateRelationship`, plus a standalone `docs/graph-id-model.md` guide with the mental model diagram.

**Rationale**: Godoc is where Go developers look first. The standalone guide provides the full mental model with diagrams and code examples that don't fit in godoc comments.

## Risks / Trade-offs

**[Risk: Breaking SDK consumers on upgrade]** The Go struct field rename will cause compile errors for all consumers using `obj.ID` or `obj.CanonicalID`.
-> _Mitigation_: Keep deprecated `ID` and `CanonicalID` fields populated during transition. Consumers can upgrade incrementally. Add a migration guide with sed/grep commands.

**[Risk: JSON backward compatibility]** Existing API consumers may depend on `"id"` and `"canonical_id"` JSON field names.
-> _Mitigation_: Custom MarshalJSON emits both old and new names simultaneously. Old names will be removed only in a future major version.

**[Risk: Custom JSON marshal/unmarshal complexity]** Adding custom (Un)MarshalJSON to DTOs increases maintenance burden and can conflict with Bun ORM's own serialization.
-> _Mitigation_: Only the response DTOs need custom marshaling, not the entity structs. The entity structs (used by Bun) keep their existing column names. Separation is clean: entity.go is database, dto.go is API.

**[Risk: SDK helpers become stale]** If the server API evolves, the helper utilities may drift.
-> _Mitigation_: Helpers operate on SDK types (`GraphObject`, `GraphRelationship`) which are versioned with the SDK. Add unit tests for all helpers.

**[Trade-off: Deprecation period adds complexity]** Supporting both old and new field names temporarily increases code complexity.
-> _Benefit_: Prevents a hard migration cliff. Worth the temporary complexity.

## Migration Plan

### Phase 1: Non-breaking additions (v1.x)

1. Add `graphutil` package with `IDSet`, `ObjectIndex`, `UniqueByEntity`
2. Add `GetByAnyID` and `HasRelationship` client methods
3. Add documentation (godoc + standalone guide)
4. Release as a minor version -- no consumer changes required

### Phase 2: Field rename with aliases (v1.x+1)

1. Add `VersionID`/`EntityID` fields to SDK `GraphObject` and `GraphRelationship`
2. Keep `ID`/`CanonicalID` populated as deprecated aliases
3. Update server DTOs to emit both old and new JSON names
4. Release as a minor version with deprecation warnings in CHANGELOG
5. Update SpecMCP and other known consumers

### Phase 3: Remove deprecated aliases (v2.0)

1. Remove `ID` and `CanonicalID` fields from SDK types
2. Remove old JSON field names from server DTOs
3. Release as a major version

### Rollback

- Phase 1 and 2 are fully backward-compatible; rollback is just reverting the release
- Phase 3 requires consumers to pin to v1.x if they haven't migrated

## Open Questions

1. **Timeline for Phase 3**: How long should the deprecation period last? Suggest at least 2 release cycles after Phase 2.
2. **Admin UI impact**: Does the admin frontend reference `id` or `canonical_id` in graph-related API calls? Need to audit `apps/admin/src/` for graph API usage.
3. **ExpandGraph response shape**: Currently `ExpandNode.ID` is already set to `CanonicalID`. After the rename, should `ExpandNode` use `EntityID` as its primary key in the response map, or keep a separate `NodeID` field?
