package graph_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestObjectVersioningPreservesNamespace is the regression test for #1916:
// object versioning must carry Namespace forward exactly like relationship
// versioning already does. A namespaced object must survive a soft-delete →
// upsert (with or without the namespace supplied) and a soft-delete → restore
// without being silently un-namespaced or duplicated (new canonical_id), while
// an un-namespaced object must stay un-namespaced through the same round-trips.
func TestObjectVersioningPreservesNamespace(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	// readHead returns the namespace, version, and deleted_at of the current
	// HEAD row (supersedes_id IS NULL) for a canonical object.
	readHead := func(t *testing.T, canonicalID uuid.UUID) (namespace *string, version int, deletedAt *time.Time) {
		t.Helper()
		var ns *string
		var ver int
		var del *time.Time
		require.NoError(t, db.NewRaw(
			`SELECT namespace, version, deleted_at FROM kb.graph_objects
			 WHERE canonical_id = ? AND supersedes_id IS NULL`,
			canonicalID,
		).Scan(ctx, &ns, &ver, &del))
		return ns, ver, del
	}

	create := func(t *testing.T, typ, key string, ns *string) *graph.GraphObjectResponse {
		t.Helper()
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type:      typ,
			Key:       &key,
			Namespace: ns,
		}, nil)
		require.NoError(t, err)
		return resp
	}

	softDelete := func(t *testing.T, id uuid.UUID) {
		t.Helper()
		require.NoError(t, svc.Delete(ctx, projectID, id, nil, nil, nil))
	}

	// create → tombstone → restore leaves three versions on the same canonical_id.
	const wantVersionAfterRestore = 3

	t.Run("namespaced: upsert without namespace restores original namespace", func(t *testing.T) {
		ns := "team/alpha"
		key := "ns-key-upsert-plain"
		created := create(t, "NsUpsert", key, &ns)
		softDelete(t, created.ID)

		restored, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "NsUpsert",
			Key:  &key,
		}, nil)
		require.NoError(t, err)
		require.False(t, isNew, "upsert must restore the tombstone, not create a new object")
		require.Equal(t, created.CanonicalID, restored.CanonicalID, "canonical_id must be preserved")
		require.NotNil(t, restored.Namespace, "namespace must not be dropped")
		require.Equal(t, ns, *restored.Namespace)

		gotNS, gotVer, gotDel := readHead(t, created.CanonicalID)
		require.NotNil(t, gotNS)
		require.Equal(t, ns, *gotNS)
		require.Equal(t, wantVersionAfterRestore, gotVer)
		require.Nil(t, gotDel)
	})

	t.Run("namespaced: upsert with namespace restores instead of duplicating", func(t *testing.T) {
		ns := "team/beta"
		key := "ns-key-upsert-ns"
		created := create(t, "NsUpsert", key, &ns)
		softDelete(t, created.ID)

		restored, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type:      "NsUpsert",
			Key:       &key,
			Namespace: &ns,
		}, nil)
		require.NoError(t, err)
		require.False(t, isNew, "upsert with namespace must restore the tombstone, not create a duplicate")
		require.Equal(t, created.CanonicalID, restored.CanonicalID, "canonical_id must be preserved (no duplicate)")
		require.NotNil(t, restored.Namespace)
		require.Equal(t, ns, *restored.Namespace)

		gotNS, gotVer, gotDel := readHead(t, created.CanonicalID)
		require.NotNil(t, gotNS)
		require.Equal(t, ns, *gotNS)
		require.Equal(t, wantVersionAfterRestore, gotVer)
		require.Nil(t, gotDel)
	})

	t.Run("namespaced: restore preserves namespace", func(t *testing.T) {
		ns := "team/gamma"
		key := "ns-key-restore"
		created := create(t, "NsRestore", key, &ns)
		softDelete(t, created.ID)

		restored, err := svc.Restore(ctx, projectID, created.ID, nil)
		require.NoError(t, err)
		require.NotNil(t, restored.Namespace, "restore must not drop the namespace")
		require.Equal(t, ns, *restored.Namespace)

		gotNS, gotVer, gotDel := readHead(t, created.CanonicalID)
		require.NotNil(t, gotNS)
		require.Equal(t, ns, *gotNS)
		require.Equal(t, wantVersionAfterRestore, gotVer)
		require.Nil(t, gotDel)
	})

	t.Run("un-namespaced: upsert without namespace stays un-namespaced", func(t *testing.T) {
		key := "plain-key-upsert"
		created := create(t, "PlainUpsert", key, nil)
		softDelete(t, created.ID)

		restored, isNew, err := svc.CreateOrUpdate(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "PlainUpsert",
			Key:  &key,
		}, nil)
		require.NoError(t, err)
		require.False(t, isNew)
		require.Equal(t, created.CanonicalID, restored.CanonicalID)
		require.Nil(t, restored.Namespace)

		gotNS, gotVer, gotDel := readHead(t, created.CanonicalID)
		require.Nil(t, gotNS)
		require.Equal(t, wantVersionAfterRestore, gotVer)
		require.Nil(t, gotDel)
	})

	t.Run("un-namespaced: restore stays un-namespaced", func(t *testing.T) {
		key := "plain-key-restore"
		created := create(t, "PlainRestore", key, nil)
		softDelete(t, created.ID)

		restored, err := svc.Restore(ctx, projectID, created.ID, nil)
		require.NoError(t, err)
		require.Nil(t, restored.Namespace)

		gotNS, gotVer, gotDel := readHead(t, created.CanonicalID)
		require.Nil(t, gotNS)
		require.Equal(t, wantVersionAfterRestore, gotVer)
		require.Nil(t, gotDel)
	})
}
