package graph_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func setupFTSRelaxTest(t *testing.T) (context.Context, bun.IDB, uuid.UUID, *config.Config) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "ftsrelax")
	if err != nil {
		t.Skipf("skipping: test database unavailable: %v", err)
	}
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "FTS Relax Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "FTS Relax Project",
	}, testutil.AdminUser.ID))

	cfg := *testDB.Config
	return ctx, db, uuid.MustParse(projectID), &cfg
}

// insertKeyedObject inserts a HEAD graph object with an explicit key and
// properties so that kb.update_graph_objects_fts builds the tsvector from the
// same inputs as production.
func insertKeyedObject(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, typ, key, properties string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, uuid(?), NULL, ?, NULL, 1, ?, ?, 'active', ?::jsonb, '{}'::text[], now(), now())
	`, id.String(), projectID.String(), id.String(), typ, key, properties).Exec(ctx)
	require.NoError(t, err)
	return id
}

// countStrictMatches counts objects matched by the raw query without any
// relaxation, i.e. what FTSSearch would have returned before this change.
func countStrictMatches(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, query string) int {
	t.Helper()
	var rows []struct {
		Count int `bun:"count"`
	}
	require.NoError(t, db.NewRaw(`
		SELECT count(*) AS count FROM kb.graph_objects
		WHERE project_id = uuid(?) AND fts @@ websearch_to_tsquery('simple', ?)
	`, projectID.String(), query).Scan(ctx, &rows))
	require.Len(t, rows, 1)
	return rows[0].Count
}

// TestFTSSearchRelaxesUnsatisfiableIdentifier covers the failure mode where a
// document identifier in the query is rewritten by websearch_to_tsquery into a
// phrase that cannot match, silently zeroing an otherwise valid search.
//
// The law's key is a composite identifier ("lov/1997-06-13-44") which the
// default parser indexes as a single lexeme, so its numeric components are
// absent from the tsvector and the generated phrase is unsatisfiable.
func TestFTSSearchRelaxesUnsatisfiableIdentifier(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	lawID := insertKeyedObject(t, ctx, db, projectID, "Law", "lov/1997-06-13-44",
		`{"title":"Lov om aksjeselskaper (aksjeloven)"}`)

	const rawQuery = "aksjeloven lov 1997-06-13-44"

	// Guard the premise: the strict query really does match nothing here, so the
	// assertions below can only pass because of the relaxed fallback.
	if got := countStrictMatches(t, ctx, db, projectID, rawQuery); got != 0 {
		t.Fatalf("premise changed: strict tsquery matches %d object(s), so this test no longer proves the fallback is what found the law", got)
	}

	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     rawQuery,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1, "relaxed fallback should return the law for a query containing its identifier")
	assert.Equal(t, lawID, results[0].Object.ID)
	require.NotNil(t, results[0].Object.Key)
	assert.Equal(t, "lov/1997-06-13-44", *results[0].Object.Key)
}

// TestFTSSearchUsesStrictQueryWhenItMatches asserts the fallback does not engage
// when the strict query already returns results.
func TestFTSSearchUsesStrictQueryWhenItMatches(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	insertKeyedObject(t, ctx, db, projectID, "Law", "lov/1997-06-13-44",
		`{"title":"Lov om aksjeselskaper (aksjeloven)"}`)

	const query = "aksjeloven lov"
	require.NotZero(t, countStrictMatches(t, ctx, db, projectID, query),
		"premise changed: the strict query should match this object")

	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     query,
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Object.Key)
	assert.Equal(t, "lov/1997-06-13-44", *results[0].Object.Key)
}

// TestFTSSearchRelaxedFallbackRespectsProjectScope asserts the fallback cannot
// leak objects from another project.
func TestFTSSearchRelaxedFallbackRespectsProjectScope(t *testing.T) {
	ctx, db, projectID, cfg := setupFTSRelaxTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)

	// A second project holding an object that would match the relaxed query.
	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "FTS Relax Other Org"))
	otherProjectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    otherProjectID,
		OrgID: orgID,
		Name:  "FTS Relax Other Project",
	}, testutil.AdminUser.ID))
	insertKeyedObject(t, ctx, db, uuid.MustParse(otherProjectID), "Law", "lov/1997-06-13-44",
		`{"title":"Lov om aksjeselskaper (aksjeloven)"}`)

	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     "aksjeloven lov 1997-06-13-44",
		Limit:     10,
	})
	require.NoError(t, err)
	assert.Empty(t, results, "relaxed fallback must stay scoped to the requested project")
}
