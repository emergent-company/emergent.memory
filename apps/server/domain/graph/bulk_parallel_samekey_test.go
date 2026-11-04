package graph

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBulkUpsertObjects_Parallel_SameKeyInOneRequestSerializes pins a NEW
// invariant not covered by the existing upsert test (which only does
// create-then-update across two sequential requests with distinct keys):
// multiple items in ONE bulk upsert request that target the SAME (type,key)
// must still serialize on the per-identity advisory lock, yielding exactly one
// HEAD and a linear v1..vN chain. Without serialization the unique HEAD index
// (IDX_graph_objects_upsert_main) rejects the losers, so this also fails as a
// partial batch.
func TestBulkUpsertObjects_Parallel_SameKeyInOneRequestSerializes(t *testing.T) {
	db := openBulkTestDBTB(t)
	const n = 6
	repo := newBulkTestRepoConc(t, db, n)
	svc := NewService(repo, repo.log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
	ctx := context.Background()
	projectID := uuid.New()
	seedProjectTB(t, db, projectID)

	key := "same-key-one-request"
	items := make([]CreateGraphObjectRequest, n)
	for i := range items {
		items[i] = CreateGraphObjectRequest{
			Type:       "Thing",
			Key:        &key,
			Properties: map[string]any{"name": fmt.Sprintf("v%d", i)},
		}
	}

	resp, err := svc.BulkUpsertObjects(ctx, projectID, &BulkUpsertObjectsRequest{Items: items}, nil)
	require.NoError(t, err)
	require.Equal(t, n, resp.Success, "all same-key items must succeed (serialized)")
	require.Equal(t, 0, resp.Failed, "no item may lose the unique-HEAD race")

	var heads, total int
	err = db.NewRaw(`SELECT count(*) FROM kb.graph_objects
		WHERE project_id = ? AND type = 'Thing' AND key = ? AND supersedes_id IS NULL`, projectID, key).Scan(ctx, &heads)
	require.NoError(t, err)
	err = db.NewRaw(`SELECT count(*) FROM kb.graph_objects
		WHERE project_id = ? AND type = 'Thing' AND key = ?`, projectID, key).Scan(ctx, &total)
	require.NoError(t, err)

	assert.Equal(t, 1, heads, "same-key items in one request must produce exactly one HEAD")
	assert.Equal(t, n, total, "same-key items must form a linear v1..vN chain")
}
