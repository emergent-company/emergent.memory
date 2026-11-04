package graph_test

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// stampRow captures the stored write stamp for a row.
type stampRow struct {
	CredentialKind string
	CredentialID   *uuid.UUID
	RunID          *uuid.UUID
	RootRunID      *uuid.UUID
	DataClass      string
	InputHash      *string
	ContentHash    []byte
}

func readObjectStamp(t *testing.T, ctx context.Context, db bun.IDB, canonicalID uuid.UUID) stampRow {
	t.Helper()
	var r stampRow
	require.NoError(t, db.NewRaw(
		`SELECT credential_kind, credential_id, run_id, root_run_id, data_class, input_hash, content_hash
		   FROM kb.graph_objects
		  WHERE canonical_id = ? AND supersedes_id IS NULL`, canonicalID,
	).Scan(ctx, &r.CredentialKind, &r.CredentialID, &r.RunID, &r.RootRunID, &r.DataClass, &r.InputHash, &r.ContentHash))
	return r
}

// TestWriteStampPersistence proves slice 1 of #2376 end to end: a write through
// an API key inside an agent run stores the credential and the run alongside the
// actor, and a background write stores an explicit "none" instead of a blank.
func TestWriteStampPersistence(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	t.Run("API key inside an agent run", func(t *testing.T) {
		agentID := uuid.New()
		credentialID := uuid.New()
		runID := uuid.New()
		rootRunID := uuid.New()

		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		sctx = auth.ContextWithCredential(sctx, auth.CredentialAPIKey, &credentialID)
		sctx = auth.ContextWithRunIDs(sctx, &runID, &rootRunID)

		resp, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       "StampObj",
			Key:        strPtr("stamp/api-key"),
			Properties: map[string]any{"title": "written by an agent"},
		}, &agentID)
		require.NoError(t, err)

		got := readObjectStamp(t, ctx, db, resp.CanonicalID)
		require.Equal(t, auth.CredentialAPIKey, got.CredentialKind)
		require.NotNil(t, got.CredentialID)
		require.Equal(t, credentialID, *got.CredentialID)
		require.NotNil(t, got.RunID)
		require.Equal(t, runID, *got.RunID)
		require.NotNil(t, got.RootRunID)
		require.Equal(t, rootRunID, *got.RootRunID)
		require.Equal(t, graph.DataClassSynthesized, got.DataClass)
		// A create has no prior content to have observed.
		require.Nil(t, got.InputHash)

		// The actor pair keeps its meaning: the agent definition, not the run.
		actor := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, actor.Type)
		require.Equal(t, graph.ActorAgent, *actor.Type)
		require.NotNil(t, actor.ID)
		require.Equal(t, agentID, *actor.ID)

		// The stamp reaches the API response, not just the row.
		require.Equal(t, auth.CredentialAPIKey, resp.CredentialKind)
		require.NotNil(t, resp.RunID)
		require.Equal(t, runID, *resp.RunID)
	})

	t.Run("background write records no credential", func(t *testing.T) {
		sctx := auth.WithActor(ctx, graph.ActorSystem, nil)

		resp, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       "StampObj",
			Key:        strPtr("stamp/background"),
			Properties: map[string]any{"title": "extracted"},
		}, nil)
		require.NoError(t, err)

		got := readObjectStamp(t, ctx, db, resp.CanonicalID)
		require.Equal(t, auth.CredentialNone, got.CredentialKind)
		require.Nil(t, got.CredentialID)
		require.Nil(t, got.RunID)
		require.Nil(t, got.RootRunID)
	})

	t.Run("an unstamped context is none, not empty", func(t *testing.T) {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       "StampObj",
			Key:        strPtr("stamp/unstamped"),
			Properties: map[string]any{"title": "no credential"},
		}, nil)
		require.NoError(t, err)

		got := readObjectStamp(t, ctx, db, resp.CanonicalID)
		require.Equal(t, auth.CredentialNone, got.CredentialKind)
	})
}

