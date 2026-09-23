package graph

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// ordering_tiebreak_integration_test.go verifies that every ORDER BY that feeds a
// LIMIT (or an ORDER BY ... LIMIT 1 in a subselect) uses a unique secondary key
// (the table PK `id`) so the selected subset is deterministic when the primary
// sort key ties. Rows are inserted in DESCENDING id order so the physical heap
// scan order differs from id-ascending order: without the id tie-break the LIMIT
// picks the first-scanned rows (heap order = descending), which makes the
// assertion fail; with the fix the selection is stable across runs.

// vec768One returns a 768-dimension embedding with a 1 in the first slot and 0s
// elsewhere. Every row in a similarity test shares this exact vector so the
// cosine distance is 0.0 for all of them (a perfect tie).
func vec768One() []float32 {
	v := make([]float32, 768)
	v[0] = 1
	return v
}

// vec768OneLiteral returns the pgvector literal for vec768One.
func vec768OneLiteral() string {
	return pgutils.FormatVector(vec768One())
}

// descendingUUIDs returns n UUIDs sorted descending (index 0 is the largest).
// Inserting them in slice order yields a heap scan order of descending id, with
// the smallest id inserted LAST.
func descendingUUIDs(n int) []uuid.UUID {
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = uuid.New()
	}
	sort.Slice(ids, func(i, j int) bool { return lessUUID(ids[j], ids[i]) })
	return ids
}

// ascendingSorted returns a freshly allocated ascending copy of ids.
func ascendingSorted(ids []uuid.UUID) []uuid.UUID {
	out := append([]uuid.UUID(nil), ids...)
	sort.Slice(out, func(i, j int) bool { return lessUUID(out[i], out[j]) })
	return out
}

// insertOrderingObject inserts a HEAD graph object with explicit id/timestamps,
// an optional last_accessed_at and an optional embedding_v2 vector.
func insertOrderingObject(t *testing.T, db *bun.DB, projectID, id uuid.UUID, typ string, createdAt time.Time, lastAccessedAt *time.Time, embedding *string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at, last_accessed_at, embedding_v2)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, 'active',
		        '{}'::jsonb, '{}'::text[], ?, ?, ?, ?, ?::vector)
	`, id, projectID, id, typ, []byte("ordering-test-hash"), createdAt, createdAt, lastAccessedAt, embedding)
	require.NoError(t, err)
}

// insertOrderingRelationship inserts a HEAD relationship with explicit id/timestamp
// and an optional embedding vector.
func insertOrderingRelationship(t *testing.T, db *bun.DB, projectID, id, srcID, dstID uuid.UUID, typ string, createdAt time.Time, embedding *string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, content_hash, created_at, embedding)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, ?,
		        '{}'::jsonb, ?, ?, ?::vector)
	`, id, projectID, id, typ, srcID, dstID, []byte("ordering-rel-hash"), createdAt, embedding)
	require.NoError(t, err)
}

