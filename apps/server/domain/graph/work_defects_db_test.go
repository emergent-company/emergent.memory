package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

func setupTwoProjectService(t *testing.T) (context.Context, *graph.Service, uuid.UUID, uuid.UUID, bun.IDB) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_board_types")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Board Org"))
	projectA := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{ID: projectA, OrgID: orgID, Name: "Project A"}, testutil.AdminUser.ID))
	projectB := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{ID: projectB, OrgID: orgID, Name: "Project B"}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)
	return ctx, svc, uuid.MustParse(projectA), uuid.MustParse(projectB), db
}

// TestListWorkObjectsByStatus_PerProjectBoardTypes verifies board enablement is
// per-project: enabling a board type in project A must not make project B's
// objects of that type work items (the multi-tenant leak).
func TestListWorkObjectsByStatus_PerProjectBoardTypes(t *testing.T) {
	ctx, svc, projectA, projectB, db := setupTwoProjectService(t)

	// A single schema pack declaring BoardTask board-enabled.
	schemaID := uuid.NewString()
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_schemas (id, name, version, object_type_schemas, relationship_type_schemas, created_at, updated_at)
		VALUES (?::uuid, 'board-pack', '1.0.0', ?::jsonb, '{}'::jsonb, NOW(), NOW())
	`, schemaID, `[{"name":"BoardTask","boardEnabled":true}]`).Exec(ctx)
	require.NoError(t, err)

	// Only project A has the pack assigned (active, non-removed).
	assignmentID := uuid.NewString()
	_, err = db.NewRaw(`
		INSERT INTO kb.project_schemas (id, project_id, schema_id, active, installed_at, created_at, updated_at)
		VALUES (?::uuid, ?::uuid, ?::uuid, true, NOW(), NOW(), NOW())
	`, assignmentID, projectA, schemaID).Exec(ctx)
	require.NoError(t, err)

	createBoardTask := func(pid uuid.UUID, key string) string {
		ready := "ready"
		obj, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type:   "BoardTask",
			Key:    &key,
			Status: &ready,
		}, nil)
		require.NoError(t, err)
		return obj.CanonicalID.String()
	}
	canonicalA := createBoardTask(projectA, "a-1")
	createBoardTask(projectB, "b-1")

	heads, err := svc.ListWorkObjectsByStatus(ctx, "", "ready", time.Time{}, 100)
	require.NoError(t, err)
	require.Len(t, heads, 1)
	require.Equal(t, canonicalA, heads[0].CanonicalID)

	// Project-scoped listing only returns the assigned project's objects.
	headsA, err := svc.ListWorkObjectsByStatus(ctx, projectA.String(), "ready", time.Time{}, 100)
	require.NoError(t, err)
	require.Len(t, headsA, 1)

	headsB, err := svc.ListWorkObjectsByStatus(ctx, projectB.String(), "ready", time.Time{}, 100)
	require.NoError(t, err)
	require.Empty(t, headsB)
}

// TestCreateOrUpdate_PreservesAndAppliesAssignee verifies the existing-object
// upsert branch applies an assignee-only change and preserves the current lane
// on unrelated upserts (no assignee field).
func TestCreateOrUpdate_PreservesAndAppliesAssignee(t *testing.T) {
	ctx, svc, projectID, _ := setupWorkTransitionTest(t)

	key := "upsert-assignee"
	assignee := "researcher"
	ready := "ready"
	obj, created, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:     "ResearchRequest",
		Key:      &key,
		Status:   &ready,
		Assignee: &assignee,
	}, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, assignee, *obj.Assignee)

	// Assignee-only upsert must be detected and applied.
	newAssignee := "reviewer"
	obj2, created2, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:     "ResearchRequest",
		Key:      &key,
		Assignee: &newAssignee,
	}, nil)
	require.NoError(t, err)
	require.False(t, created2)
	require.Equal(t, newAssignee, *obj2.Assignee)

	// Unrelated upsert (no assignee) must preserve the current lane.
	obj3, _, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "ResearchRequest",
		Key:        &key,
		Properties: map[string]any{"topic": "x"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, newAssignee, *obj3.Assignee)

	// Sanity: the HEAD still carries the latest version.
	head, err := svc.GetHeadObject(ctx, projectID.String(), obj3.CanonicalID.String())
	require.NoError(t, err)
	require.Equal(t, newAssignee, head.Assignee)
}

// TestTransitionWorkObject_PreservesHeadMetadata verifies a work transition
// keeps the HEAD's namespace, extraction confidence, schema version, migration
// archive, and review flags instead of dropping them.
func TestTransitionWorkObject_PreservesHeadMetadata(t *testing.T) {
	ctx, svc, projectID, db := setupWorkTransitionTest(t)

	key := "meta-1"
	status := "ready"
	ns := "some.namespace"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:      "ResearchRequest",
		Key:       &key,
		Status:    &status,
		Namespace: &ns,
	}, nil)
	require.NoError(t, err)

	// Stamp metadata that Create cannot set directly.
	_, err = db.NewRaw(`
		UPDATE kb.graph_objects SET
			extraction_confidence = 0.95,
			schema_version = '1.2.3',
			migration_archive = '[{"field":"old","value":1}]'::jsonb,
			needs_review = true
		WHERE id = ?`,
		obj.ID).Exec(ctx)
	require.NoError(t, err)

	ok, err := svc.TransitionWorkObject(ctx, projectID.String(), obj.CanonicalID.String(), graph.WorkObjectTransition{
		FromStatus: "ready",
		ToStatus:   "in_progress",
	})
	require.NoError(t, err)
	require.True(t, ok)

	var nsOut *string
	var conf float64
	var schemaVersion *string
	var archive string
	var needsReview bool
	require.NoError(t, db.NewRaw(`
		SELECT namespace, extraction_confidence::float8, schema_version, migration_archive::text, needs_review
		FROM kb.graph_objects WHERE canonical_id = ?::uuid AND supersedes_id IS NULL`,
		obj.CanonicalID.String()).Scan(ctx, &nsOut, &conf, &schemaVersion, &archive, &needsReview))

	require.NotNil(t, nsOut)
	require.Equal(t, ns, *nsOut)
	require.InDelta(t, 0.95, conf, 0.0001)
	require.NotNil(t, schemaVersion)
	require.Equal(t, "1.2.3", *schemaVersion)
	require.Contains(t, archive, "old")
	require.True(t, needsReview)
}
