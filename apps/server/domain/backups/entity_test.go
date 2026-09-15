package backups

import "testing"

func TestBackupStatsToMap(t *testing.T) {
	got := backupStatsToMap(&BackupStats{
		Documents:          1,
		Chunks:             2,
		GraphObjects:       3,
		GraphRelationships: 4,
		ChatConversations:  5,
		ChatMessages:       6,
		ExtractionJobs:     7,
		ProjectMemberships: 8,
		Files:              9,
		TotalSizeBytes:     10,
	})

	want := map[string]any{
		"documents":          1,
		"chunks":             2,
		"graphObjects":       3,
		"graphRelationships": 4,
		"chatConversations":  5,
		"chatMessages":       6,
		"extractionJobs":     7,
		"projectMemberships": 8,
		"files":              9,
		"totalSizeBytes":     int64(10),
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("backupStatsToMap()[%q] = %#v, want %#v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("backupStatsToMap() returned %d keys, want %d", len(got), len(want))
	}
}

func TestBackupStatsToMapNil(t *testing.T) {
	got := backupStatsToMap(nil)
	if got == nil {
		t.Fatal("backupStatsToMap(nil) returned nil, want empty map")
	}
	if len(got) != 0 {
		t.Errorf("backupStatsToMap(nil) returned %d keys, want 0", len(got))
	}
}