// insertOrderingJob inserts a graph embedding job row with explicit id/timestamp.
func insertOrderingJob(t *testing.T, db *bun.DB, id, objectID uuid.UUID, status string, createdAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_embedding_jobs (id, object_id, status, created_at)
		VALUES (?, ?, ?, ?)
	`, id, objectID, status, createdAt)
	require.NoError(t, err)
}

// graphObjectsIDs returns the ids of all graph objects in a project, ascending.
func graphObjectsIDs(t *testing.T, db *bun.DB, projectID uuid.UUID) []uuid.UUID {
	t.Helper()
	var rows []struct {
		ID uuid.UUID `bun:"id"`
	}
	err := db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id").
		Where("project_id = ?", projectID).
		Order("id ASC").
		Scan(context.Background(), &rows)
	require.NoError(t, err)
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestBulkActionHardDelete_DeterministicSelection(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	for run := 0; run < 5; run++ {
		// Fresh project each run because hard delete is destructive.
		pid := uuid.New()
		seedProject(t, db, pid)
		rids := descendingUUIDs(12)
		for _, id := range rids {
			insertOrderingObject(t, db, pid, id, "HardDel", createdAt, nil, nil)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", pid)
			_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", pid)
		})

		matched, affected, err := repo.BulkActionByFilter(ctx, BulkActionParams{
			ProjectID: pid,
			Action:    BulkActionHardDelete,
			Limit:     5,
			Filter:    BulkActionFilter{Types: []string{"HardDel"}},
		})
		require.NoError(t, err)
		assert.Equal(t, 12, matched, "matched must count all 12 objects")
		assert.Equal(t, 5, affected, "affected must be exactly the capped 5")

		remaining := graphObjectsIDs(t, db, pid)
		require.Len(t, remaining, 7, "7 objects must survive")
		// The 5 smallest (ascending) ids were deleted; the 7 largest survive.
		want := ascendingSorted(rids)[5:]
		assert.Equal(t, want, remaining, "run %d: the 5 smallest ids must be deleted, 7 largest survive", run)
	}
}

func TestGetEdges_DeterministicOrder(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	rootCanonical := uuid.New()
	insertOrderingObject(t, db, projectID, rootCanonical, "EdgeRoot", time.Now().UTC(), nil, nil)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	outgoing := descendingUUIDs(6)
	incoming := descendingUUIDs(6)
	for _, id := range outgoing {
		insertOrderingRelationship(t, db, projectID, id, rootCanonical, uuid.New(), "links", createdAt, nil)
	}
	for _, id := range incoming {
		insertOrderingRelationship(t, db, projectID, id, uuid.New(), rootCanonical, "links", createdAt, nil)
	}

	wantIncoming := ascendingSorted(incoming)[:3]
	wantOutgoing := ascendingSorted(outgoing)[:3]

	var referenceIn, referenceOut []uuid.UUID
	for run := 0; run < 5; run++ {
		inc, out, err := repo.GetEdges(ctx, projectID, rootCanonical, GetEdgesParams{Limit: 3})
		require.NoError(t, err)
		require.Len(t, inc, 3)
		require.Len(t, out, 3)

		incIDs := relIDs(inc)
		outIDs := relIDs(out)

		if run == 0 {
			referenceIn = incIDs
			referenceOut = outIDs
		}
		assert.Equal(t, referenceIn, incIDs, "run %d: incoming ids must be identical", run)
		assert.Equal(t, referenceOut, outIDs, "run %d: outgoing ids must be identical", run)
		assert.True(t, isUUIDsSorted(incIDs), "incoming must be ascending")
		assert.True(t, isUUIDsSorted(outIDs), "outgoing must be ascending")
		assert.Equal(t, wantIncoming, incIDs, "incoming must be the 3 smallest ids")
		assert.Equal(t, wantOutgoing, outIDs, "outgoing must be the 3 smallest ids")
	}
}

func TestGetEdgesForObjects_DeterministicOrder(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	rootA := uuid.New()
	rootB := uuid.New()
	insertOrderingObject(t, db, projectID, rootA, "EdgeRoot", time.Now().UTC(), nil, nil)
	insertOrderingObject(t, db, projectID, rootB, "EdgeRoot", time.Now().UTC(), nil, nil)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	relIDs := descendingUUIDs(6)
	// src assignment: the 3 smallest ids from root A, the 3 largest from root B.
	srcOf := make(map[uuid.UUID]uuid.UUID)
	for i, id := range relIDs {
		if i >= 3 {
			srcOf[id] = rootA
		} else {
			srcOf[id] = rootB
		}
	}
	for _, id := range relIDs {
		insertOrderingRelationship(t, db, projectID, id, srcOf[id], uuid.New(), "links", createdAt, nil)
	}

	want := ascendingSorted(relIDs)[:4]

	for run := 0; run < 5; run++ {
		res, err := repo.GetEdgesForObjects(ctx, projectID, []uuid.UUID{rootA, rootB}, GetEdgesParams{Direction: "outgoing", Limit: 4})
		require.NoError(t, err)

		var union []uuid.UUID
		for _, entry := range res {
			for _, rel := range entry.Outgoing {
				union = append(union, rel.ID)
			}
		}
		require.Len(t, union, 4, "exactly 4 rels must be returned")
		got := ascendingSorted(union)
		assert.Equal(t, want, got, "run %d: union must be the 4 smallest ids", run)

		// Per-object grouping must be stable: A gets its 3 smallest, B gets 1.
		aOut := relIDsIn(res[rootA].Outgoing)
		bOut := relIDsIn(res[rootB].Outgoing)
		assert.Equal(t, ascendingSorted(relIDs)[:3], aOut, "root A grouping stable")
		assert.Equal(t, ascendingSorted(relIDs)[3:4], bOut, "root B grouping stable")
	}
}

func TestGetNeighborObjects_DeterministicTruncation(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	rootCanonical := uuid.New()
	insertOrderingObject(t, db, projectID, rootCanonical, "NeighborRoot", time.Now().UTC(), nil, nil)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	dstIDs := descendingUUIDs(5)
	srcIDs := descendingUUIDs(5)
	for _, id := range dstIDs {
		insertOrderingObject(t, db, projectID, id, "Dst", createdAt, nil, nil)
	}
	for _, id := range srcIDs {
		insertOrderingObject(t, db, projectID, id, "Src", createdAt, nil, nil)
	}
	// Distinct types defeat the incidental ascending-dst order the
	// uq_graph_relationships_head_main unique index (project_id, type, src_id,
	// dst_id) would otherwise give the outgoing scan; without the explicit
	// `dst_id ASC` ORDER BY the scan returns rows grouped by type, not by dst_id.
	for i, dst := range dstIDs {
		insertOrderingRelationship(t, db, projectID, uuid.New(), rootCanonical, dst, fmt.Sprintf("olink%d", i), createdAt, nil)
	}
	for i, src := range srcIDs {
		insertOrderingRelationship(t, db, projectID, uuid.New(), src, rootCanonical, fmt.Sprintf("ilink%d", i), createdAt, nil)
	}

	want := ascendingSorted(dstIDs)[:3]

	for run := 0; run < 5; run++ {
		objs, err := repo.GetNeighborObjects(ctx, projectID, rootCanonical, nil, 3)
		require.NoError(t, err)
		require.Len(t, objs, 3, "exactly 3 neighbors returned")

		got := make([]uuid.UUID, len(objs))
		for i, o := range objs {
			got[i] = o.CanonicalID
		}
		got = ascendingSorted(got)
		assert.Equal(t, want, got, "run %d: neighbors must be the 3 smallest dst ids", run)
	}
}

func TestFindSimilarObjectInBranch_TieDeterminism(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	emb := vec768OneLiteral()
	ids := descendingUUIDs(5)
	for _, id := range ids {
		insertOrderingObject(t, db, projectID, id, "SimilarObj", createdAt, nil, &emb)
	}
	smallest := ascendingSorted(ids)[0]

	for run := 0; run < 5; run++ {
		obj, dist, err := repo.FindSimilarObjectInBranch(ctx, projectID, nil, "SimilarObj", vec768One(), nil, 2.0)
		require.NoError(t, err)
		require.NotNil(t, obj, "a match must be found")
		assert.Equal(t, smallest, obj.ID, "run %d: must deterministically pick smallest id", run)
		assert.InDelta(t, 0.0, float64(dist), 1e-6)
	}
}

func TestFindSimilarRelationshipInBranch_TypeGate(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	src := uuid.New()
	dst := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	emb := vec768OneLiteral()
	ids := descendingUUIDs(5)
	// Distinct types per rel: uq_graph_relationships_head_main is unique on
	// (project_id, type, src_id, dst_id) for HEAD rows, so identical (src, dst)
	// pairs must differ by type. FindSimilarRelationshipInBranch filters on
	// (type, src, dst), so a query only ever returns the row whose type matches
	// the requested type (or nil when no such type exists).
	for i, id := range ids {
		insertOrderingRelationship(t, db, projectID, id, src, dst, fmt.Sprintf("links%d", i), createdAt, &emb)
	}

	// Each type returns exactly its own row (the type gate selects by type).
	for i, id := range ids {
		typ := fmt.Sprintf("links%d", i)
		rel, dist, err := repo.FindSimilarRelationshipInBranch(ctx, projectID, nil, src, dst, typ, vec768One(), nil, 2.0)
		require.NoError(t, err)
		require.NotNil(t, rel, "a match must be found for type %s", typ)
		assert.Equal(t, id, rel.ID, "type gate must return the row of the queried type %s", typ)
		assert.InDelta(t, 0.0, float64(dist), 1e-6)
	}

	// A type with no matching row yields nil (no cross-type match).
	rel, _, err := repo.FindSimilarRelationshipInBranch(ctx, projectID, nil, src, dst, "nonexistent", vec768One(), nil, 2.0)
	require.NoError(t, err)
	assert.Nil(t, rel, "a relationship of a different type must not match")
}

func TestGetMostAccessed_TieDeterminism(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	accessedAt := time.Now().UTC().Truncate(time.Microsecond)
	ids := descendingUUIDs(8)
	for _, id := range ids {
		insertOrderingObject(t, db, projectID, id, "AccessedObj", time.Now().UTC(), &accessedAt, nil)
	}
	want := ascendingSorted(ids)[:3]

	for run := 0; run < 5; run++ {
		objs, err := repo.GetMostAccessed(ctx, projectID, 3, 0)
		require.NoError(t, err)
		require.Len(t, objs, 3)
		got := objIDs(objs)
		assert.Equal(t, want, got, "run %d: must be the 3 smallest ids", run)
	}
}

func TestGetUnused_TieDeterminism(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	// 60 days ago — older than the 30-day cutoff.
	accessedAt := time.Now().UTC().AddDate(0, 0, -60).Truncate(time.Microsecond)
	ids := descendingUUIDs(8)
	for _, id := range ids {
		insertOrderingObject(t, db, projectID, id, "UnusedObj", time.Now().UTC(), &accessedAt, nil)
	}
	want := ascendingSorted(ids)[:3]

	for run := 0; run < 5; run++ {
		objs, err := repo.GetUnused(ctx, projectID, 3, 30)
		require.NoError(t, err)
		require.Len(t, objs, 3)
		got := objIDs(objs)
		assert.Equal(t, want, got, "run %d: must be the 3 smallest ids", run)
	}
}

func TestEmbeddingStatusExpr_TieDeterminism(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_embedding_jobs WHERE object_id IN (SELECT id FROM kb.graph_objects WHERE project_id = ?)", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	objectID := uuid.New()
	insertOrderingObject(t, db, projectID, objectID, "EmbeddingStatusObj", time.Now().UTC(), nil, nil)

	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	twoIDs := descendingUUIDs(2) // [larger, smaller]
	smallerJobID := twoIDs[1]    // 'failed'
	largerJobID := twoIDs[0]     // 'completed'

	// Insert the LARGER id FIRST so heap order (larger first) differs from the
	// id-ascending tie-break. Without the id tie-break the heap-first 'completed'
	// row yields 'missing'; with it the smaller-id 'failed' row wins.
	insertOrderingJob(t, db, largerJobID, objectID, "completed", createdAt)
	insertOrderingJob(t, db, smallerJobID, objectID, "failed", createdAt)

	for run := 0; run < 5; run++ {
		obj, err := repo.GetByID(ctx, projectID, objectID)
		require.NoError(t, err)
		assert.Equal(t, "failed", obj.EmbeddingStatus, "run %d: newest job tie must resolve to smallest id ('failed')", run)
	}
}

// TestGetByID_MultipleHeadFallbackDeterministic covers the sweep fix shared by
// GetByID / GetByIDIncludeDeleted / GetRelationshipByID: when several HEAD rows
// share a canonical_id (one per branch), the HEAD pick and the non-HEAD fallback
// must both resolve to the smallest id. It is the regression guard for the
// `supersedes_id ASC NULLS FIRST, id ASC` tie-break; without the `id ASC` the
// `[0]` fallback is heap-order dependent.
func TestGetByID_MultipleHeadFallbackDeterministic(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	createdAt := time.Now().UTC().Truncate(time.Microsecond)

	for run := 0; run < 5; run++ {
		projectID := uuid.New()
		seedProject(t, db, projectID)
		t.Cleanup(func() {
			_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
			_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
			_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
		})

		// Three HEAD objects sharing one canonical_id; inserted descending so
		// heap order differs from id-ascending.
		canonical := uuid.New()
		objIDs := descendingUUIDs(3)
		for _, id := range objIDs {
			insertHeadObjectWithCanonical(t, db, projectID, id, canonical, "MultiHead", createdAt)
		}
		wantObj := ascendingSorted(objIDs)[0]

		got, err := repo.GetByID(ctx, projectID, canonical)
		require.NoError(t, err)
		assert.Equal(t, wantObj, got.ID, "run %d: GetByID must pick the smallest-id HEAD", run)

		gotDel, err := repo.GetByIDIncludeDeleted(ctx, projectID, canonical)
		require.NoError(t, err)
		assert.Equal(t, wantObj, gotDel.ID, "run %d: GetByIDIncludeDeleted must pick the smallest-id HEAD", run)

		// Three HEAD relationships sharing one canonical_id; distinct (type,
		// src, dst) satisfies uq_graph_relationships_head_main.
		relCanonical := uuid.New()
		relIDs := descendingUUIDs(3)
		for i, id := range relIDs {
			insertHeadRelationshipWithCanonical(t, db, projectID, id, relCanonical,
				uuid.New(), uuid.New(), fmt.Sprintf("mrel%d", i), createdAt)
		}
		wantRel := ascendingSorted(relIDs)[0]

		gotRel, err := repo.GetRelationshipByID(ctx, projectID, relCanonical)
		require.NoError(t, err)
		assert.Equal(t, wantRel, gotRel.ID, "run %d: GetRelationshipByID must pick the smallest-id HEAD", run)
	}
}

// insertHeadObjectWithCanonical inserts a HEAD object whose canonical_id differs
// from its physical id, so a lookup by canonical_id can match several HEAD rows.
func insertHeadObjectWithCanonical(t *testing.T, db *bun.DB, projectID, id, canonicalID uuid.UUID, typ string, createdAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, content_hash, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, 'active',
		        '{}'::jsonb, '{}'::text[], ?, ?, ?)
	`, id, projectID, canonicalID, typ, []byte("ordering-head-hash"), createdAt, createdAt)
	require.NoError(t, err)
}

