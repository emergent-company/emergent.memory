package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestBatchRelationshipDriverFailureStaysInternal is the fail-first regression
// for the batch driver leak found in review round 2 (#899). The per-item result
// is the one place a batch failure is explained to the caller, and it rendered
// the resolver error with %v. Since resolveEntityIDByKey wraps a store failure in
// apperror.NewInternal (whose Error() includes the cause), that printed the raw
// driver text into the tool result while the same error's PublicMessage was
// correctly collapsed. The item now renders PublicMessage, and the cause is
// logged instead.
func TestBatchRelationshipDriverFailureStaysInternal(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "mcpbatchleak")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := seedProject(t, tdb.DB)

	// A store failure looks, from the service, like any query error. A finished
	// transaction produces one deterministically without damaging the schema:
	// every statement fails with sql.ErrTxDone.
	t.Run("store failure collapses to the internal sentence", func(t *testing.T) {
		tx, err := tdb.DB.BeginTx(ctx, nil)
		require.NoError(t, err)
		require.NoError(t, tx.Rollback())

		svc := &Service{db: tx}
		res, err := svc.executeBatchCreateRelationships(ctx, projectID, map[string]any{
			"relationships": []any{map[string]any{
				"type":      "KNOWS",
				"source_id": "store-failure-key",
				"target_id": uuid.NewString(),
			}},
		})
		require.NoError(t, err)

		text := toolResultText(t, res, nil)
		assert.Contains(t, text, "An internal error occurred")
		for _, leak := range []string{"sql:", "transaction", "graph_objects", "pgx", "SQLSTATE"} {
			assert.NotContains(t, text, leak,
				"driver detail must not reach a tool result: %s", text)
		}
		assert.Contains(t, text, inlineRelReasonUnknownSourceID,
			"the item still says which endpoint could not be resolved")
	})

	// The other direction: a genuine key miss is the caller's to fix, so its
	// text (and its guidance) must survive.
	t.Run("key miss stays caller-addressable", func(t *testing.T) {
		svc := &Service{db: tdb.DB}
		res, err := svc.executeBatchCreateRelationships(ctx, projectID, map[string]any{
			"relationships": []any{map[string]any{
				"type":      "KNOWS",
				"source_id": "ghost-key",
				"target_id": uuid.NewString(),
			}},
		})
		require.NoError(t, err)

		text := toolResultText(t, res, nil)
		assert.Contains(t, text, "entity with key")
		assert.Contains(t, text, "ghost-key")
		assert.NotContains(t, text, "An internal error occurred")
	})
}
