package graph_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

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

// fakeEmbeddingEnqueuer records the object IDs enqueued for embedding. Its
// write methods are mutex-guarded so the bulk worker pool (issue #1573) can
// enqueue from several goroutines without a data race under -race.
type fakeEmbeddingEnqueuer struct {
	mu       sync.Mutex
	enqueued []string
}

func (f *fakeEmbeddingEnqueuer) EnqueueEmbedding(ctx context.Context, objectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueued = append(f.enqueued, objectID)
	return nil
}

func (f *fakeEmbeddingEnqueuer) EnqueueBatchEmbeddings(ctx context.Context, objectIDs []string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueued = append(f.enqueued, objectIDs...)
	return len(objectIDs), nil
}

func setupWorkConfigEnqueueTest(t *testing.T, objectSchemas map[string]agents.ObjectSchema) (context.Context, *graph.Service, uuid.UUID, *fakeEmbeddingEnqueuer, bun.IDB) {
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
	return ctx, svc, uuid.MustParse(projectID), enqueuer, db
}

func TestCreate_SkipEmbeddingsSuppressesEnqueue(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, map[string]agents.ObjectSchema{
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
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, map[string]agents.ObjectSchema{
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

// --- issue #1478: embedding enqueue is content-aware ---

// createForEnqueue creates an object (which always enqueues once) and resets
// the recorder so a test can assert only the enqueues caused by the operation
// under test.
func createForEnqueue(t *testing.T, ctx context.Context, svc *graph.Service, projectID uuid.UUID, enqueuer *fakeEmbeddingEnqueuer, objType, key string, props map[string]any) *graph.GraphObjectResponse {
	t.Helper()
	obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       objType,
		Key:        &key,
		Properties: props,
	}, nil)
	require.NoError(t, err)
	require.Len(t, enqueuer.enqueued, 1) // create always enqueues
	enqueuer.enqueued = nil
	return obj
}

// pv768 returns a pgvector literal with the 768 dimensions of embedding_v2.
func pv768() string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 768; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('1')
	}
	b.WriteByte(']')
	return b.String()
}

func TestPatch_NonSemanticPropertyChange_DoesNotEnqueue(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "p-1", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	_, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"version": "2"},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, enqueuer.enqueued)
}

func TestPatch_SemanticPropertyChange_Enqueues(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "p-2", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	_, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"description": "Now described"},
	}, nil)
	require.NoError(t, err)
	require.Len(t, enqueuer.enqueued, 1)
}

func TestCreateOrUpdate_PropertyOnlyChange_DoesNotEnqueue(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	key := "u-1"
	_, _, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "NormalType",
		Key:        &key,
		Properties: map[string]any{"name": "Original", "version": "1"},
	}, nil)
	require.NoError(t, err)
	require.Len(t, enqueuer.enqueued, 1)
	enqueuer.enqueued = nil

	_, _, err = svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "NormalType",
		Key:        &key,
		Properties: map[string]any{"name": "Original", "version": "2"},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, enqueuer.enqueued)
}

func TestBulkUpdateObjects_PropertyOnly_DoesNotEnqueue(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "b-1", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	resp, err := svc.BulkUpdateObjects(ctx, projectID, &graph.BulkUpdateObjectsRequest{
		Items: []graph.BulkUpdateObjectItem{
			{ID: obj.ID.String(), Properties: map[string]any{"version": "2"}},
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Success)
	require.Empty(t, enqueuer.enqueued)
}

// TestPatch_NonSemanticChange_CarriesEmbedding verifies that suppressing the
// redundant enqueue does not drop the object's embedding: the vector from the
// previous version is copied onto the new HEAD row.
func TestPatch_NonSemanticChange_CarriesEmbedding(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "c-1", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	_, err := db.NewRaw(
		`UPDATE kb.graph_objects SET embedding_v2 = ?::vector, embedding_updated_at = now() WHERE id = ?`,
		pv768(), obj.ID).Exec(ctx)
	require.NoError(t, err)

	head, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"version": "2"},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, enqueuer.enqueued)

	var emb string
	err = db.NewRaw(
		`SELECT COALESCE(embedding_v2::text, '') FROM kb.graph_objects WHERE id = ?`,
		head.ID).Scan(ctx, &emb)
	require.NoError(t, err)
	require.NotEmpty(t, emb, "new HEAD should inherit the previous embedding")
}

// TestPatch_CarriesEmbedding_WithoutTimestamp verifies that carry-over is
// governed by actual vector presence, not embedding_updated_at: a row with a
// vector but a NULL timestamp must still carry onto the new HEAD (#1488 review).
func TestPatch_CarriesEmbedding_WithoutTimestamp(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "c-2", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	_, err := db.NewRaw(
		`UPDATE kb.graph_objects SET embedding_v2 = ?::vector, embedding_updated_at = NULL WHERE id = ?`,
		pv768(), obj.ID).Exec(ctx)
	require.NoError(t, err)

	head, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"version": "2"},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, enqueuer.enqueued)

	var emb string
	err = db.NewRaw(
		`SELECT COALESCE(embedding_v2::text, '') FROM kb.graph_objects WHERE id = ?`,
		head.ID).Scan(ctx, &emb)
	require.NoError(t, err)
	require.NotEmpty(t, emb, "new HEAD should inherit a vector even when its timestamp is NULL")
}

// TestPatch_ProgrammaticTypedProperty_Enqueues verifies the guard does not miss
// a real content change when a programmatic caller supplies a non-JSON-decoded
// Go value such as []string (#1488 review).
func TestPatch_ProgrammaticTypedProperty_Enqueues(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "c-3", map[string]any{
		"name": "Original",
	})

	_, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"tags": []string{"alpha", "beta"}},
	}, nil)
	require.NoError(t, err)
	require.Len(t, enqueuer.enqueued, 1, "adding a typed array property must enqueue")
}
