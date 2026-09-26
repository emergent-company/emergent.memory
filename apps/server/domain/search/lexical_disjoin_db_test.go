package search_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestLexicalSearchDisjoinsMultiTermQuery is the regression test for the chunks
// keyword leg of issue #996: websearch_to_tsquery's AND semantics zeroes a
// natural multi-term query when no single chunk holds every term. The disjoined
// fallback ORs the terms, so the text leg returns hits ordered by term coverage.
func TestLexicalSearchDisjoinsMultiTermQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "lexdisjoin")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Lexical Disjoin Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Lexical Disjoin Project",
	}, testutil.AdminUser.ID))

	docID := uuid.NewString()
	require.NoError(t, testutil.CreateTestDocument(ctx, db, testutil.TestDocument{
		ID:        docID,
		ProjectID: projectID,
	}))

	// Each chunk shares some but not all of the query's terms, so the AND query
	// is unsatisfiable while the terms are individually well represented.
	chunks := []testutil.TestChunk{
		{ID: uuid.NewString(), DocumentID: docID, ChunkIndex: 0, Text: "aksjeloven"},
		{ID: uuid.NewString(), DocumentID: docID, ChunkIndex: 1, Text: "innbetaling aksjekapital"},
		{ID: uuid.NewString(), DocumentID: docID, ChunkIndex: 2, Text: "innbetaling aksjekapital innskudd penger"},
	}
	for _, c := range chunks {
		require.NoError(t, testutil.CreateTestChunk(ctx, db, c))
	}

	const query = "aksjeloven innbetaling aksjekapital innskudd penger"

	repo := search.NewRepository(db, slog.Default())
	resp, err := repo.LexicalSearch(ctx, search.TextSearchParams{
		ProjectID: uuid.MustParse(projectID),
		Query:     query,
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Results, "multi-term query must return results via the disjoined fallback")

	// Term-coverage ordering: the 4-term chunk ranks first, the 1-term chunk last.
	require.Len(t, resp.Results, 3, "all three chunks should be returned")
	assert.Equal(t, chunks[2].ID, resp.Results[0].ID.String(), "highest-coverage chunk must rank first")
	assert.Equal(t, chunks[1].ID, resp.Results[1].ID.String(), "medium-coverage chunk must rank second")
	assert.Equal(t, chunks[0].ID, resp.Results[2].ID.String(), "lowest-coverage chunk must rank last")
}
