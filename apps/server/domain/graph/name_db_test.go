package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A name is resolved on Create and stored in its own column, so every read that
// feeds a write path must select it. GetHeadByCanonicalID is what Patch reads,
// and a name it cannot see looks like a rename on the very next no-op write
// (#530 regression: a no-op `name` patch created a version).
func TestCreateAndGetByIDCarryName(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	created := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "name-col-1", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	require.NotNil(t, created.Name, "Create must report the resolved name")
	require.Equal(t, "Original", *created.Name)

	got, err := svc.GetByID(ctx, projectID, created.ID, true)
	require.NoError(t, err)
	require.NotNil(t, got.Name, "GetByID reads the detail columns; a column missing there reads as nil")
	require.Equal(t, "Original", *got.Name)
}
