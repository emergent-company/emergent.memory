package search

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVectorSearch_FloorDropsWeakSemanticMatches pins the relevance floor for the
// semantic (vector) path (#244): a candidate whose cosine similarity is below the
// floor must not surface, while one above it survives.
func TestVectorSearch_FloorDropsWeakSemanticMatches(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()

	mock.ExpectQuery(`[\s\S]*LIMIT 20`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "parent_chunk_id", "score"}).
			AddRow(uuid.New(), uuid.New(), 0, "weak", nil, 0.3).
			AddRow(uuid.New(), uuid.New(), 0, "strong", nil, 0.8))

	resp, err := repo.VectorSearch(context.Background(), TextSearchParams{
		ProjectID:     projectID,
		Vector:        []float32{0.1, 0.2},
		Limit:         20,
		MinSimilarity: 0.5,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "strong", resp.Results[0].Text)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestHybridSearch_LexicalMatchOutranksVectorOnly pins the ranking rule for the
// hybrid path (#244): a chunk that matched the query lexically (contains the
// term) must rank above a chunk the vector leg matched on similarity alone, even
// when the vector-only chunk has a higher raw similarity.
func TestHybridSearch_LexicalMatchOutranksVectorOnly(t *testing.T) {
	repo, mock := newChunkSearchRepo(t)
	projectID := uuid.New()
	lexID := uuid.New()
	vecID := uuid.New()

	// Lexical leg: one term match.
	mock.ExpectQuery(`[\s\S]*LIMIT 40`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "parent_chunk_id", "score"}).
			AddRow(lexID, uuid.New(), 0, "contains the query term", nil, 0.5))
	// Vector leg: a different chunk with higher similarity but no term match.
	mock.ExpectQuery(`[\s\S]*LIMIT 40`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "document_id", "chunk_index", "text", "parent_chunk_id", "score"}).
			AddRow(vecID, uuid.New(), 0, "semantically similar but no term", nil, 0.99))

	resp, err := repo.HybridSearch(context.Background(), TextSearchParams{
		ProjectID: projectID,
		Query:     "needle",
		Vector:    []float32{0.1, 0.2},
		Limit:     20,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, lexID, resp.Results[0].ID, "lexical term match must rank first")
	assert.Equal(t, vecID, resp.Results[1].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}
