package graph

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// newBulkUpsertService builds a graph Service with all optional providers nil,
// which is sufficient for the CreateOrUpdate / UpsertRelationship paths (schema
// validation and embedding enqueue are best-effort no-ops when providers are nil).
func newBulkUpsertService(t *testing.T, repo *Repository) *Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	return NewService(repo, log, nil, nil, nil, nil, nil, NoopEventSink{}, nil, nil)
}

// insertRawObject inserts a graph_object row with explicit key/branch/supersedes
// control so repo-level tests can seed HEAD and non-HEAD rows without going
// through the service layer.
func insertRawObject(t *testing.T, db *bun.DB, projectID uuid.UUID, objType, key string, branchID *uuid.UUID, supersedesID *uuid.UUID, version int) uuid.UUID {
	t.Helper()
	seedProject(t, db, projectID)
	id := uuid.New()
	canonicalID := id
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES
			(?, ?, ?, ?, ?, ?, ?, ?, 'active',
			 '{}'::jsonb, '{}'::text[], ?, NOW(), NOW())
	`, id, projectID, branchID, canonicalID, supersedesID, version, objType, key, "hash-"+id.String())
	require.NoError(t, err)
	return id
}

func cleanupObjects(t *testing.T, db *bun.DB, projectIDs ...uuid.UUID) {
	t.Cleanup(func() {
		for _, pid := range projectIDs {
			db.ExecContext(context.Background(), "DELETE FROM kb.graph_relationships WHERE project_id = ?", pid) //nolint:errcheck
			db.ExecContext(context.Background(), "DELETE FROM kb.graph_objects WHERE project_id = ?", pid)       //nolint:errcheck
		}
	})
}

// =============================================================================
// Bulk object upserts
// =============================================================================

func TestBulkUpsertObjects_CreateThenUpdate(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	svc := newBulkUpsertService(t, repo)
	ctx := context.Background()
	projectID := uuid.New()
	seedProject(t, db, projectID)
	cleanupObjects(t, db, projectID)

	key := "obj-1"
	first, err := svc.BulkUpsertObjects(ctx, projectID, &BulkUpsertObjectsRequest{
		Items: []CreateGraphObjectRequest{{Type: "Thing", Key: strPtr(key), Properties: map[string]any{"name": "one"}}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, first.Success)
	require.Equal(t, 0, first.Failed)
	require.Len(t, first.Results, 1)
	assert.Equal(t, 0, first.Results[0].Index, "result index must match request position")
	require.True(t, first.Results[0].Success)
	require.True(t, first.Results[0].Created, "first upsert must create")
	require.NotNil(t, first.Results[0].Object)
	v1 := first.Results[0].Object.Version
	require.Equal(t, 1, v1)

	second, err := svc.BulkUpsertObjects(ctx, projectID, &BulkUpsertObjectsRequest{
		Items: []CreateGraphObjectRequest{{Type: "Thing", Key: strPtr(key), Properties: map[string]any{"name": "two"}}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, second.Success)
	require.Len(t, second.Results, 1)
	assert.Equal(t, 0, second.Results[0].Index, "result index must match request position")
	require.True(t, second.Results[0].Success)
	require.False(t, second.Results[0].Created, "second upsert with changed properties must update, not create")
	require.NotNil(t, second.Results[0].Object)
	assert.Equal(t, v1+1, second.Results[0].Object.Version, "version must increment on update")

	head, err := repo.FindHeadByTypeAndKey(ctx, repo.db, projectID, nil, "Thing", key)
	require.NoError(t, err)
	require.NotNil(t, head)
	assert.Equal(t, "two", head.Properties["name"], "head must reflect the latest properties")
}

func TestBulkUpsertObjects_PerItemIsolation(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	svc := newBulkUpsertService(t, repo)
	ctx := context.Background()
	projectID := uuid.New()
	seedProject(t, db, projectID)
	cleanupObjects(t, db, projectID)

	resp, err := svc.BulkUpsertObjects(ctx, projectID, &BulkUpsertObjectsRequest{
		Items: []CreateGraphObjectRequest{
			{Type: "Thing", Key: strPtr("k1")},
			{Type: "Thing", Key: nil}, // invalid: missing key
			{Type: "Thing", Key: strPtr("k3")},
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, resp.Success)
	require.Equal(t, 1, resp.Failed)
	require.Len(t, resp.Results, 3)

	assert.True(t, resp.Results[0].Success, "valid item 0 must succeed")
	assert.True(t, resp.Results[0].Created, "valid item 0 must be created")
	assert.False(t, resp.Results[1].Success, "invalid item 1 must fail")
	require.NotNil(t, resp.Results[1].Error)
	assert.True(t, resp.Results[2].Success, "valid item 2 must succeed")
	assert.True(t, resp.Results[2].Created, "valid item 2 must be created")
	for i := range resp.Results {
		assert.Equal(t, i, resp.Results[i].Index, "result %d index must match request position", i)
	}

	// Both valid items must have persisted.
	head1, err := repo.FindHeadByTypeAndKey(ctx, repo.db, projectID, nil, "Thing", "k1")
	require.NoError(t, err)
	require.NotNil(t, head1, "valid item k1 must be persisted")
	head3, err := repo.FindHeadByTypeAndKey(ctx, repo.db, projectID, nil, "Thing", "k3")
	require.NoError(t, err)
	require.NotNil(t, head3, "valid item k3 must be persisted")
}

func TestBulkUpsertObjects_ProjectScoping(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	svc := newBulkUpsertService(t, repo)
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	seedProject(t, db, projectA)
	seedProject(t, db, projectB)
	cleanupObjects(t, db, projectA, projectB)

	key := "scoped"
	_, err := svc.BulkUpsertObjects(ctx, projectA, &BulkUpsertObjectsRequest{
		Items: []CreateGraphObjectRequest{{Type: "Thing", Key: strPtr(key), Properties: map[string]any{"name": "A"}}},
	}, nil)
	require.NoError(t, err)

	_, err = svc.BulkUpsertObjects(ctx, projectB, &BulkUpsertObjectsRequest{
		Items: []CreateGraphObjectRequest{{Type: "Thing", Key: strPtr(key), Properties: map[string]any{"name": "B"}}},
	}, nil)
	require.NoError(t, err)

	headA, err := repo.FindHeadByTypeAndKey(ctx, repo.db, projectA, nil, "Thing", key)
	require.NoError(t, err)
	require.NotNil(t, headA)
	assert.Equal(t, "A", headA.Properties["name"], "project A object must be unchanged")

	headB, err := repo.FindHeadByTypeAndKey(ctx, repo.db, projectB, nil, "Thing", key)
	require.NoError(t, err)
	require.NotNil(t, headB, "project B must have its own row")
	assert.Equal(t, "B", headB.Properties["name"])
	assert.NotEqual(t, headA.CanonicalID, headB.CanonicalID, "projects must not share an object")
}

// =============================================================================
// Bulk relationship upserts
// =============================================================================

func TestBulkUpsertRelationships_CreateThenIdempotent(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	svc := newBulkUpsertService(t, repo)
	ctx := context.Background()
	projectID := uuid.New()
	seedProject(t, db, projectID)
	cleanupObjects(t, db, projectID)

	src, created, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{Type: "Node", Key: strPtr("node-a")}, nil)
	require.NoError(t, err)
	require.True(t, created)
	dst, created, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{Type: "Node", Key: strPtr("node-b")}, nil)
	require.NoError(t, err)
	require.True(t, created)

	rel := CreateGraphRelationshipRequest{
		Type:       "REL",
		SrcID:      src.CanonicalID,
		DstID:      dst.CanonicalID,
		Properties: map[string]any{"v": 1},
	}

	first, err := svc.BulkUpsertRelationships(ctx, projectID, &BulkUpsertRelationshipsRequest{Items: []CreateGraphRelationshipRequest{rel}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, first.Success)
	require.Len(t, first.Results, 1)
	assert.Equal(t, 0, first.Results[0].Index, "result index must match request position")
	require.True(t, first.Results[0].Success)
	require.True(t, first.Results[0].Created, "first relationship upsert must create")
	require.NotNil(t, first.Results[0].Relationship)

	// Unchanged re-upsert of the EXACT same relationship: must report created=false
	// (regression test — a version-1 relationship re-upserted unchanged is NOT a
	// new creation), version must stay 1, and there must still be exactly one HEAD.
	same, err := svc.BulkUpsertRelationships(ctx, projectID, &BulkUpsertRelationshipsRequest{Items: []CreateGraphRelationshipRequest{rel}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, same.Success)
	require.Len(t, same.Results, 1)
	assert.Equal(t, 0, same.Results[0].Index, "result index must match request position")
	require.True(t, same.Results[0].Success)
	require.False(t, same.Results[0].Created, "unchanged re-upsert must not report created")
	require.NotNil(t, same.Results[0].Relationship)
	assert.Equal(t, 1, same.Results[0].Relationship.Version)

	var unchangedHeadCount int
	err = db.NewSelect().TableExpr("kb.graph_relationships").
		Where("project_id = ?", projectID).
		Where("type = ?", "rel").
		Where("src_id = ?", src.CanonicalID).
		Where("dst_id = ?", dst.CanonicalID).
		Where("supersedes_id IS NULL").
		ColumnExpr("count(*)").
		Scan(ctx, &unchangedHeadCount)
	require.NoError(t, err)
	assert.Equal(t, 1, unchangedHeadCount, "unchanged re-upsert must not create a duplicate HEAD relationship")

	// Re-upsert with changed properties: bumps to a new version (created=false),
	// and must not produce a second HEAD row.
	rel.Properties = map[string]any{"v": 2}
	second, err := svc.BulkUpsertRelationships(ctx, projectID, &BulkUpsertRelationshipsRequest{Items: []CreateGraphRelationshipRequest{rel}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, second.Success)
	require.Len(t, second.Results, 1)
	assert.Equal(t, 0, second.Results[0].Index, "result index must match request position")
	require.True(t, second.Results[0].Success)
	require.False(t, second.Results[0].Created, "re-upsert with changed properties must not report created")
	require.NotNil(t, second.Results[0].Relationship)
	assert.Equal(t, 2, second.Results[0].Relationship.Version)

	// Exactly one HEAD row for (type, src, dst).
	var headCount int
	err = db.NewSelect().TableExpr("kb.graph_relationships").
		Where("project_id = ?", projectID).
		Where("type = ?", "rel").
		Where("src_id = ?", src.CanonicalID).
		Where("dst_id = ?", dst.CanonicalID).
		Where("supersedes_id IS NULL").
		ColumnExpr("count(*)").
		Scan(ctx, &headCount)
	require.NoError(t, err)
	assert.Equal(t, 1, headCount, "re-upsert must not create a duplicate HEAD relationship")
}

func TestBulkUpsertRelationships_PerItemIsolation(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	svc := newBulkUpsertService(t, repo)
	ctx := context.Background()
	projectID := uuid.New()
	seedProject(t, db, projectID)
	cleanupObjects(t, db, projectID)

	src, _, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{Type: "Node", Key: strPtr("node-a")}, nil)
	require.NoError(t, err)
	dst, _, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{Type: "Node", Key: strPtr("node-b")}, nil)
	require.NoError(t, err)

	missing := uuid.New()

	resp, err := svc.BulkUpsertRelationships(ctx, projectID, &BulkUpsertRelationshipsRequest{
		Items: []CreateGraphRelationshipRequest{
			{Type: "REL", SrcID: src.CanonicalID, DstID: dst.CanonicalID},
			{Type: "REL", SrcID: missing, DstID: dst.CanonicalID}, // non-existent src
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Success)
	require.Equal(t, 1, resp.Failed)
	require.Len(t, resp.Results, 2)

	assert.True(t, resp.Results[0].Success, "valid relationship must succeed")
	assert.False(t, resp.Results[1].Success, "relationship with missing endpoint must fail")
	require.NotNil(t, resp.Results[1].Error)
	for i := range resp.Results {
		assert.Equal(t, i, resp.Results[i].Index, "result %d index must match request position", i)
	}
}

func TestBulkUpsertRelationships_EmptyTypeValidation(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	svc := newBulkUpsertService(t, repo)
	ctx := context.Background()
	projectID := uuid.New()
	seedProject(t, db, projectID)
	cleanupObjects(t, db, projectID)

	src, _, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{Type: "Node", Key: strPtr("node-a")}, nil)
	require.NoError(t, err)
	dst, _, err := svc.CreateOrUpdate(ctx, projectID, &CreateGraphObjectRequest{Type: "Node", Key: strPtr("node-b")}, nil)
	require.NoError(t, err)

	resp, err := svc.BulkUpsertRelationships(ctx, projectID, &BulkUpsertRelationshipsRequest{
		Items: []CreateGraphRelationshipRequest{
			{Type: "", SrcID: src.CanonicalID, DstID: dst.CanonicalID}, // invalid: empty type
			{Type: "REL", SrcID: src.CanonicalID, DstID: dst.CanonicalID},
		},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, resp.Success)
	require.Equal(t, 1, resp.Failed)
	require.Len(t, resp.Results, 2)

	assert.False(t, resp.Results[0].Success, "empty-type item must fail")
	require.NotNil(t, resp.Results[0].Error)
	assert.True(t, resp.Results[1].Success, "valid item must succeed")
	assert.True(t, resp.Results[1].Created, "valid item must be created")
	for i := range resp.Results {
		assert.Equal(t, i, resp.Results[i].Index, "result %d index must match request position", i)
	}
}

// =============================================================================
// Endpoint limit constant
// =============================================================================

func TestBulkUpsertMaxItems_Constant(t *testing.T) {
	assert.Equal(t, 100, bulkUpsertMaxItems)
}

// =============================================================================
// Repository: FindHeadsByTypeAndKeys
// =============================================================================

func TestFindHeadsByTypeAndKeys(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()
	projectID := uuid.New()
	otherProjectID := uuid.New()
	cleanupObjects(t, db, projectID, otherProjectID)

	// HEAD rows for the target project.
	headK1 := insertRawObject(t, db, projectID, "Thing", "k1", nil, nil, 1)
	insertRawObject(t, db, projectID, "Thing", "k2", nil, nil, 1)
	insertRawObject(t, db, projectID, "Thing", "k3", nil, nil, 1)

	// A superseded (non-HEAD) row sharing key k1 — must be excluded.
	supersededID := uuid.New()
	insertRawObject(t, db, projectID, "Thing", "k1", nil, &supersededID, 2)

	// A row in another project sharing key k1 — must be excluded by scoping.
	insertRawObject(t, db, otherProjectID, "Thing", "k1", nil, nil, 1)

	// A branch-scoped row sharing key k1 on the target project — must be excluded
	// when the main graph is requested (branchID nil).
	branchID := uuid.New()
	insertRawObject(t, db, projectID, "Thing", "k1", &branchID, nil, 1)

	objs, err := repo.FindHeadsByTypeAndKeys(ctx, repo.db, projectID, nil, "Thing", []string{"k1", "k2", "k3"})
	require.NoError(t, err)

	var keys []string
	var contentHashSeen bool
	var k1IDs []uuid.UUID
	for _, o := range objs {
		keys = append(keys, *o.Key)
		if len(o.ContentHash) > 0 {
			contentHashSeen = true
		}
		if o.Key != nil && *o.Key == "k1" {
			k1IDs = append(k1IDs, o.ID)
		}
	}
	assert.ElementsMatch(t, []string{"k1", "k2", "k3"}, keys, "only HEAD rows matching keys are returned")
	assert.True(t, contentHashSeen, "ContentHash must be populated")
	assert.Len(t, k1IDs, 1, "k1 must resolve to exactly one HEAD row")
	assert.Equal(t, headK1, k1IDs[0], "k1 HEAD must be the non-superseded, main-graph row")

	// Empty keys -> empty slice, no error.
	empty, err := repo.FindHeadsByTypeAndKeys(ctx, repo.db, projectID, nil, "Thing", nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// Cross-type assertion: TypeA and TypeB share the same key; querying objType
	// "TypeA" must return exactly the TypeA row (type filter must apply).
	typeAID := insertRawObject(t, db, projectID, "TypeA", "shared", nil, nil, 1)
	insertRawObject(t, db, projectID, "TypeB", "shared", nil, nil, 1)

	typeAHeads, err := repo.FindHeadsByTypeAndKeys(ctx, repo.db, projectID, nil, "TypeA", []string{"shared"})
	require.NoError(t, err)
	require.Len(t, typeAHeads, 1, "type filter must restrict to TypeA")
	assert.Equal(t, typeAID, typeAHeads[0].ID)
}

// =============================================================================
// Repository: multi-key List/Count
// =============================================================================

func TestListMultiKeySearchIntegration(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()
	projectID := uuid.New()
	cleanupObjects(t, db, projectID)

	insertRawObject(t, db, projectID, "Thing", "k1", nil, nil, 1)
	insertRawObject(t, db, projectID, "Thing", "k2", nil, nil, 1)
	insertRawObject(t, db, projectID, "Thing", "k3", nil, nil, 1)

	objs, err := repo.List(ctx, ListParams{ProjectID: projectID, Keys: []string{"k1", "k3"}, Limit: 10})
	require.NoError(t, err)

	var keys []string
	for _, o := range objs {
		keys = append(keys, *o.Key)
	}
	assert.ElementsMatch(t, []string{"k1", "k3"}, keys, "List must return only k1 and k3")

	count, err := repo.Count(ctx, ListParams{ProjectID: projectID, Keys: []string{"k1", "k3"}})
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Count must count only k1 and k3")
}