// insertHeadRelationshipWithCanonical inserts a HEAD relationship whose
// canonical_id differs from its physical id. Rows sharing a canonical_id must
// differ in (type, src_id, dst_id) to satisfy uq_graph_relationships_head_main.
func insertHeadRelationshipWithCanonical(t *testing.T, db *bun.DB, projectID, id, canonicalID, srcID, dstID uuid.UUID, typ string, createdAt time.Time) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, content_hash, created_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, ?, ?, '{}'::jsonb, ?, ?)
	`, id, projectID, canonicalID, typ, srcID, dstID, []byte("ordering-head-rel-hash"), createdAt)
	require.NoError(t, err)
}

// relIDs extracts relation ids from a relationship slice, preserving order.
func relIDs(rels []*GraphRelationship) []uuid.UUID {
	ids := make([]uuid.UUID, len(rels))
	for i, r := range rels {
		ids[i] = r.ID
	}
	return ids
}

// relIDsIn extracts relation ids from a relationship slice, ascending-sorted.
func relIDsIn(rels []*GraphRelationship) []uuid.UUID {
	return ascendingSorted(relIDs(rels))
}

// objIDs extracts object ids from an object slice, preserving order.
func objIDs(objs []*GraphObject) []uuid.UUID {
	ids := make([]uuid.UUID, len(objs))
	for i, o := range objs {
		ids[i] = o.ID
	}
	return ids
}

// selectIDColumn runs a raw SELECT that returns a single id column (aliased
// `id`) and returns the ids in result order. Used to assert which rows a bulk
// action mutated.
func selectIDColumn(t *testing.T, db *bun.DB, query string, args ...any) []uuid.UUID {
	t.Helper()
	var rows []struct {
		ID uuid.UUID `bun:"id"`
	}
	err := db.NewRaw(query, args...).Scan(context.Background(), &rows)
	require.NoError(t, err)
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// TestBulkActionUpdate_DeterministicLimit proves that every UPDATE-based bulk
// action honours its `limit` deterministically: with 12 same-created_at objects
// inserted in descending id order, a Limit of 5 must mutate exactly the 5
// SMALLEST ids (created_at ASC, id ASC), never an arbitrary heap-ordered subset.
func TestBulkActionUpdate_DeterministicLimit(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	type testCase struct {
		name      string
		action    string
		value     string
		props     map[string]any
		labels    []string
		preSeed   func(t *testing.T, pid uuid.UUID)
		selectSQL string
	}

	cases := []testCase{
		{
			name:      "update_status",
			action:    BulkActionUpdateStatus,
			value:     "archived",
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND status = 'archived' ORDER BY id ASC`,
		},
		{
			name:      "soft_delete",
			action:    BulkActionSoftDelete,
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND deleted_at IS NOT NULL ORDER BY id ASC`,
		},
		{
			name:      "merge_properties",
			action:    BulkActionMergeProperties,
			props:     map[string]any{"probe": "yes"},
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND properties->>'probe' = 'yes' ORDER BY id ASC`,
		},
		{
			name:      "replace_properties",
			action:    BulkActionReplaceProperties,
			props:     map[string]any{"probe": "yes"},
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND properties->>'probe' = 'yes' ORDER BY id ASC`,
		},
		{
			name:      "set_labels",
			action:    BulkActionSetLabels,
			labels:    []string{"marked"},
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND 'marked' = ANY(labels) ORDER BY id ASC`,
		},
		{
			name:      "add_labels",
			action:    BulkActionAddLabels,
			labels:    []string{"marked"},
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND 'marked' = ANY(labels) ORDER BY id ASC`,
		},
		{
			name:   "remove_labels",
			action: BulkActionRemoveLabels,
			labels: []string{"marked"},
			// Seed every object with the label so removal is observable.
			preSeed: func(t *testing.T, pid uuid.UUID) {
				_, err := db.ExecContext(ctx, `UPDATE kb.graph_objects SET labels = ARRAY['marked'] WHERE project_id = ?`, pid)
				require.NoError(t, err)
			},
			selectSQL: `SELECT id FROM kb.graph_objects WHERE project_id = ? AND NOT ('marked' = ANY(labels)) ORDER BY id ASC`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for iter := 0; iter < 3; iter++ {
				pid := uuid.New()
				seedProject(t, db, pid)
				createdAt := time.Now().UTC().Truncate(time.Microsecond)
				rids := descendingUUIDs(12)
				for _, id := range rids {
					insertOrderingObject(t, db, pid, id, "LimitTest", createdAt, nil, nil)
				}
				if tc.preSeed != nil {
					tc.preSeed(t, pid)
				}
				t.Cleanup(func() {
					_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", pid)
					_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", pid)
				})

				matched, affected, err := repo.BulkActionByFilter(ctx, BulkActionParams{
					ProjectID:  pid,
					Action:     tc.action,
					Value:      tc.value,
					Properties: tc.props,
					Labels:     tc.labels,
					Limit:      5,
					Filter:     BulkActionFilter{Types: []string{"LimitTest"}},
				})
				require.NoError(t, err)
				assert.Equal(t, 12, matched, "matched must count all 12 objects")
				assert.Equal(t, 5, affected, "affected must be exactly the capped 5")

				got := selectIDColumn(t, db, tc.selectSQL, pid)
				want := ascendingSorted(rids)[:5]
				assert.Equal(t, want, got, "iter %d: mutation must land on the 5 smallest ids, not the 7 largest", iter)
			}
		})
	}
}

// TestGetBranchRelationshipEmbedding_ReadsEmbeddingColumn proves the helper reads
// the real kb.graph_relationships.embedding column (added in migration 00011),
// not the non-existent embedding_v2 column it previously selected.
func TestGetBranchRelationshipEmbedding_ReadsEmbeddingColumn(t *testing.T) {
	db := openBulkTestDB(t)
	repo := newBulkTestRepo(t, db)
	ctx := context.Background()

	projectID := uuid.New()
	seedProject(t, db, projectID)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_relationships WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.graph_objects WHERE project_id = ?", projectID)
		_, _ = db.ExecContext(ctx, "DELETE FROM kb.projects WHERE id = ?", projectID)
	})

	src := uuid.New()
	dst := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	emb := vec768OneLiteral()

	relWithEmb := uuid.New()
	insertOrderingRelationship(t, db, projectID, relWithEmb, src, dst, "links", createdAt, &emb)

	relNoEmb := uuid.New()
	insertOrderingRelationship(t, db, projectID, relNoEmb, src, dst, "links2", createdAt, nil)

	vec, err := repo.GetBranchRelationshipEmbedding(ctx, relWithEmb)
	require.NoError(t, err)
	require.Len(t, vec, 768, "embedded relationship must return a 768-dim vector")
	assert.Equal(t, float32(1), vec[0], "first component must be the seeded 1")

	vec2, err := repo.GetBranchRelationshipEmbedding(ctx, relNoEmb)
	require.NoError(t, err)
	assert.Nil(t, vec2, "un-embedded relationship must return nil, nil")
}
