package graph

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embeddingStatus mirrors the SQL CASE expression in embeddingStatusExpr
// (repository.go). Keep the two in sync. It is the deterministic reference for
// the per-object embedding status precedence:
//
//  1. hasVector                        -> embedded
//  2. latestJobStatus pending/processing/failed/dead_letter -> that status
//  3. latestJobStatus completed/cancelled (or no job)       -> missing
func embeddingStatus(hasVector bool, latestJobStatus string) string {
	if hasVector {
		return "embedded"
	}
	switch latestJobStatus {
	case "pending", "processing", "failed", "dead_letter":
		return latestJobStatus
	default:
		return "missing"
	}
}

func TestEmbeddingStatus_Pure(t *testing.T) {
	tests := []struct {
		name            string
		hasVector       bool
		latestJobStatus string
		want            string
	}{
		{"vector wins over failed job", true, "failed", "embedded"},
		{"vector wins over processing job", true, "processing", "embedded"},
		{"vector wins over dead_letter job", true, "dead_letter", "embedded"},
		{"vector with no job", true, "", "embedded"},
		{"pending job", false, "pending", "pending"},
		{"processing job", false, "processing", "processing"},
		{"failed job", false, "failed", "failed"},
		{"dead_letter job", false, "dead_letter", "dead_letter"},
		{"completed job has no vector", false, "completed", "missing"},
		{"cancelled job has no vector", false, "cancelled", "missing"},
		{"no vector and no job", false, "", "missing"},
		{"unknown job status", false, "weird", "missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, embeddingStatus(tt.hasVector, tt.latestJobStatus))
		})
	}
}

// TestEmbeddingStatusExprStructure pins the SQL CASE expression so that a
// regression in the derivation rules is caught without a live database. It
// asserts the expression covers every status value and the vector precedence.
func TestEmbeddingStatusExprStructure(t *testing.T) {
	expr := embeddingStatusExpr

	// Vector precedence first.
	require.Contains(t, expr, "embedding_v2 IS NOT NULL")
	require.Contains(t, expr, "'embedded'")

	// Latest-job subquery, ordered by created_at DESC.
	require.Contains(t, expr, "kb.graph_embedding_jobs")
	require.Contains(t, expr, "ORDER BY j.created_at DESC")
	require.Contains(t, expr, "LIMIT 1")

	// Every passthrough status is handled.
	for _, s := range []string{"pending", "processing", "failed", "dead_letter"} {
		require.Contains(t, expr, "'"+s+"'", "expr must map status %q", s)
	}

	// Falls through to 'missing' (completed/cancelled/no row).
	require.Contains(t, expr, "ELSE 'missing'")
	require.Contains(t, expr, "COALESCE(")
}

// TestToResponse_SerializesEmbeddingFields proves embedding_status and
// embedding_updated_at are emitted in the JSON output of ToResponse.
func TestToResponse_SerializesEmbeddingFields(t *testing.T) {
	now := time.Date(2026, 2, 19, 10, 0, 0, 0, time.UTC)

	obj := &GraphObject{
		ID:                 uuid.New(),
		ProjectID:          uuid.New(),
		CanonicalID:        uuid.New(),
		Version:            1,
		Type:               "Person",
		Properties:         map[string]any{},
		Labels:             []string{},
		CreatedAt:          now,
		EmbeddingStatus:    "embedded",
		EmbeddingUpdatedAt: &now,
	}

	resp := obj.ToResponse()
	require.Equal(t, "embedded", resp.EmbeddingStatus)
	require.NotNil(t, resp.EmbeddingUpdatedAt)
	require.Equal(t, now, *resp.EmbeddingUpdatedAt)

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "embedding_status")
	assert.Contains(t, raw, "embedding_updated_at")

	var status string
	require.NoError(t, json.Unmarshal(raw["embedding_status"], &status))
	assert.Equal(t, "embedded", status)

	// embedding_updated_at is serialized as an RFC3339 timestamp string.
	var ts string
	require.NoError(t, json.Unmarshal(raw["embedding_updated_at"], &ts))
	assert.Equal(t, now.Format(time.RFC3339Nano), ts)
}

// TestToResponse_OmitsEmbeddingStatusWhenEmpty proves a non-embedded object
// (no status) does not emit an empty embedding_status field.
func TestToResponse_OmitsEmbeddingStatusWhenEmpty(t *testing.T) {
	obj := &GraphObject{
		ID:          uuid.New(),
		ProjectID:   uuid.New(),
		CanonicalID: uuid.New(),
		Version:     1,
		Type:        "Person",
		Properties:  map[string]any{},
		Labels:      []string{},
		CreatedAt:   time.Now(),
	}

	data, err := json.Marshal(obj.ToResponse())
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "embedding_status", "empty embedding_status must be omitted")
	assert.NotContains(t, raw, "embedding_updated_at", "nil embedding_updated_at must be omitted")
}
