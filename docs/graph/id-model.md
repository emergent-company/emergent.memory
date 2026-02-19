# Graph ID Model

The Emergent graph API uses a **dual-ID model** for objects and relationships. Understanding how these IDs work is essential for correct usage — especially around updates, relationship creation, and query deduplication.

## The Two IDs

Every graph object has two IDs:

| Field         | JSON           | Semantics                           | Changes on update?            |
| ------------- | -------------- | ----------------------------------- | ----------------------------- |
| `ID`          | `id`           | Version-specific (immutable row ID) | Yes — new ID each version     |
| `CanonicalID` | `canonical_id` | Stable entity identifier            | No — same across all versions |

> **Future rename:** These will be renamed to `VersionID` / `EntityID` in a future release. The old names will remain as deprecated aliases during the transition period.

### Lifecycle Example

```
CreateObject({type: "Person", properties: {name: "Alice"}})
  → ID: "obj_abc"  CanonicalID: "obj_abc"   Version: 1
  (On first create, ID == CanonicalID)

UpdateObject("obj_abc", {properties: {name: "Alice Smith"}})
  → ID: "obj_xyz"  CanonicalID: "obj_abc"   Version: 2
  (ID changed! CanonicalID stayed the same)

UpdateObject("obj_xyz", {properties: {name: "Alice S."}})
  → ID: "obj_999"  CanonicalID: "obj_abc"   Version: 3
  (ID changed again, CanonicalID still "obj_abc")
```

The key insight: **`ID` is a version pointer, `CanonicalID` is an entity pointer.** After an update, the old `ID` becomes stale — it points to a superseded version. The `CanonicalID` always resolves to the latest version.

## The UpdateObject Footgun

The most common mistake with the dual-ID model:

```go
// WRONG — using the original ID after update
obj, _ := client.CreateObject(ctx, &CreateObjectRequest{Type: "Person", ...})
updated, _ := client.UpdateObject(ctx, obj.ID, &UpdateObjectRequest{...})
// obj.ID is now STALE — it points to version 1, not version 2
client.CreateRelationship(ctx, &CreateRelationshipRequest{
    SrcID: obj.ID,  // BUG: stale version-specific ID
    DstID: other.ID,
})

// CORRECT — use the returned object from UpdateObject
obj, _ := client.CreateObject(ctx, &CreateObjectRequest{Type: "Person", ...})
updated, _ := client.UpdateObject(ctx, obj.ID, &UpdateObjectRequest{...})
client.CreateRelationship(ctx, &CreateRelationshipRequest{
    SrcID: updated.CanonicalID,  // stable entity ID, always works
    DstID: other.CanonicalID,
})
```

**Rule of thumb:** After calling `UpdateObject`, always use the **returned** object for subsequent operations. Never cache and reuse `ID` values across updates.

## Relationship Lookup Guidance

When creating relationships, prefer `CanonicalID` for `SrcID` and `DstID`:

```go
rel, err := client.CreateRelationship(ctx, &CreateRelationshipRequest{
    Type:  "WORKS_FOR",
    SrcID: person.CanonicalID,  // stable
    DstID: org.CanonicalID,     // stable
})
```

Using `CanonicalID` ensures the relationship remains valid even if either object is subsequently updated (which would change their `ID` but not their `CanonicalID`).

### Checking if a Relationship Exists

Use `HasRelationship` for a simple boolean check:

```go
exists, err := client.HasRelationship(ctx, "WORKS_FOR", person.CanonicalID, org.CanonicalID)
```

Both version-specific and canonical IDs work — the server resolves either form.

### Looking Up Objects by Any ID

Use `GetByAnyID` when you have an ID that could be either form:

```go
obj, err := client.GetByAnyID(ctx, someID)
// Works with both "obj_abc" (version) and "can_123" (canonical)
```

The server already resolves both forms, so this is semantically identical to `GetObject` — but the name makes the caller's intent explicit.

## Query Result Deduplication

When querying objects (e.g., via `ListObjects` or search), results may include multiple versions of the same entity. Use the `graphutil` package to deduplicate:

```go
import "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph/graphutil"

// Deduplicate by entity, keeping the latest version
unique := graphutil.UniqueByEntity(results)

// Or build an index for O(1) lookups by either ID
index := graphutil.NewObjectIndex(results)
obj := index.Get("can_123")  // works with either ID form

// Check if an ID matches a specific object
ids := graphutil.NewIDSet(obj)
if ids.Contains(someID) {
    // someID is either the version ID or canonical ID of obj
}
```

## Server Behavior Summary

The server handles canonical IDs correctly in all operations:

- **GetObject / GetByAnyID**: Accepts either ID form, returns the current (non-superseded) version
- **CreateRelationship**: Internally resolves src/dst to their canonical IDs before storing
- **ExpandGraph**: BFS traversal is keyed by canonical ID — no duplicate visits
- **Search (FTS, hybrid)**: Filters `supersedes_id IS NULL` to return only current versions
- **ListRelationships**: Accepts either ID form for `src_id`, `dst_id`, `object_id` filters
