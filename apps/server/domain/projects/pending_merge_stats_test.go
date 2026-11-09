package projects

import "testing"

// TestPopulateStatsReportsPendingExtractionMerge pins the project-status signal
// from #496: a default project must be able to say how much extraction output is
// stranded on unmerged branches, and whether default graph reads are empty.
func TestPopulateStatsReportsPendingExtractionMerge(t *testing.T) {
	row := &projectWithStats{
		Project: Project{ID: "p1"},
		// One branch with objects only on the branch, nothing on main: stranded.
		PendingExtractionBranchCount:       1,
		PendingExtractionObjectCount:       29104,
		PendingExtractionRelationshipCount: 51651,
		MainGraphObjectCount:               0,
	}

	row.populateStats()

	stats := row.Stats
	if stats == nil || stats.PendingExtractionMerge == nil {
		t.Fatal("PendingExtractionMerge was not populated")
	}
	got := stats.PendingExtractionMerge
	if got.BranchCount != 1 || got.ObjectCount != 29104 || got.RelationshipCount != 51651 {
		t.Errorf("pending merge counts = %+v", got)
	}
	if got.MainGraphObjectCount != 0 {
		t.Errorf("MainGraphObjectCount = %d, want 0", got.MainGraphObjectCount)
	}
	if !got.Stranded {
		t.Error("Stranded = false, want true when extraction output exists and main is empty")
	}
}

// TestPopulateStatsPendingMergeNotStrandedWhenMainHasObjects is the
// no-false-positive case.
func TestPopulateStatsPendingMergeNotStrandedWhenMainHasObjects(t *testing.T) {
	row := &projectWithStats{
		Project:                      Project{ID: "p1"},
		PendingExtractionBranchCount: 2,
		PendingExtractionObjectCount: 5,
		MainGraphObjectCount:         10,
	}

	row.populateStats()

	got := row.Stats.PendingExtractionMerge
	if got == nil {
		t.Fatal("PendingExtractionMerge was not populated")
	}
	if got.Stranded {
		t.Error("Stranded = true, want false when the main graph has objects")
	}
}
