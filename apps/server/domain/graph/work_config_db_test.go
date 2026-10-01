package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// fakeSchemaProvider is a static SchemaProvider returning a fixed type config,
// so the write path can be exercised against per-type work config without a
// real schema pack.
type fakeSchemaProvider struct {
	objectSchemas map[string]agents.ObjectSchema
}

func (f *fakeSchemaProvider) GetProjectSchemas(ctx context.Context, projectID string) (*graph.ExtractionSchemas, error) {
	return &graph.ExtractionSchemas{ObjectSchemas: f.objectSchemas, RelationshipSchemas: map[string]agents.RelationshipSchema{}}, nil
}

func (f *fakeSchemaProvider) InvalidateProjectCache(projectID string) {}

func setupWorkConfigTest(t *testing.T, objectSchemas map[string]agents.ObjectSchema) (context.Context, *graph.Service, uuid.UUID) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_work_config")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Work Config Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Work Config Project",
	}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, &fakeSchemaProvider{objectSchemas: objectSchemas}, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)
	return ctx, svc, uuid.MustParse(projectID)
}

func TestCreate_RejectsStatusOutsideAllowedSet(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{
		"BoardTask": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
			BoardEnabled:    true,
			AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
		}},
	})

	shipped := "shipped"
	_, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "BoardTask",
		Status: &shipped,
	}, nil)
	require.Error(t, err)

	// The object is unchanged: no BoardTask row exists.
	bt := "BoardTask"
	list, err := svc.List(ctx, graph.ListParams{Type: &bt})
	require.NoError(t, err)
	require.NotNil(t, list.Total)
	require.Equal(t, 0, *list.Total)
}

func TestCreate_AcceptsStatusInAllowedSet(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{
		"BoardTask": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
			BoardEnabled:    true,
			AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
		}},
	})

	ready := "ready"
	key := "bt-1"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "BoardTask",
		Key:    &key,
		Status: &ready,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "ready", *obj.Status)
}

func TestCreate_UnconfiguredTypeUnconstrained(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, nil)

	shipped := "shipped"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "FreeForm",
		Status: &shipped,
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "shipped", *obj.Status)
}

func TestPatch_RejectsStatusOutsideAllowedSet(t *testing.T) {
	ctx, svc, projectID := setupWorkConfigTest(t, map[string]agents.ObjectSchema{
		"BoardTask": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
			BoardEnabled:    true,
			AllowedStatuses: []string{"ready", "in_progress", "review", "done"},
		}},
	})

	ready := "ready"
	key := "bt-2"
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "BoardTask",
		Key:    &key,
		Status: &ready,
	}, nil)
	require.NoError(t, err)

	shipped := "shipped"
	_, err = svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{Status: &shipped}, nil)
	require.Error(t, err)

	// Object unchanged: still ready at version 1.
	head, err := svc.GetByID(ctx, projectID, obj.ID, false)
	require.NoError(t, err)
	require.Equal(t, "ready", *head.Status)
}

// fakeEmbeddingEnqueuer records the object IDs enqueued for embedding.
type fakeEmbeddingEnqueuer struct {
	enqueued []string
}

func (f *fakeEmbeddingEnqueuer) EnqueueEmbedding(ctx context.Context, objectID string) error {
	f.enqueued = append(f.enqueued, objectID)
	return nil
}

func (f *fakeEmbeddingEnqueuer) EnqueueBatchEmbeddings(ctx context.Context, objectIDs []string) (int, error) {
	f.enqueued = append(f.enqueued, objectIDs...)
	return len(objectIDs), nil
}

func setupWorkConfigEnqueueTest(t *testing.T, objectSchemas map[string]agents.ObjectSchema) (context.Context, *graph.Service, uuid.UUID, *fakeEmbeddingEnqueuer) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_work_config_enqueue")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Work Config Enqueue Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Work Config Enqueue Project",
	}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	enqueuer := &fakeEmbeddingEnqueuer{}
	svc := graph.NewService(repo, log, &fakeSchemaProvider{objectSchemas: objectSchemas}, nil, nil, enqueuer, nil, graph.NoopEventSink{}, nil, nil)
	return ctx, svc, uuid.MustParse(projectID), enqueuer
}

func TestCreate_SkipEmbeddingsSuppressesEnqueue(t *testing.T) {
	ctx, svc, projectID, enqueuer := setupWorkConfigEnqueueTest(t, map[string]agents.ObjectSchema{
		"BoardTask": {ObjectTypeWorkConfig: agents.ObjectTypeWorkConfig{
			BoardEnabled:   true,
			SkipEmbeddings: true,
		}},
	})

	ready := "ready"
	key := "bt-3"
	_, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "BoardTask",
		Key:    &key,
		Status: &ready,
	}, nil)
	require.NoError(t, err)
	require.Empty(t, enqueuer.enqueued)
}

func TestCreate_NoSkipEmbeddingsEnqueues(t *testing.T) {
	ctx, svc, projectID, enqueuer := setupWorkConfigEnqueueTest(t, map[string]agents.ObjectSchema{
		"NormalType": {},
	})

	ready := "ready"
	_, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:   "NormalType",
		Status: &ready,
	}, nil)
	require.NoError(t, err)
	require.Len(t, enqueuer.enqueued, 1)
}
