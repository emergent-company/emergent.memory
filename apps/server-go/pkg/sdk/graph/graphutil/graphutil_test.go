package graphutil_test

import (
	"testing"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph/graphutil"
)

// =============================================================================
// IDSet tests
// =============================================================================

func TestIDSet_Contains_MatchesVersionID(t *testing.T) {
	obj := &graph.GraphObject{
		ID:          "v1-abc",
		CanonicalID: "ent-xyz",
	}
	s := graphutil.NewIDSet(obj)

	if !s.Contains("v1-abc") {
		t.Error("expected Contains(VersionID) to return true")
	}
}

func TestIDSet_Contains_MatchesEntityID(t *testing.T) {
	obj := &graph.GraphObject{
		ID:          "v1-abc",
		CanonicalID: "ent-xyz",
	}
	s := graphutil.NewIDSet(obj)

	if !s.Contains("ent-xyz") {
		t.Error("expected Contains(EntityID) to return true")
	}
}

func TestIDSet_Contains_RejectsUnrelated(t *testing.T) {
	obj := &graph.GraphObject{
		ID:          "v1-abc",
		CanonicalID: "ent-xyz",
	}
	s := graphutil.NewIDSet(obj)

	if s.Contains("other-id") {
		t.Error("expected Contains(unrelated) to return false")
	}
}

func TestIDSet_NewIDSetFromIDs(t *testing.T) {
	s := graphutil.NewIDSetFromIDs("v1-abc", "ent-xyz")
	if !s.Contains("v1-abc") {
		t.Error("expected Contains(VersionID) to return true")
	}
	if !s.Contains("ent-xyz") {
		t.Error("expected Contains(EntityID) to return true")
	}
}

func TestIDSet_ConstructFromObject(t *testing.T) {
	obj := &graph.GraphObject{
		ID:          "v1-abc",
		CanonicalID: "ent-xyz",
		Version:     1,
		Type:        "task",
		ProjectID:   "proj_1",
		Properties:  map[string]any{"name": "test"},
		Labels:      []string{},
		CreatedAt:   time.Now(),
	}
	s := graphutil.NewIDSet(obj)
	if s.VersionID != "v1-abc" {
		t.Errorf("expected VersionID = v1-abc, got %s", s.VersionID)
	}
	if s.EntityID != "ent-xyz" {
		t.Errorf("expected EntityID = ent-xyz, got %s", s.EntityID)
	}
}

// =============================================================================
// ObjectIndex tests
// =============================================================================

func makeObj(id, canonicalID string, version int) *graph.GraphObject {
	return &graph.GraphObject{
		ID:          id,
		CanonicalID: canonicalID,
		Version:     version,
		Type:        "test",
		ProjectID:   "proj_1",
		Properties:  map[string]any{},
		Labels:      []string{},
		CreatedAt:   time.Now(),
	}
}

func TestObjectIndex_LookupByVersionID(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
		makeObj("v1-def", "ent-2", 1),
	}
	idx := graphutil.NewObjectIndex(objs)

	got := idx.Get("v1-abc")
	if got == nil {
		t.Fatal("expected to find object by VersionID")
	}
	if got.ID != "v1-abc" {
		t.Errorf("expected ID v1-abc, got %s", got.ID)
	}
}

func TestObjectIndex_LookupByEntityID(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
		makeObj("v1-def", "ent-2", 1),
	}
	idx := graphutil.NewObjectIndex(objs)

	got := idx.Get("ent-2")
	if got == nil {
		t.Fatal("expected to find object by EntityID")
	}
	if got.CanonicalID != "ent-2" {
		t.Errorf("expected CanonicalID ent-2, got %s", got.CanonicalID)
	}
}

func TestObjectIndex_UnknownReturnsNil(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
	}
	idx := graphutil.NewObjectIndex(objs)

	got := idx.Get("unknown-id")
	if got != nil {
		t.Errorf("expected nil for unknown ID, got %+v", got)
	}
}

func TestObjectIndex_DuplicatesKeepsLatest(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
		makeObj("v2-abc", "ent-1", 2), // same entity, newer version
	}
	idx := graphutil.NewObjectIndex(objs)

	got := idx.Get("ent-1")
	if got == nil {
		t.Fatal("expected to find object")
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
	if got.ID != "v2-abc" {
		t.Errorf("expected ID v2-abc, got %s", got.ID)
	}
}

func TestObjectIndex_Len(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
		makeObj("v2-abc", "ent-1", 2), // duplicate entity
		makeObj("v1-def", "ent-2", 1),
	}
	idx := graphutil.NewObjectIndex(objs)

	if idx.Len() != 2 {
		t.Errorf("expected 2 unique entities, got %d", idx.Len())
	}
}

// =============================================================================
// UniqueByEntity tests
// =============================================================================

func TestUniqueByEntity_RemovesOlderVersions(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
		makeObj("v2-abc", "ent-1", 2),
	}
	result := graphutil.UniqueByEntity(objs)

	if len(result) != 1 {
		t.Fatalf("expected 1 unique entity, got %d", len(result))
	}
	if result[0].Version != 2 {
		t.Errorf("expected version 2, got %d", result[0].Version)
	}
}

func TestUniqueByEntity_PreservesUniqueEntities(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "ent-1", 1),
		makeObj("v1-def", "ent-2", 1),
		makeObj("v1-ghi", "ent-3", 1),
	}
	result := graphutil.UniqueByEntity(objs)

	if len(result) != 3 {
		t.Errorf("expected 3 unique entities, got %d", len(result))
	}
}

func TestUniqueByEntity_HandlesEmptyEntityID(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-abc", "", 1), // empty CanonicalID, uses ID as key
		makeObj("v1-def", "", 1), // different ID, also falls back
	}
	result := graphutil.UniqueByEntity(objs)

	if len(result) != 2 {
		t.Errorf("expected 2 objects (different fallback keys), got %d", len(result))
	}
}

func TestUniqueByEntity_PreservesFirstAppearanceOrder(t *testing.T) {
	objs := []*graph.GraphObject{
		makeObj("v1-c", "ent-3", 1),
		makeObj("v1-a", "ent-1", 1),
		makeObj("v1-b", "ent-2", 1),
	}
	result := graphutil.UniqueByEntity(objs)

	if len(result) != 3 {
		t.Fatalf("expected 3 entities, got %d", len(result))
	}
	if result[0].CanonicalID != "ent-3" {
		t.Errorf("expected first entity ent-3, got %s", result[0].CanonicalID)
	}
	if result[1].CanonicalID != "ent-1" {
		t.Errorf("expected second entity ent-1, got %s", result[1].CanonicalID)
	}
}

func TestUniqueByEntity_EmptySlice(t *testing.T) {
	result := graphutil.UniqueByEntity(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}
