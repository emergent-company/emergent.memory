package extraction

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestQuarantineRoundTrip pins the write → list → dismiss cycle for dropped
// unknown-type extraction results.
func TestQuarantineRoundTrip(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())

	jobID := uuid.NewString()
	docID := uuid.NewString()
	n, err := svc.QuarantineDropped(ctx, projectID, &docID, &jobID, []QuarantineItem{
		{Kind: "object", TypeName: "Personnel", Payload: map[string]any{"name": "Ada", "properties": map[string]any{"role": "CEO"}}},
		{Kind: "relationship", TypeName: "REPORTS_TO", Payload: map[string]any{"source_ref": "a", "target_ref": "b"}},
	})
	require.NoError(t, err)
	require.Equal(t, 2, n)

	rows, err := svc.ListQuarantine(ctx, projectID, "open", 100)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	var objRow, relRow *QuarantineEntry
	for i := range rows {
		if rows[i].Kind == "object" {
			objRow = &rows[i]
		} else {
			relRow = &rows[i]
		}
	}
	require.NotNil(t, objRow, "object row must be listed")
	require.NotNil(t, relRow, "relationship row must be listed")
	require.Equal(t, "Personnel", objRow.TypeName)
	require.Equal(t, "unknown_type", objRow.Reason)
	require.Equal(t, "open", objRow.Status)
	require.Equal(t, "Ada", objRow.Payload["name"])
	require.Equal(t, &jobID, objRow.JobID)
	require.Equal(t, &docID, objRow.DocumentID)

	// Dismiss one and confirm the open list shrinks.
	dismissed, err := svc.DismissQuarantine(ctx, projectID, []string{objRow.ID})
	require.NoError(t, err)
	require.Equal(t, 1, dismissed)

	open, err := svc.ListQuarantine(ctx, projectID, "open", 100)
	require.NoError(t, err)
	require.Len(t, open, 1)
	require.Equal(t, relRow.ID, open[0].ID)

	all, err := svc.ListQuarantine(ctx, projectID, "all", 100)
	require.NoError(t, err)
	require.Len(t, all, 2, "dismissed rows remain visible under 'all'")
}

// TestQuarantineEmptyNoOp pins that an empty batch writes nothing and never
// touches the database.
func TestQuarantineEmptyNoOp(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	svc := NewObjectExtractionJobsService(db, quietLogger(), DefaultObjectExtractionConfig())
	n, err := svc.QuarantineDropped(ctx, projectID, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