// TestWriteStampInputHashOnUpdate proves the update path records what the writer
// saw (the previous version's content hash), and that rewriting an object with
// identical content under a different credential is a genuine no-op: no new
// version, no moved content hash. That is the property that keeps the stamp out
// of `properties` and out of the re-embedding path.
func TestWriteStampInputHashOnUpdate(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	firstCredential := uuid.New()
	createCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &firstCredential)

	created, err := svc.Create(createCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/update"),
		Properties: map[string]any{"title": "v1"},
	}, nil)
	require.NoError(t, err)

	var v1Hash []byte
	require.NoError(t, db.NewRaw(
		`SELECT content_hash FROM kb.graph_objects WHERE canonical_id = ? AND version = 1`, created.CanonicalID,
	).Scan(ctx, &v1Hash))
	require.NotEmpty(t, v1Hash)

	// A real content change under a different credential: new version, new stamp,
	// and an input hash naming the content the writer observed.
	secondCredential := uuid.New()
	updateCtx := auth.ContextWithCredential(ctx, auth.CredentialMCPLink, &secondCredential)

	_, isNew, err := svc.CreateOrUpdate(updateCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/update"),
		Properties: map[string]any{"title": "v2"},
	}, nil)
	require.NoError(t, err)
	require.False(t, isNew)

	head := readObjectStamp(t, ctx, db, created.CanonicalID)
	require.NotNil(t, head.InputHash)
	require.Equal(t, hex.EncodeToString(v1Hash), *head.InputHash)
	require.NotEqual(t, v1Hash, head.ContentHash, "a content change must move the content hash")
	require.Equal(t, auth.CredentialMCPLink, head.CredentialKind)
	require.NotNil(t, head.CredentialID)
	require.Equal(t, secondCredential, *head.CredentialID)

	// Same content again, third credential: nothing to write. If the stamp were
	// stored in properties the upsert would look like a content change and mint a
	// version (and an embedding job) on every call.
	thirdCredential := uuid.New()
	repeatCtx := auth.ContextWithCredential(ctx, auth.CredentialSession, &thirdCredential)

	_, isNew, err = svc.CreateOrUpdate(repeatCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/update"),
		Properties: map[string]any{"title": "v2"},
	}, nil)
	require.NoError(t, err)
	require.False(t, isNew)

	var versionCount int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.graph_objects WHERE canonical_id = ?`, created.CanonicalID,
	).Scan(ctx, &versionCount))
	require.Equal(t, 2, versionCount, "a stamp-only rewrite must not mint a version")

	unchanged := readObjectStamp(t, ctx, db, created.CanonicalID)
	require.Equal(t, head.ContentHash, unchanged.ContentHash)
	require.Equal(t, auth.CredentialMCPLink, unchanged.CredentialKind,
		"no row was written, so the stored stamp must not change")
}

// TestForkBranchPreservesWriteStamp proves a fork copy carries the source row's
// stamp and data class instead of being rebuilt as an unattributed row.
func TestForkBranchPreservesWriteStamp(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	credentialID := uuid.New()
	runID := uuid.New()
	rootRunID := uuid.New()
	agentID := uuid.New()

	createCtx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
	createCtx = auth.ContextWithCredential(createCtx, auth.CredentialAPIKey, &credentialID)
	createCtx = auth.ContextWithRunIDs(createCtx, &runID, &rootRunID)

	created, err := svc.Create(createCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/fork"),
		Properties: map[string]any{"title": "forked"},
	}, &agentID)
	require.NoError(t, err)

	fork, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "stamp-fork"})
	require.NoError(t, err)

	branchID := uuid.MustParse(fork.BranchID)
	require.NotEqual(t, uuid.Nil, branchID)

	var copied stampRow
	require.NoError(t, db.NewRaw(
		`SELECT credential_kind, credential_id, run_id, root_run_id, data_class, input_hash
		   FROM kb.graph_objects
		  WHERE canonical_id = ? AND branch_id = ? AND supersedes_id IS NULL`,
		created.CanonicalID, branchID,
	).Scan(ctx, &copied.CredentialKind, &copied.CredentialID, &copied.RunID, &copied.RootRunID, &copied.DataClass, &copied.InputHash))

	require.Equal(t, auth.CredentialAPIKey, copied.CredentialKind)
	require.NotNil(t, copied.CredentialID)
	require.Equal(t, credentialID, *copied.CredentialID)
	require.NotNil(t, copied.RunID)
	require.Equal(t, runID, *copied.RunID)
	require.NotNil(t, copied.RootRunID)
	require.Equal(t, rootRunID, *copied.RootRunID)
	require.Equal(t, graph.DataClassSynthesized, copied.DataClass)
	require.Nil(t, copied.InputHash, "a create supersedes nothing, so there is no input to fingerprint")

	// A fork copies the head row, so an updated object's input_hash (the content
	// hash of the row it supersedes) must survive the copy instead of being reset
	// as it would be on a create.
	updated, err := svc.Patch(createCtx, projectID, created.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"title": "forked and edited"},
	}, &agentID)
	require.NoError(t, err)
	require.NotNil(t, updated.InputHash)

	forkAgain, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "stamp-fork-2"})
	require.NoError(t, err)
	branchID2 := uuid.MustParse(forkAgain.BranchID)

	var copied2 stampRow
	require.NoError(t, db.NewRaw(
		`SELECT input_hash FROM kb.graph_objects
		  WHERE canonical_id = ? AND branch_id = ? AND supersedes_id IS NULL`,
		created.CanonicalID, branchID2,
	).Scan(ctx, &copied2.InputHash))
	require.NotNil(t, copied2.InputHash)
	require.Equal(t, *updated.InputHash, *copied2.InputHash)
}

// TestWriteStampReadBack proves the stamp is actually selected on the read paths,
// not just written. A stale projection would return empty credential_kind and
// data_class, and — worse — feed an empty data class into the next edit.
func TestWriteStampReadBack(t *testing.T) {
	ctx, _, repo, svc, projectID := setupProvenanceTest(t)

	credentialID := uuid.New()
	createCtx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &credentialID)

	created, err := svc.Create(createCtx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/read"),
		Properties: map[string]any{"title": "read me back"},
	}, nil)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, projectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, auth.CredentialAPIKey, fetched.CredentialKind)
	require.NotNil(t, fetched.CredentialID)
	require.Equal(t, credentialID, *fetched.CredentialID)
	require.Equal(t, graph.DataClassSynthesized, fetched.DataClass)

	listed, err := repo.List(ctx, graph.ListParams{ProjectID: projectID})
	require.NoError(t, err)
	require.NotEmpty(t, listed)
	var found bool
	for _, obj := range listed {
		if obj.ID != fetched.ID {
			continue
		}
		found = true
		require.Equal(t, auth.CredentialAPIKey, obj.CredentialKind)
		require.Equal(t, graph.DataClassSynthesized, obj.DataClass)
	}
	require.True(t, found, "the created object should appear in the list projection")

	// Search results are the same GraphObject type but a different SELECT. A
	// missing column there scans as the zero value, so a search hit would report
	// an out-of-enum data_class and an empty credential_kind.
	results, err := repo.FTSSearch(ctx, graph.FTSSearchParams{
		ProjectID: projectID,
		Query:     "read",
		Limit:     20,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	var searched *graph.GraphObject
	for _, r := range results {
		if r.Object.ID == fetched.ID {
			searched = r.Object
			break
		}
	}
	require.NotNil(t, searched, "the created object should be searchable")
	require.Equal(t, auth.CredentialAPIKey, searched.CredentialKind)
	require.Equal(t, graph.DataClassSynthesized, searched.DataClass)
	require.NotNil(t, searched.CredentialID)
}

// TestPatchPreservesSourceTagging is the laundering guard: an ordinary edit reads
// the current row and writes a new version from it, so an empty or re-defaulted
// data class would silently turn a source-owned object into a synthesized one.
// No API sets ownership yet (slice 2/3), so the row is classified directly.
func TestPatchPreservesSourceTagging(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	created, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/patch"),
		Properties: map[string]any{"title": "source owned"},
	}, nil)
	require.NoError(t, err)

	var sourceID uuid.UUID
	require.NoError(t, db.NewRaw(
		`INSERT INTO kb.sources (project_id, provider, display_name) VALUES (?, 'external', ?) RETURNING id`,
		projectID, "stamp-source-"+uuid.NewString()[:8],
	).Scan(ctx, &sourceID))

	_, err = db.NewRaw(
		`UPDATE kb.graph_objects SET data_class = 'source', owner_source_id = ? WHERE canonical_id = ?`,
		sourceID, created.CanonicalID,
	).Exec(ctx)
	require.NoError(t, err)

	patched, err := svc.Patch(ctx, projectID, created.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"title": "edited by a human"},
	}, nil)
	require.NoError(t, err)

	require.Equal(t, graph.DataClassSource, patched.DataClass)
	require.NotNil(t, patched.OwnerSourceID)
	require.Equal(t, sourceID, *patched.OwnerSourceID)
}

// TestMergeBranchClonesCarryMergeStamp covers the merge path that clones a
// branch-only object onto the target: the clone is a new write on main, so it
// must carry the merge's credential/run — not the stamp of whoever created the
// object on the branch, and not an empty one.
func TestMergeBranchClonesCarryMergeStamp(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	forked, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "stamp-merge"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forked.BranchID)

	createCredential := uuid.New()
	created, err := svc.Create(
		auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &createCredential),
		projectID,
		&graph.CreateGraphObjectRequest{
			Type:     "StampObj",
			Key:      strPtr("stamp/merge"),
			BranchID: &branchID,
		},
		nil,
	)
	require.NoError(t, err)

	// A relationship between two branch-only objects is merged by a direct
	// insert as well, so it needs the same treatment.
	second, err := svc.Create(
		auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &createCredential),
		projectID,
		&graph.CreateGraphObjectRequest{
			Type:     "StampObj",
			Key:      strPtr("stamp/merge-2"),
			BranchID: &branchID,
		},
		nil,
	)
	require.NoError(t, err)
	relationship, err := svc.CreateRelationship(
		auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &createCredential),
		projectID,
		&graph.CreateGraphRelationshipRequest{
			Type:     "StampRel",
			SrcID:    created.CanonicalID,
			DstID:    second.CanonicalID,
			BranchID: &branchID,
		},
		nil,
	)
	require.NoError(t, err)

	mergeCredential := uuid.New()
	runID := uuid.New()
	mergeCtx := auth.ContextWithRunIDs(
		auth.ContextWithCredential(ctx, auth.CredentialMCPLink, &mergeCredential),
		&runID,
		nil,
	)

	_, err = svc.MergeBranch(mergeCtx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	require.NotEqual(t, created.CanonicalID, uuid.Nil)

	// A merged clone gets a fresh canonical ID on the target, so the row is
	// found by its key rather than by the branch object's identity.
	var head struct {
		CredentialKind string
		CredentialID   *uuid.UUID
		RunID          *uuid.UUID
	}
	require.NoError(t, db.NewRaw(
		`SELECT credential_kind, credential_id, run_id
		   FROM kb.graph_objects
		  WHERE project_id = ? AND key = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		projectID, "stamp/merge",
	).Scan(ctx, &head.CredentialKind, &head.CredentialID, &head.RunID))

	require.Equal(t, auth.CredentialMCPLink, head.CredentialKind)
	require.NotNil(t, head.CredentialID)
	require.Equal(t, mergeCredential, *head.CredentialID)
	require.NotNil(t, head.RunID)
	require.Equal(t, runID, *head.RunID)

	var mergedRel struct {
		CredentialKind string
		CredentialID   *uuid.UUID
		RunID          *uuid.UUID
	}
	require.NoError(t, db.NewRaw(
		`SELECT credential_kind, credential_id, run_id
		   FROM kb.graph_relationships
		  WHERE type = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		relationship.Type,
	).Scan(ctx, &mergedRel.CredentialKind, &mergedRel.CredentialID, &mergedRel.RunID))
	require.NotEqual(t, uuid.Nil, relationship.ID)
	require.Equal(t, auth.CredentialMCPLink, mergedRel.CredentialKind)
	require.NotNil(t, mergedRel.CredentialID)
	require.Equal(t, mergeCredential, *mergedRel.CredentialID)
	require.NotNil(t, mergedRel.RunID)
	require.Equal(t, runID, *mergedRel.RunID)
}

// TestMergeBranchFastForwardCarriesMergeStamp covers the other merge shape: a
// branch-only edit is fast-forwarded onto the target as a new version row, and
// that row is the merge's write, so it carries the merge credential rather than
// the editor's. The source's data class still rides along.
func TestMergeBranchFastForwardCarriesMergeStamp(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	createCredential := uuid.New()
	created, err := svc.Create(
		auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &createCredential),
		projectID,
		&graph.CreateGraphObjectRequest{
			Type:       "StampObj",
			Key:        strPtr("stamp/ff"),
			Properties: map[string]any{"title": "v1"},
		},
		nil,
	)
	require.NoError(t, err)

	forked, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "stamp-ff"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forked.BranchID)

	editorCredential := uuid.New()
	_, err = svc.Patch(
		auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &editorCredential),
		projectID,
		created.ID,
		&graph.PatchGraphObjectRequest{BranchID: &branchID, Properties: map[string]any{"title": "v2"}},
		nil,
	)
	require.NoError(t, err)

	mergeCredential := uuid.New()
	mergeCtx := auth.ContextWithCredential(ctx, auth.CredentialMCPLink, &mergeCredential)
	_, err = svc.MergeBranch(mergeCtx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	var head struct {
		CredentialKind string
		CredentialID   *uuid.UUID
		DataClass      string
		Version        int
	}
	require.NoError(t, db.NewRaw(
		`SELECT credential_kind, credential_id, data_class, version
		   FROM kb.graph_objects
		  WHERE canonical_id = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
		created.CanonicalID,
	).Scan(ctx, &head.CredentialKind, &head.CredentialID, &head.DataClass, &head.Version))

	require.Equal(t, 2, head.Version, "the fast-forward must land as a new version on the target")
	require.Equal(t, auth.CredentialMCPLink, head.CredentialKind)
	require.NotNil(t, head.CredentialID)
	require.Equal(t, mergeCredential, *head.CredentialID)
	require.Equal(t, graph.DataClassSynthesized, head.DataClass)
}

