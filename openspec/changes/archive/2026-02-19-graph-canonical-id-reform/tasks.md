## 1. SDK Helper Utilities (Phase 1 — non-breaking)

- [x] 1.1 Create `pkg/sdk/graph/graphutil/` package directory
- [x] 1.2 Implement `IDSet` type with `NewIDSet(obj *graph.GraphObject) IDSet` and `Contains(id string) bool` methods
- [x] 1.3 Implement `ObjectIndex` type with `NewObjectIndex(objects []*graph.GraphObject) *ObjectIndex` and `Get(anyID string) *graph.GraphObject` — index by both VersionID and EntityID, keep latest version on duplicates
- [x] 1.4 Implement `UniqueByEntity(objects []*graph.GraphObject) []*graph.GraphObject` — dedup by EntityID, fall back to VersionID when EntityID is empty
- [x] 1.5 Write unit tests for `IDSet` (match VersionID, match EntityID, reject unrelated, construct from object)
- [x] 1.6 Write unit tests for `ObjectIndex` (lookup by VersionID, lookup by EntityID, unknown returns nil, duplicate handling)
- [x] 1.7 Write unit tests for `UniqueByEntity` (removes older versions, preserves unique entities, handles empty EntityID)

## 2. SDK Client Methods (Phase 1 — non-breaking)

- [x] 2.1 Add `GetByAnyID(ctx context.Context, id string) (*GraphObject, error)` to `pkg/sdk/graph/client.go` — delegates to existing `GetObject` (server already dual-resolves)
- [x] 2.2 Add `HasRelationship(ctx context.Context, relType, srcID, dstID string) (bool, error)` to `pkg/sdk/graph/client.go` — uses `ListRelationships` with type/src/dst filter, returns true if non-empty
- [x] 2.3 Write unit tests for `GetByAnyID` (resolve by VersionID, resolve by EntityID, stale VersionID, unknown ID error)
- [x] 2.4 Write unit tests for `HasRelationship` (found with EntityIDs, found with VersionIDs, found with mixed, not found returns false)

## 3. Documentation (Phase 1 — non-breaking)

- [x] 3.1 Create `docs/graph-id-model.md` with: Create/Update lifecycle diagram, UpdateObject footgun example (wrong vs correct), relationship lookup guidance, query result dedup patterns
- [x] 3.2 Add godoc comments to `GraphObject.ID` and `GraphObject.CanonicalID` in `pkg/sdk/graph/client.go` explaining version-specific vs stable semantics
- [x] 3.3 Add godoc warning to `Client.UpdateObject` in `pkg/sdk/graph/client.go` about ID mutation — callers MUST use the returned object for subsequent operations
- [x] 3.4 Add godoc note to `Client.CreateRelationship` in `pkg/sdk/graph/client.go` recommending EntityID (CanonicalID) for src/dst

## 4. Server DTO Field Rename (Phase 2 — backward-compatible)

- [x] 4.1 Update `GraphObjectResponse` in `domain/graph/dto.go` — add `VersionID` and `EntityID` fields alongside existing `ID` and `CanonicalID`
- [x] 4.2 Add custom `MarshalJSON` to `GraphObjectResponse` that emits both old names (`id`, `canonical_id`) and new names (`version_id`, `entity_id`)
- [x] 4.3 Update `GraphRelationshipResponse` in `domain/graph/dto.go` — add `VersionID` and `EntityID` fields, add custom `MarshalJSON` emitting both name sets
- [x] 4.4 Update `ExpandNode` response in `domain/graph/dto.go` — ensure node IDs use `entity_id` as the map key, edges reference `entity_id` for src/dst
- [x] 4.5 Update search response DTOs (FTS, hybrid) in `domain/graph/dto.go` to include both old and new field names via custom marshaling
- [x] 4.6 Write/update E2E tests in `tests/e2e/graph_test.go` verifying both old and new JSON field names appear in responses for: CreateObject, GetObject, ListObjects, CreateRelationship, ExpandGraph, FTSSearch

## 5. SDK Client Type Rename (Phase 2 — backward-compatible)

- [x] 5.1 Add `VersionID` and `EntityID` fields to `GraphObject` struct in `pkg/sdk/graph/client.go` alongside existing `ID`/`CanonicalID` (mark old fields as `// Deprecated`)
- [x] 5.2 Add custom `UnmarshalJSON` to `GraphObject` that populates `VersionID` from `version_id` or `id`, and `EntityID` from `entity_id` or `canonical_id`, and keeps deprecated fields populated
- [x] 5.3 Add `VersionID` and `EntityID` fields to `GraphRelationship` struct in `pkg/sdk/graph/client.go` with same custom unmarshal pattern
- [x] 5.4 Update `graphutil` helpers (`IDSet`, `ObjectIndex`, `UniqueByEntity`) to use `VersionID`/`EntityID` fields (with fallback to `ID`/`CanonicalID` during transition)
- [x] 5.5 Update all existing SDK unit tests in `pkg/sdk/graph/client_test.go` to verify both old and new field names are populated after deserialization

## 6. Admin UI Audit (Phase 2)

- [x] 6.1 Search `apps/admin/src/` for references to graph object `id` and `canonical_id` field usage in API responses
- [x] 6.2 Update any admin frontend code to use `version_id`/`entity_id` from API responses (or verify it works with both during transition)

## 7. Verification and Release

- [x] 7.1 Run full server Go test suite (`nx run server-go:test`) and verify all pass
- [x] 7.2 Run E2E test suite (`nx run server-go:test-e2e`) and verify all pass
- [x] 7.3 Run admin lint and tests (`nx run admin:lint && nx run admin:test`) if admin changes were made
- [x] 7.4 Add CHANGELOG entry documenting: new `graphutil` package, new `GetByAnyID`/`HasRelationship` methods, field rename with deprecation timeline, link to `docs/graph-id-model.md`
