package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// countingDetailSelectHook counts SELECT queries that read the full
// graph_objects detail columns (the ones that select the properties JSONB). It
// is the write-path-perf probe: after the Patch identity-resolution change, a
// single-property patch must issue exactly one full detail-column read.
type countingDetailSelectHook struct {
	count int
}

func (h *countingDetailSelectHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	q := event.Query
	if event.Operation() == "SELECT" &&
		strings.Contains(q, "graph_objects") &&
		strings.Contains(q, "properties") {
		h.count++
	}
	return ctx
}

func (h *countingDetailSelectHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {}

func TestPatch_SinglePropertyFetch(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)

	bdb, ok := db.(*bun.DB)
	require.True(t, ok, "test DB must be a *bun.DB when no transaction is active")
	hook := &countingDetailSelectHook{}
	bdb.AddQueryHook(hook)

	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "f-1", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	// Reset after the create (which itself writes a properties column) so the
	// counter reflects only the Patch.
	hook.count = 0

	_, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"version": "2"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, 1, hook.count,
		"a single-property patch must do exactly one full detail-column SELECT (identity resolution must not load properties)")
}

func TestPatch_NoOpName_DoesNotCreateVersion(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "np-1", map[string]any{
		"name":    "Original",
		"version": "1",
	})

	resp, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"name": "Original"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, obj.ID, resp.ID, "no-op patch must return the existing HEAD id")
	require.Equal(t, obj.Version, resp.Version, "no-op patch must not bump the version")

	var count int
	err = db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE canonical_id = ?`, obj.CanonicalID).Scan(ctx, &count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "no-op patch must not create a new version row")
}

func TestPatch_AssigneeOnly_ReusesContentHash(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "ao-1", map[string]any{
		"name": "Original",
	})

	var prevHash []byte
	err := db.NewRaw(`SELECT content_hash FROM kb.graph_objects WHERE id = ?`, obj.ID).Scan(ctx, &prevHash)
	require.NoError(t, err)
	require.NotNil(t, prevHash)

	alice := "alice"
	head, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{Assignee: &alice}, nil)
	require.NoError(t, err)

	require.NotEqual(t, obj.ID, head.ID, "assignee-only patch must create a new HEAD")
	require.Equal(t, obj.Version+1, head.Version, "assignee-only patch must bump the version")

	var newHash []byte
	err = db.NewRaw(`SELECT content_hash FROM kb.graph_objects WHERE id = ?`, head.ID).Scan(ctx, &newHash)
	require.NoError(t, err)
	require.NotNil(t, newHash, "new HEAD must carry a content hash")
	require.Equal(t, prevHash, newHash, "assignee-only patch must reuse the previous content hash")
}

func TestCreateOrUpdate_NoOpSameProps_DoesNotCreateVersion(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)
	key := "upsert-nop-1"

	first, created, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "NormalType",
		Key:        &key,
		Properties: map[string]any{"name": "Original", "version": "1"},
	}, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.Len(t, enqueuer.enqueued, 1)
	enqueuer.enqueued = nil

	second, created, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "NormalType",
		Key:        &key,
		Properties: map[string]any{"name": "Original", "version": "1"},
	}, nil)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, second.ID, "no-op upsert must return the same HEAD id")
	require.Equal(t, first.Version, second.Version, "no-op upsert must not bump the version")

	var count int
	err = db.NewRaw(`SELECT count(*) FROM kb.graph_objects WHERE canonical_id = ?`, first.CanonicalID).Scan(ctx, &count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "no-op upsert must not create a new version row")
}

// TestPatch_NamespaceOnlyKeyChange_DoesNotEnqueue locks the #1488 contract that
// an enqueue happens only when the embedding-relevant text changes. The
// embedding builder strips namespace prefixes, so renaming `ns:x` -> `other:x`
// produces identical text: the embedding must be carried, not regenerated. This
// guards the propsUnchanged fast path against over-enqueueing on raw key
// inequality.
func TestPatch_NamespaceOnlyKeyChange_DoesNotEnqueue(t *testing.T) {
	ctx, svc, projectID, enqueuer, db := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "ns:kx-1", map[string]any{
		"name": "Original",
	})

	_, err := db.NewRaw(
		`UPDATE kb.graph_objects SET embedding_v2 = ?::vector, embedding_updated_at = now() WHERE id = ?`,
		pv768(), obj.ID).Exec(ctx)
	require.NoError(t, err)

	newKey := "other:kx-1"
	head, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Key: &newKey,
	}, nil)
	require.NoError(t, err)
	require.Empty(t, enqueuer.enqueued, "namespace-only key change must not re-enqueue")

	var emb string
	err = db.NewRaw(
		`SELECT COALESCE(embedding_v2::text, '') FROM kb.graph_objects WHERE id = ?`,
		head.ID).Scan(ctx, &emb)
	require.NoError(t, err)
	require.NotEmpty(t, emb, "new HEAD should inherit the previous embedding")
}

// TestPatch_SemanticKeyChange_Enqueues is the converse of the namespace-only
// case: when a key change alters the embedding text (the suffix is part of it),
// the fast path must still enqueue a fresh embedding.
func TestPatch_SemanticKeyChange_Enqueues(t *testing.T) {
	ctx, svc, projectID, enqueuer, _ := setupWorkConfigEnqueueTest(t, nil)
	obj := createForEnqueue(t, ctx, svc, projectID, enqueuer, "NormalType", "ns:kx-2", map[string]any{
		"name": "Original",
	})

	newKey := "ns:ky-2"
	_, err := svc.Patch(ctx, projectID, obj.ID, &graph.PatchGraphObjectRequest{
		Key: &newKey,
	}, nil)
	require.NoError(t, err)
	require.Len(t, enqueuer.enqueued, 1, "a text-changing key rename must enqueue")
}