// TestMergeBranchFastForwardRefusesForeignOwner: a fast-forward used to copy the
// source's data class and owner over the target's, so a branch edit could hand a
// target object owned by one source to a different source with no error. It now
// resolves ownership the same way a conflict does (#2376).
func TestMergeBranchFastForwardRefusesForeignOwner(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	created, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
		Type:       "StampObj",
		Key:        strPtr("stamp/ff-owner"),
		Properties: map[string]any{"title": "v1"},
	}, nil)
	require.NoError(t, err)

	var ownerA, ownerB uuid.UUID
	for name, dst := range map[string]*uuid.UUID{"a": &ownerA, "b": &ownerB} {
		require.NoError(t, db.NewRaw(
			`INSERT INTO kb.sources (project_id, provider, display_name) VALUES (?, 'external', ?) RETURNING id`,
			projectID, "stamp-ff-owner-"+name+"-"+uuid.NewString()[:8],
		).Scan(ctx, dst))
	}

	_, err = db.NewRaw(
		`UPDATE kb.graph_objects SET data_class = 'source', owner_source_id = ? WHERE canonical_id = ? AND branch_id IS NULL`,
		ownerA, created.CanonicalID,
	).Exec(ctx)
	require.NoError(t, err)

	forked, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "stamp-ff-owner"})
	require.NoError(t, err)
	branchID := uuid.MustParse(forked.BranchID)

	_, err = svc.Patch(ctx, projectID, created.ID, &graph.PatchGraphObjectRequest{
		BranchID:   &branchID,
		Properties: map[string]any{"title": "v2 on the branch"},
	}, nil)
	require.NoError(t, err)

	_, err = db.NewRaw(
		`UPDATE kb.graph_objects SET data_class = 'source', owner_source_id = ? WHERE canonical_id = ? AND branch_id = ? AND supersedes_id IS NULL`,
		ownerB, created.CanonicalID, branchID,
	).Exec(ctx)
	require.NoError(t, err)

	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.Error(t, err, "a fast-forward must not reassign an object from one source to another")
	require.Contains(t, err.Error(), "owner_conflict")
}
