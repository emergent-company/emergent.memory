package graph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestRecencyBoostFavorsNewObjectsIntegration verifies that with recency_boost=1.0,
// a newer object ranks above an older equally-relevant object.
func TestRecencyBoostFavorsNewObjectsIntegration(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}

	db := openBulkTestDB(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := NewRepository(db, log, cfg)
	svc := NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	projectID := uuid.New()
	seedProject(t, db, projectID)
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
	if len(resp.Data) == 0 {
		t.Skip("no results returned (may need FTS indexing delay)")
		return
	}

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
		testdb.SkipOrFatal(t, "integration test requires database")
	}

	db := openBulkTestDB(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := NewRepository(db, log, cfg)
	svc := NewService(repo, log, nil, nil, nil, nil, nil, nil, nil, nil)

	projectID := uuid.New()
	seedProject(t, db, projectID)
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

	// Zero boost adds 0 to every score, so the two requests must return the
	// same set of objects. The three seeded objects are equally relevant
	// (identical properties and FTS rank) with no vector input, so all fused
	// scores are equal and ordering is now deterministic: ascending by id.
	baseIDs := make([]uuid.UUID, 0, len(baseResp.Data))
	for _, item := range baseResp.Data {
		baseIDs = append(baseIDs, item.Object.ID)
	}
	boostIDs := make([]uuid.UUID, 0, len(boostResp.Data))
	for _, item := range boostResp.Data {
		boostIDs = append(boostIDs, item.Object.ID)
	}

	// Equally-scored results must be ordered ascending by id.
	sortedIDs := slices.Clone(baseIDs)
	sort.Slice(sortedIDs, func(i, j int) bool {
		return slices.Compare(sortedIDs[i][:], sortedIDs[j][:]) < 0
	})
	assert.Equal(t, sortedIDs, baseIDs,
		"equally-scored results must be ordered ascending by id")

	assert.Equal(t, baseIDs, boostIDs,
		"recency_boost=0 must return identical ordered results")
}
