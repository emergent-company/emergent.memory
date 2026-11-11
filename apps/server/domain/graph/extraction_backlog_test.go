package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestBuildExtractionBacklogResponseAggregatesAndTruncates pins the contract a
// caller relies on to decide whether a project's graph is invisible (#496): the
// aggregate counts cover every branch even when the returned branch list is
// capped, and the stranded flag is exactly "there is extraction output but the
// main graph is empty".
func TestBuildExtractionBacklogResponseAggregatesAndTruncates(t *testing.T) {
	now := time.Now().UTC()
	label := "Invoice.pdf"
	branches := []*PendingExtractionBranch{
		{ID: uuid.New(), Name: "extraction/a/1", OriginLabel: &label, CreatedAt: now, ObjectCount: 10, RelationshipCount: 20},
		{ID: uuid.New(), Name: "extraction/b/2", CreatedAt: now, ObjectCount: 5, RelationshipCount: 7},
		{ID: uuid.New(), Name: "extraction/c/3", CreatedAt: now, ObjectCount: 1, RelationshipCount: 0},
	}

	got := buildExtractionBacklogResponse(branches, 0, 2)

	if got.BranchCount != 3 {
		t.Errorf("BranchCount = %d, want 3", got.BranchCount)
	}
	if got.ObjectCount != 16 {
		t.Errorf("ObjectCount = %d, want 16", got.ObjectCount)
	}
	if got.RelationshipCount != 27 {
		t.Errorf("RelationshipCount = %d, want 27", got.RelationshipCount)
	}
	if len(got.Branches) != 2 {
		t.Fatalf("len(Branches) = %d, want the page size 2", len(got.Branches))
	}
	if !got.BranchesTruncated {
		t.Error("BranchesTruncated = false, want true")
	}
	if got.Branches[0].OriginLabel == nil || *got.Branches[0].OriginLabel != label {
		t.Errorf("first branch label not preserved: %+v", got.Branches[0])
	}
	if !got.Stranded {
		t.Error("Stranded = false, want true when objects exist and the main graph is empty")
	}
}

// TestBuildExtractionBacklogResponseNotStrandedWhenMainHasObjects is the
// no-false-positive case: a backlog still exists but default reads are not
// empty, so the "your graph is invisible" alarm must not fire.
func TestBuildExtractionBacklogResponseNotStrandedWhenMainHasObjects(t *testing.T) {
	branches := []*PendingExtractionBranch{
		{ID: uuid.New(), Name: "extraction/a/1", ObjectCount: 3},
	}

	got := buildExtractionBacklogResponse(branches, 1, extractionBacklogBranchPageSize)

	if got.Stranded {
		t.Error("Stranded = true, want false when the main graph has an object")
	}
	if got.BranchesTruncated {
		t.Error("BranchesTruncated = true, want false when the list fits")
	}
}

// TestBuildExtractionBacklogResponseEmptyIsQuiet covers a project with no
// extraction backlog: empty branch list, zero counts, not stranded.
func TestBuildExtractionBacklogResponseEmptyIsQuiet(t *testing.T) {
	got := buildExtractionBacklogResponse(nil, 0, extractionBacklogBranchPageSize)

	if got.BranchCount != 0 || got.ObjectCount != 0 || got.RelationshipCount != 0 {
		t.Errorf("expected zero counts, got %+v", got)
	}
	if got.Stranded {
		t.Error("Stranded = true, want false for an empty project")
	}
	if got.Branches == nil || len(got.Branches) != 0 {
		t.Errorf("Branches = %v, want an empty non-nil slice", got.Branches)
	}
}
