# Graph ID Model

Emergent graph objects have **two distinct ID fields** that serve different purposes. Understanding the difference is critical for building correct integrations.

## The Dual-ID Model

As of SDK v0.8.0, every `GraphObject` and `GraphRelationship` has four ID fields (two are
the canonical names, two are deprecated aliases):

| Field | Alias (deprecated) | Stability | Changes on update? |
|-------|--------------------|-----------|-------------------|
| `VersionID` | `ID` | Version-specific | **Yes** — changes on every `UpdateObject` |
| `EntityID` | `CanonicalID` | Stable | No — stable for the lifetime of the object |

Use `EntityID` when you need a stable reference to an object across updates.  
Use `VersionID` when you need to reference the exact version (e.g., in relationship endpoints).

## Example

```go
obj, err := client.Graph.GetObject(ctx, someID)
if err != nil {
    return err
}

// Stable identifier — use for bookmarks, links, foreign keys
stableRef := obj.EntityID

// Version-specific — changes after UpdateObject
versionRef := obj.VersionID

// Update the object
updated, err := client.Graph.UpdateObject(ctx, obj.VersionID, &graph.UpdateObjectRequest{
    Properties: map[string]any{"status": "active"},
})
// updated.VersionID != obj.VersionID  ← new version created
// updated.EntityID == obj.EntityID    ← same entity
```

!!! warning "Don't store `VersionID` as a long-lived reference"
    Calling `UpdateObject` creates a new version. If you saved `VersionID` before the update,
    it will no longer resolve with `GetObject`. Always use `EntityID` for stable references.

## Deprecated Names

In v0.8.0, the old names were deprecated:

- `GraphObject.ID` → use `GraphObject.VersionID`
- `GraphObject.CanonicalID` → use `GraphObject.EntityID`
- Same for `GraphRelationship.ID` and `GraphRelationship.CanonicalID`

The old fields still exist and are cross-populated for backward compatibility — they always
contain the same value as their replacements. But they will be removed in a future major version.

## Custom JSON Unmarshaling

The SDK uses custom `UnmarshalJSON` on `GraphObject` and `GraphRelationship` to cross-populate
all four fields regardless of which JSON field names the server sends. Server responses
may use `id`/`canonical_id` (old) or `version_id`/`entity_id` (new) — the SDK handles both.

## Accepting Either ID

Use `GetByAnyID` when the caller might have either a version-specific or entity-stable ID:

```go
// Works with both obj.VersionID and obj.EntityID
obj, err := client.Graph.GetByAnyID(ctx, anyKindOfID)
```

## graphutil Helpers

The `graph/graphutil` package provides helpers designed for the dual-ID model:

```go
import "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph/graphutil"

// Create an ID set for membership testing
ids := graphutil.NewIDSet(obj)
ids.Contains(someID) // true for both VersionID and EntityID

// Build a lookup index from a slice of objects
idx := graphutil.NewObjectIndex(objects)
obj := idx.Get(anyID) // O(1) lookup by either ID

// Deduplicate a result set by EntityID (keeps newest version of each entity)
unique := graphutil.UniqueByEntity(objects)
```

See the [graphutil reference](reference/graphutil.md) for full API details.

## Further Reading

The internal design document at `docs/graph/id-model.md` contains lifecycle diagrams,
`UpdateObject` footgun examples, relationship lookup guidance, and deduplication patterns.
