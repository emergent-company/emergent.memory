package search_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestLexicalSearchExcludesSupersededDocumentVersions is the regression test for
// #494: the chunk search legs join kb.documents but must only consider canonical
// HEAD rows (supersedes_id IS NULL). Chunks belonging to a superseded document
// version carry valid FTS content and would otherwise be returned as live hits.
//
// The two document rows are inserted directly with raw SQL (not
// documents.CreateVersion) so the test owns the physical layout and does not
// depend on version-creation side effects such as retired-chunk deletion.
func TestLexicalSearchExcludesSupersededDocumentVersions(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "searchdochead")
	t.Cleanup(testDB.Close)
	db := testDB.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Search Document Head Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Search Document Head Project",
	}, testutil.AdminUser.ID))

	canonicalID := uuid.NewString()
	v2ID := uuid.NewString()
	v1ID := uuid.NewString()

	insertDocument := func(id string, supersedesID *string, version int, content string) {
		_, err := db.NewRaw(`
			INSERT INTO kb.documents (
				id, project_id, canonical_id, supersedes_id, version, content,
				created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW())
		`, id, projectID, canonicalID, supersedesID, version, content).Exec(ctx)
		require.NoError(t, err)
	}

	// HEAD first: the prior version's supersedes_id FK points forward at it.
	insertDocument(v2ID, nil, 2, "current document version")
	insertDocument(v1ID, &v2ID, 1, "superseded document version")

	// Both chunks carry the same distinctive query term; only the extra words
	// differ, so a HEAD filter is the only thing that can separate them.
	v1ChunkID := uuid.NewString()
	v2ChunkID := uuid.NewString()
	require.NoError(t, testutil.CreateTestChunk(ctx, db, testutil.TestChunk{
		ID:         v1ChunkID,
		DocumentID: v1ID,
		ChunkIndex: 0,
		Text:       "zygomorphic obsoletebeta",
	}))
	require.NoError(t, testutil.CreateTestChunk(ctx, db, testutil.TestChunk{
		ID:         v2ChunkID,
		DocumentID: v2ID,
		ChunkIndex: 0,
		Text:       "zygomorphic currentalpha",
	}))

	repo := search.NewRepository(db, slog.Default())
	resp, err := repo.LexicalSearch(ctx, search.TextSearchParams{
		ProjectID: uuid.MustParse(projectID),
		Query:     "zygomorphic",
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1, "chunks from superseded document versions must be excluded")
	assert.Equal(t, v2ChunkID, resp.Results[0].ID.String(), "only the canonical HEAD chunk may be returned")
}
