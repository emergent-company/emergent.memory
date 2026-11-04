# graph/graphutil

Package `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph/graphutil`

The `graphutil` package provides utilities for working with the dual-ID graph model — where each object has a version-specific `VersionID` and a stable `EntityID`.

## IDSet

```go
type IDSet struct { /* ... */ }

func NewIDSet(obj *graph.GraphObject) IDSet
func NewIDSetFromIDs(versionID, entityID string) IDSet
func (s IDSet) Contains(id string) bool
```

An `IDSet` represents both IDs for a single object. Use `Contains` to test membership against either ID without knowing which variant you have.

```go
set := graphutil.NewIDSet(obj)
if set.Contains(someID) {
    // someID matches either obj.VersionID or obj.EntityID
}
```

## ObjectIndex

```go
type ObjectIndex struct { /* ... */ }

func NewObjectIndex(objects []*graph.GraphObject) *ObjectIndex
func (idx *ObjectIndex) Get(anyID string) *graph.GraphObject
func (idx *ObjectIndex) Len() int
```

`ObjectIndex` builds an O(1) lookup map over a slice of objects, indexed by both `VersionID` and `EntityID`. Use it when you need to look up objects by either ID form repeatedly.

```go
idx := graphutil.NewObjectIndex(objects)
obj := idx.Get(someID)  // works with VersionID or EntityID
if obj == nil {
    // not found
}
fmt.Println(idx.Len()) // number of unique objects
```

## UniqueByEntity

```go
func UniqueByEntity(objects []*graph.GraphObject) []*graph.GraphObject
```

Deduplicates a slice of objects by `EntityID`. When multiple versions of the same entity appear (e.g., from a search result that returns both old and new versions), `UniqueByEntity` keeps only the most recently-seen entry for each entity.

```go
// Search results may contain multiple versions of the same entity
results := searchResponse.Objects
unique := graphutil.UniqueByEntity(results)
// unique contains at most one entry per EntityID
```

## When to Use Each

| Helper | Use Case |
|--------|----------|
| `IDSet` | Membership test when you have a single object and an unknown ID |
| `ObjectIndex` | Repeated lookups over a fixed slice of objects |
| `UniqueByEntity` | Deduplicating query/search results before displaying or processing |

## See Also

- [Graph ID Model guide](../graph-id-model.md)
- [graph reference](graph.md)
