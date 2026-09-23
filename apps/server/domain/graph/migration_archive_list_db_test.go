package graph_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// These tests pin the migration_archive projection on Repository.List and the
// full-scan behaviour of Repository.ListAll. Both back the schema
// migrate/rollback paths, which read and write obj.MigrationArchive.
//
// Every test skips cleanly when Postgres is unavailable.

func setupArchiveListTest(t *testing.T) (context.Context, bun.IDB, string, *config.Config) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "archiveproj")
	t.Cleanup(testDB.Close)

	db := testDB.GetDB()
	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Archive Projection Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Archive Projection Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	cfg := *testDB.Config
	return ctx, db, projectID, &cfg
}

func insertArchiveObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, typ string, createdAt time.Time, archiveJSON, propsJSON string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, created_at, updated_at, migration_archive)
		VALUES (?, uuid(?), NULL, ?, NULL, 1, ?, 'active', ?::jsonb, '{}'::text[], ?, ?, ?::jsonb)
	`, id.String(), projectID, id.String(), typ, propsJSON, createdAt, createdAt, archiveJSON).Exec(ctx)
	if err != nil {
		t.Fatalf("insert %s object: %v", typ, err)
	}
	return id
}

// TestListMigrationArchiveProjectionOptIn proves the archive column is only
// selected when IncludeMigrationArchive is set — leaving the default (public
// list/search) projection free of archive JSON.
func TestListMigrationArchiveProjectionOptIn(t *testing.T) {
	ctx, db, projectID, cfg := setupArchiveListTest(t)
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)

	archive := `[{"from_version":"1.0.0","to_version":"2.0.0","dropped_data":{"legacy":"kept"}}]`
	withArchive := insertArchiveObject(t, ctx, db, projectID, "WithArchive",
		time.Now().UTC(), archive, `{"name":"a"}`)
	withoutArchive := insertArchiveObject(t, ctx, db, projectID, "WithoutArchive",
		time.Now().UTC().Add(-time.Minute), `[]`, `{"name":"b"}`)

	byID := func(objs []*graph.GraphObject) map[uuid.UUID]*graph.GraphObject {
		m := make(map[uuid.UUID]*graph.GraphObject, len(objs))
		for _, o := range objs {
			m[o.ID] = o
		}
		return m
	}

	// Default projection: column omitted, so the slice always scans empty.
	defaultObjs, err := repo.List(ctx, graph.ListParams{ProjectID: pid})
	require.NoError(t, err)
	require.Contains(t, byID(defaultObjs), withArchive)
	assert.Empty(t, byID(defaultObjs)[withArchive].MigrationArchive,
		"List without IncludeMigrationArchive must not project migration_archive")
	assert.Empty(t, byID(defaultObjs)[withoutArchive].MigrationArchive)

	// Opt-in projection: archive JSON is returned for the object that has one
	// and stays empty for the object that does not.
	optInObjs, err := repo.List(ctx, graph.ListParams{
		ProjectID:               pid,
		IncludeMigrationArchive: true,
	})
	require.NoError(t, err)
	require.Contains(t, byID(optInObjs), withArchive)
	require.Len(t, byID(optInObjs)[withArchive].MigrationArchive, 1)
	assert.Equal(t, "2.0.0", byID(optInObjs)[withArchive].MigrationArchive[0]["to_version"])
	assert.Empty(t, byID(optInObjs)[withoutArchive].MigrationArchive)
}

// TestListAllCoversEveryPage proves ListAll returns every matching object even
// when the project holds more objects than a single MaxListLimit-sized page.
func TestListAllCoversEveryPage(t *testing.T) {
	ctx, db, projectID, cfg := setupArchiveListTest(t)
	cfg.Graph.MaxListLimit = 3
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)
	require.Equal(t, 3, repo.MaxListLimit())

	const total = 7
	base := time.Now().UTC()
	for i := 0; i < total; i++ {
		archive := fmt.Sprintf(`[{"from_version":"1.0.0","to_version":"2.0.0","dropped_data":{"n":%d}}]`, i)
		insertArchiveObject(t, ctx, db, projectID, "PagedType",
			base.Add(-time.Duration(i)*time.Second), archive, fmt.Sprintf(`{"n":%d}`, i))
	}

	// A single List call is capped at MaxListLimit (+1 sentinel row).
	page, err := repo.List(ctx, graph.ListParams{ProjectID: pid, IncludeMigrationArchive: true})
	require.NoError(t, err)
	require.Len(t, page, 4, "one List page must be capped at MaxListLimit+1")

	// ListAll walks every page.
	all, err := repo.ListAll(ctx, graph.ListParams{ProjectID: pid, IncludeMigrationArchive: true})
	require.NoError(t, err)
	require.Len(t, all, total)
	for _, obj := range all {
		require.Lenf(t, obj.MigrationArchive, 1, "object %s lost its archive entry", obj.ID)
	}
}

// TestListOnlyWithMigrationArchive proves the opt-in archive predicate filters
// out empty-archive objects while still paging across more than one page.
func TestListOnlyWithMigrationArchive(t *testing.T) {
	ctx, db, projectID, cfg := setupArchiveListTest(t)
	cfg.Graph.MaxListLimit = 3
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)

	base := time.Now().UTC()
	for i := 0; i < 4; i++ {
		archive := fmt.Sprintf(`[{"from_version":"1.0.0","to_version":"2.0.0","dropped_data":{"n":%d}}]`, i)
		insertArchiveObject(t, ctx, db, projectID, "BoundType",
			base.Add(-time.Duration(i)*time.Second), archive, fmt.Sprintf(`{"n":%d}`, i))
	}
	for i := 0; i < 3; i++ {
		insertArchiveObject(t, ctx, db, projectID, "BoundType",
			base.Add(-time.Duration(10+i)*time.Second), `[]`, fmt.Sprintf(`{"empty":%d}`, i))
	}

	// Without the predicate every object is returned.
	all, err := repo.ListAll(ctx, graph.ListParams{ProjectID: pid, IncludeMigrationArchive: true})
	require.NoError(t, err)
	require.Len(t, all, 7)

	// With the predicate only archive-carrying objects are returned, and the
	// result still pages (4 objects against a MaxListLimit of 3).
	only, err := repo.ListAll(ctx, graph.ListParams{
		ProjectID:                pid,
		IncludeMigrationArchive:  true,
		OnlyWithMigrationArchive: true,
	})
	require.NoError(t, err)
	require.Len(t, only, 4)
	for _, obj := range only {
		require.Lenf(t, obj.MigrationArchive, 1, "only archive-carrying objects must be returned")
	}
}

// TestListEachMatchesListAll proves ListEach visits every page in the same order
// as ListAll without accumulating.
func TestListEachMatchesListAll(t *testing.T) {
	ctx, db, projectID, cfg := setupArchiveListTest(t)
	cfg.Graph.MaxListLimit = 3
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)

	base := time.Now().UTC()
	const total = 7
	for i := 0; i < total; i++ {
		archive := fmt.Sprintf(`[{"from_version":"1.0.0","to_version":"2.0.0","dropped_data":{"n":%d}}]`, i)
		insertArchiveObject(t, ctx, db, projectID, "EachType",
			base.Add(-time.Duration(i)*time.Second), archive, fmt.Sprintf(`{"n":%d}`, i))
	}

	all, err := repo.ListAll(ctx, graph.ListParams{ProjectID: pid, IncludeMigrationArchive: true})
	require.NoError(t, err)

	var got []*graph.GraphObject
	pages := 0
	err = repo.ListEach(ctx, graph.ListParams{ProjectID: pid, IncludeMigrationArchive: true}, func(page []*graph.GraphObject) error {
		pages++
		got = append(got, page...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, total)
	require.Greater(t, pages, 1, "must page across more than one page")

	require.Equal(t, len(all), len(got))
	for i := range all {
		require.Equal(t, all[i].ID, got[i].ID, "ListEach order must match ListAll at index %d", i)
	}
}

// TestListEachStopsOnFnError proves ListEach propagates an fn error immediately
// and does not fetch further pages.
func TestListEachStopsOnFnError(t *testing.T) {
	ctx, db, projectID, cfg := setupArchiveListTest(t)
	cfg.Graph.MaxListLimit = 3
	repo := graph.NewRepository(db, slog.Default(), cfg)
	pid := uuid.MustParse(projectID)

	base := time.Now().UTC()
	const total = 7
	for i := 0; i < total; i++ {
		archive := fmt.Sprintf(`[{"from_version":"1.0.0","to_version":"2.0.0","dropped_data":{"n":%d}}]`, i)
		insertArchiveObject(t, ctx, db, projectID, "StopType",
			base.Add(-time.Duration(i)*time.Second), archive, fmt.Sprintf(`{"n":%d}`, i))
	}

	sentinel := errors.New("stop")
	calls := 0
	err := repo.ListEach(ctx, graph.ListParams{ProjectID: pid, IncludeMigrationArchive: true}, func(page []*graph.GraphObject) error {
		calls++
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, calls, "ListEach must not fetch further pages after fn returns an error")
}
