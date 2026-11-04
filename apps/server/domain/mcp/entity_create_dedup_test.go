package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// entityCreateDedupResult mirrors the JSON response of executeBatchCreateEntities
// for the fields exercised by the near-duplicate (check_similar) tests. The
// batch tool now returns the uniform envelope {ok, data: {results, message},
// meta: {created, failed, total, similar, similar_count}}.
type entityCreateDedupResult struct {
	OK   bool `json:"ok"`
	Data struct {
		Results []struct {
			OK    bool   `json:"ok"`
			Index int    `json:"index"`
			Error string `json:"error"`
		} `json:"results"`
	} `json:"data"`
	Meta struct {
		Created      int `json:"created"`
		Failed       int `json:"failed"`
		Total        int `json:"total"`
		SimilarCount int `json:"similar_count"`
		Similar      []struct {
			EntityID   string  `json:"entity_id"`
			Key        string  `json:"key"`
			Type       string  `json:"type"`
			Content    string  `json:"content"`
			Similarity float64 `json:"similarity"`
			Suggested  string  `json:"suggested"`
		} `json:"similar"`
	} `json:"meta"`
}

// newEntityCreateDedupService wires a real graph.Service (with a fake embedder
// that returns testVector() for every query) behind an mcp.Service, mirroring
// how production constructs them. No HTTP, no fx.
func newEntityCreateDedupService(t *testing.T, db bun.IDB) *Service {
	t.Helper()
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	graphRepo := graph.NewRepository(db, log, cfg)
	graphSvc := graph.NewService(graphRepo, log, nil, nil, &fakeEmbedder{vec: testVector()}, nil, nil, nil, nil, nil)
	return &Service{graphService: graphSvc, db: db, log: log}
}

// seedNoteObject inserts a Note graph object whose embedding_v2 equals
// testVector() (the same vector the fake embedder returns for any query), so
// VectorSearchByText yields distance 0 / score 1.0 for identical content.
// Returns the canonical object id.
func seedNoteObject(t *testing.T, db bun.IDB, projectID string, orgID string) string {
	t.Helper()
	ctx := context.Background()
	objID := uuid.NewString()
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, embedding_v2, created_at, updated_at)
		VALUES
			(?, ?, NULL, ?, NULL, 1, ?, 'active',
			 ?::jsonb, '{}'::text[], ?, ?::vector, ?, ?)
	`, objID, projectID, objID, "Note",
		`{"content":"The user really likes ice cream."}`,
		"content-hash-"+objID,
		pgutils.FormatVector(testVector()),
		now, now)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.projects WHERE id = ?", projectID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM kb.orgs WHERE id = ?", orgID)
	})
	return objID
}

// runEntityCreate executes executeBatchCreateEntities and decodes the JSON text
// response into an entityCreateDedupResult. It also asserts the uniform
// envelope contract: top-level ok/data/meta only, no legacy top-level fields.
func runEntityCreate(t *testing.T, svc *Service, ctx context.Context, projectID string, args map[string]any) entityCreateDedupResult {
	t.Helper()
	res, err := svc.executeBatchCreateEntities(ctx, projectID, args)
	text := toolResultText(t, res, err)

	var top map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &top))
	okVal, isBool := top["ok"].(bool)
	require.True(t, isBool, "top-level ok must be a bool, got %T", top["ok"])
	require.True(t, okVal, "successful entity-create must carry ok=true")
	for _, legacy := range []string{"success", "created", "failed", "total", "similar", "similar_count", "message"} {
		_, present := top[legacy]
		assert.False(t, present, "legacy field %q must not appear at top level", legacy)
	}
	require.Contains(t, top, "data", "top-level data must be present")
	require.Contains(t, top, "meta", "top-level meta must be present")

	var out entityCreateDedupResult
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	return out
}

// TestEntityCreateCheckSimilarFlagsNearDuplicate verifies that with
// check_similar=true and content identical to a seeded Note, the create is
// suppressed and the existing object is reported in the "similar" array.
func TestEntityCreateCheckSimilarFlagsNearDuplicate(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	objID := seedNoteObject(t, db, projectID, orgID)
	svc := newEntityCreateDedupService(t, db)

	out := runEntityCreate(t, svc, ctx, projectID, map[string]any{
		"entities": []any{
			map[string]any{
				"type": "Note",
				"properties": map[string]any{
					"content": "The user really likes ice cream.",
				},
			},
		},
		"check_similar": true,
	})

	assert.True(t, out.OK, "no failures, so ok must be true")
	assert.Equal(t, 0, out.Meta.Created, "near-duplicate must NOT be created")
	assert.Equal(t, 1, out.Meta.SimilarCount)
	require.Len(t, out.Meta.Similar, 1)
	assert.Equal(t, objID, out.Meta.Similar[0].EntityID, "entity_id must be the seeded canonical id")
	assert.Equal(t, "Note", out.Meta.Similar[0].Type)
	assert.Equal(t, "edit_existing", out.Meta.Similar[0].Suggested)
	assert.GreaterOrEqual(t, out.Meta.Similar[0].Similarity, 0.85, "identical vectors must score >= threshold")
}

// TestEntityCreateCheckSimilarCreatesWhenNoMatch verifies that check_similar
// does not block creates when no object of that type exists (score absent).
func TestEntityCreateCheckSimilarCreatesWhenNoMatch(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	_ = seedNoteObject(t, db, projectID, orgID)
	svc := newEntityCreateDedupService(t, db)

	out := runEntityCreate(t, svc, ctx, projectID, map[string]any{
		"entities": []any{
			map[string]any{
				"type":       "Person",
				"properties": map[string]any{"content": "Alice"},
			},
		},
		"check_similar": true,
	})

	assert.Equal(t, 1, out.Meta.Created, "no Person exists, so create must proceed")
	assert.Equal(t, 0, out.Meta.SimilarCount)
	assert.Empty(t, out.Meta.Similar)
}

// TestEntityCreateBackwardCompatNoCheckSimilar verifies that the default path
// (no check_similar arg) keeps old behavior: identical content still creates.
func TestEntityCreateBackwardCompatNoCheckSimilar(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	orgID, projectID := seedProject(t, db)
	_ = seedNoteObject(t, db, projectID, orgID)
	svc := newEntityCreateDedupService(t, db)

	out := runEntityCreate(t, svc, ctx, projectID, map[string]any{
		"entities": []any{
			map[string]any{
				"type": "Note",
				"properties": map[string]any{
					"content": "The user really likes ice cream.",
				},
			},
		},
	})

	assert.Equal(t, 1, out.Meta.Created, "gate is off without check_similar, so create proceeds")
	assert.Equal(t, 0, out.Meta.SimilarCount)
	assert.Empty(t, out.Meta.Similar)
}
