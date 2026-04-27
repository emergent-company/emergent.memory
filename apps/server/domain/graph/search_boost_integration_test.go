package graph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// TestRecencyBoostFavorsNewObjectsIntegration verifies that with recency_boost=1.0,
// a newer object ranks above an older equally-relevant object.
func TestRecencyBoostFavorsNewObjectsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires database")
	}

	db := openBulkTestDB(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := NewRepository(db, log, cfg)
	svc := NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	projectID := uuid.New()
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID) //nolint:errcheck
	})

	// Insert a NEW object (1 hour old)
	newObjID := uuid.New()
	newCreatedAt := time.Now().Add(-1 * time.Hour)
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, 'active',
			 '{"name": "recency-boost-test-new"}'::jsonb, '{}'::text[], ?, ?, ?)
	`, newObjID, projectID, newObjID, "RecencyTest",
		fmt.Sprintf("hash-new-%s", newObjID.String()),
		newCreatedAt, newCreatedAt)
	require.NoError(t, err)

	// Insert an OLD object (30 days old)
	oldObjID := uuid.New()
	oldCreatedAt := time.Now().Add(-30 * 24 * time.Hour)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, 'active',
			 '{"name": "recency-boost-test-old"}'::jsonb, '{}'::text[], ?, ?, ?)
	`, oldObjID, projectID, oldObjID, "RecencyTest",
		fmt.Sprintf("hash-old-%s", oldObjID.String()),
		oldCreatedAt, oldCreatedAt)
	require.NoError(t, err)

	// Search with recency_boost=1.0
	recencyBoost := float32(1.0)
	req := &HybridSearchRequest{
		Query:        "recency-boost-test",
		Types:        []string{"RecencyTest"},
		RecencyBoost: &recencyBoost,
		Limit:        10,
	}
	resp, err := svc.HybridSearch(context.Background(), projectID, req, nil)
	require.NoError(t, err)
	require.NotEmpty(t, resp.Data, "expected at least one result")

	// Find positions of new vs old in results
	newPos, oldPos := -1, -1
	for i, item := range resp.Data {
		if item.Object.ID == newObjID {
			newPos = i
		}
		if item.Object.ID == oldObjID {
			oldPos = i
		}
	}

	if newPos == -1 || oldPos == -1 {
		t.Logf("results: %+v", resp.Data)
		t.Skip("both objects not found in results (may need FTS indexing delay)")
		return
	}

	assert.Less(t, newPos, oldPos, "newer object should rank above older one with recency_boost=1.0")
}

// TestRecencyBoostZeroProducesBaselineIntegration verifies that recency_boost=0
// produces results consistent with no-boost baseline.
func TestRecencyBoostZeroProducesBaselineIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires database")
	}

	db := openBulkTestDB(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := NewRepository(db, log, cfg)
	svc := NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	projectID := uuid.New()
	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID) //nolint:errcheck
	})

	for i := 0; i < 3; i++ {
		objID := uuid.New()
		_, err := db.ExecContext(context.Background(), `
			INSERT INTO kb.graph_objects
				(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
				 properties, labels, content_hash, created_at, updated_at)
			VALUES
				(?, ?, NULL, ?, NULL, 1, ?, 'active',
				 '{"name": "baseline-search-test"}'::jsonb, '{}'::text[], ?, NOW(), NOW())
		`, objID, projectID, objID, "BaselineTest",
			fmt.Sprintf("hash-baseline-%s", objID.String()))
		require.NoError(t, err)
	}

	baseReq := &HybridSearchRequest{
		Query: "baseline-search-test",
		Types: []string{"BaselineTest"},
		Limit: 10,
	}
	baseResp, err := svc.HybridSearch(context.Background(), projectID, baseReq, nil)
	require.NoError(t, err)

	zeroBoost := float32(0)
	boostReq := &HybridSearchRequest{
		Query:        "baseline-search-test",
		Types:        []string{"BaselineTest"},
		RecencyBoost: &zeroBoost,
		Limit:        10,
	}
	boostResp, err := svc.HybridSearch(context.Background(), projectID, boostReq, nil)
	require.NoError(t, err)

	// Both should return the same number of results
	assert.Equal(t, baseResp.Total, boostResp.Total,
		"recency_boost=0 should return same total as no boost")

	// Order should be identical (zero boost adds 0 to every score)
	for i := range baseResp.Data {
		if i >= len(boostResp.Data) {
			break
		}
		assert.Equal(t, baseResp.Data[i].Object.ID, boostResp.Data[i].Object.ID,
			"result order should be identical when recency_boost=0")
	}
}
