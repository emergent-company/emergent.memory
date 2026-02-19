// Package graphutil provides utility types for working with the dual ID model
// of Emergent graph objects (VersionID / EntityID).
//
// Emergent graph objects have two identifiers:
//   - ID (VersionID): changes on every UpdateObject call — a version handle
//   - CanonicalID (EntityID): stable, never changes after creation — the logical identity
//
// These helpers make it easy to compare, index, and deduplicate objects
// regardless of which ID variant you have.
package graphutil

import (
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
)

// =============================================================================
// IDSet — canonical-aware ID comparison
// =============================================================================

// IDSet wraps both ID variants of a graph entity for canonical-aware comparison.
// Use Contains to check whether an arbitrary ID matches either the version-specific
// ID or the stable entity ID.
type IDSet struct {
	VersionID string
	EntityID  string
}

// NewIDSet creates an IDSet from a GraphObject, extracting its version-specific
// and entity identifiers. Prefers VersionID/EntityID, falls back to ID/CanonicalID.
func NewIDSet(obj *graph.GraphObject) IDSet {
	vid := obj.VersionID
	if vid == "" {
		vid = obj.ID
	}
	eid := obj.EntityID
	if eid == "" {
		eid = obj.CanonicalID
	}
	return IDSet{
		VersionID: vid,
		EntityID:  eid,
	}
}

// NewIDSetFromIDs creates an IDSet from explicit ID values.
func NewIDSetFromIDs(versionID, entityID string) IDSet {
	return IDSet{
		VersionID: versionID,
		EntityID:  entityID,
	}
}

// Contains returns true if the given id matches either the VersionID or EntityID.
func (s IDSet) Contains(id string) bool {
	return id == s.VersionID || id == s.EntityID
}

// =============================================================================
// ObjectIndex — O(1) lookup by either ID variant
// =============================================================================

// ObjectIndex indexes a slice of GraphObjects by both ID (version) and
// CanonicalID (entity) for O(1) lookup. When multiple objects share the same
// CanonicalID (multiple versions), the one with the highest Version is kept.
type ObjectIndex struct {
	byID     map[string]*graph.GraphObject
	entities int // number of unique entities
}

// NewObjectIndex builds an index from a slice of GraphObjects.
// If multiple objects share the same CanonicalID, the one with the highest
// Version number is retained.
func NewObjectIndex(objects []*graph.GraphObject) *ObjectIndex {
	// First pass: find the latest version per entity
	latest := make(map[string]*graph.GraphObject, len(objects))
	for _, obj := range objects {
		key := entityKeyFor(obj)
		if existing, ok := latest[key]; ok {
			if obj.Version > existing.Version {
				latest[key] = obj
			}
		} else {
			latest[key] = obj
		}
	}

	// Second pass: build the lookup map from winners only.
	// Index by all available ID variants (ID, VersionID, CanonicalID, EntityID).
	idx := &ObjectIndex{
		byID:     make(map[string]*graph.GraphObject, len(latest)*4),
		entities: len(latest),
	}
	for _, obj := range latest {
		for _, id := range []string{obj.ID, obj.VersionID, obj.CanonicalID, obj.EntityID} {
			if id != "" {
				idx.byID[id] = obj
			}
		}
	}
	return idx
}

// Get retrieves a GraphObject by any ID variant (version-specific or entity).
// Returns nil if no match is found.
func (idx *ObjectIndex) Get(anyID string) *graph.GraphObject {
	return idx.byID[anyID]
}

// Len returns the number of unique entities in the index.
func (idx *ObjectIndex) Len() int {
	return idx.entities
}

// =============================================================================
// UniqueByEntity — deduplication
// =============================================================================

// UniqueByEntity deduplicates a slice of GraphObjects by CanonicalID (entity ID),
// keeping only the object with the highest Version for each entity.
// If an object has an empty CanonicalID, it falls back to using ID as the dedup key.
// The returned slice preserves the order of first appearance.
func UniqueByEntity(objects []*graph.GraphObject) []*graph.GraphObject {
	seen := make(map[string]int, len(objects)) // entityKey -> index in result
	result := make([]*graph.GraphObject, 0, len(objects))

	for _, obj := range objects {
		key := entityKeyFor(obj)

		if idx, exists := seen[key]; exists {
			// Keep the one with the higher version
			if obj.Version > result[idx].Version {
				result[idx] = obj
			}
		} else {
			seen[key] = len(result)
			result = append(result, obj)
		}
	}

	return result
}

// entityKeyFor returns the canonical key for dedup/indexing.
// Prefers EntityID, falls back to CanonicalID, then ID.
func entityKeyFor(obj *graph.GraphObject) string {
	if obj.EntityID != "" {
		return obj.EntityID
	}
	if obj.CanonicalID != "" {
		return obj.CanonicalID
	}
	if obj.VersionID != "" {
		return obj.VersionID
	}
	return obj.ID
}
