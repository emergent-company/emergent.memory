package graph_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// setupProvenanceTest opens a throwaway test database (which runs the embedded
// migrations, including 00201), seeds an org+project, and returns the graph
// Repository + Service wired with a NoopEventSink (no journal/embedding deps).
func setupProvenanceTest(t *testing.T) (context.Context, bun.IDB, *graph.Repository, *graph.Service, uuid.UUID) {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_provenance")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Prov Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Prov Project",
	}, testutil.AdminUser.ID))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, nil, nil)

	return ctx, db, repo, svc, uuid.MustParse(projectID)
}

// actorRow captures the stored actor pair for a row.
type actorRow struct {
	Type *string
	ID   *uuid.UUID
}

func readObjectActor(t *testing.T, ctx context.Context, db bun.IDB, canonicalID uuid.UUID) actorRow {
	t.Helper()
	var r actorRow
	require.NoError(t, db.NewRaw(
		`SELECT actor_type, actor_id FROM kb.graph_objects WHERE canonical_id = ? AND supersedes_id IS NULL`, canonicalID,
	).Scan(ctx, &r.Type, &r.ID))
	return r
}

func readRelationshipActor(t *testing.T, ctx context.Context, db bun.IDB, canonicalID uuid.UUID) actorRow {
	t.Helper()
	var r actorRow
	require.NoError(t, db.NewRaw(
		`SELECT actor_type, actor_id FROM kb.graph_relationships WHERE canonical_id = ? AND supersedes_id IS NULL`, canonicalID,
	).Scan(ctx, &r.Type, &r.ID))
	return r
}

func strPtr(s string) *string { return &s }

// TestObjectActorPersistence proves object writes record the acting actor:
// a ctx-stamped agent, a ctx-stamped system (NULL id), and an unstamped context
// falling back to the HTTP-style user actorID (tasks 1.3.3 / 1.4.4).
func TestObjectActorPersistence(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	t.Run("agent actor stamped on context", func(t *testing.T) {
		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		resp, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{Type: "AgentObj"}, nil)
		require.NoError(t, err)

		got := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorAgent, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, agentID, *got.ID)
	})

	t.Run("system actor stamped on context (NULL id)", func(t *testing.T) {
		sctx := auth.WithActor(ctx, graph.ActorSystem, nil)
		resp, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{Type: "SystemObj"}, nil)
		require.NoError(t, err)

		got := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorSystem, *got.Type)
		require.Nil(t, got.ID, "system actor must persist NULL actor_id")
	})

	t.Run("unstamped context falls back to user actorID", func(t *testing.T) {
		userID := uuid.New()
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "UserObj"}, &userID)
		require.NoError(t, err)

		got := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorUser, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, userID, *got.ID)
	})
}

// TestRelationshipActorPersistence proves relationship create/update/bulk record
// the actor, human actorID is persisted, and the 00201 migration columns + CHECK
// + index exist (task 1.5.3).
func TestRelationshipActorPersistence(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	makeObject := func(typ string) uuid.UUID {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: typ}, nil)
		require.NoError(t, err)
		return resp.CanonicalID
	}
	srcID := makeObject("RelSrc")
	dstID := makeObject("RelDst")

	t.Run("stamped agent actor on create", func(t *testing.T) {
		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		rel, err := svc.CreateRelationship(sctx, projectID, &graph.CreateGraphRelationshipRequest{
			Type: "links", SrcID: srcID, DstID: dstID,
		}, nil)
		require.NoError(t, err)

		got := readRelationshipActor(t, ctx, db, rel.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorAgent, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, agentID, *got.ID)
	})

	t.Run("human actorID on create", func(t *testing.T) {
		userID := uuid.New()
		rel, err := svc.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
			Type: "owned_by", SrcID: srcID, DstID: dstID,
		}, &userID)
		require.NoError(t, err)

		got := readRelationshipActor(t, ctx, db, rel.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorUser, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, userID, *got.ID)
	})

	t.Run("stamped actor on update (new version)", func(t *testing.T) {
		rel, err := svc.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
			Type: "updatable", SrcID: srcID, DstID: dstID,
		}, nil)
		require.NoError(t, err)

		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		patched, err := svc.PatchRelationship(sctx, projectID, rel.ID, &graph.PatchGraphRelationshipRequest{
			Properties: map[string]any{"v": 2},
		}, nil)
		require.NoError(t, err)

		got := readRelationshipActor(t, ctx, db, patched.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorAgent, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, agentID, *got.ID)
	})

	t.Run("bulk create records stamped actor", func(t *testing.T) {
		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		resp, err := svc.BulkCreateRelationships(sctx, projectID, &graph.BulkCreateRelationshipsRequest{
			Items: []graph.CreateGraphRelationshipRequest{
				{Type: "bulk_a", SrcID: srcID, DstID: dstID},
				{Type: "bulk_b", SrcID: srcID, DstID: dstID},
			},
		}, nil)
		require.NoError(t, err)
		require.Equal(t, 2, resp.Success)
		for _, r := range resp.Results {
			require.True(t, r.Success)
			got := readRelationshipActor(t, ctx, db, r.Relationship.CanonicalID)
			require.NotNil(t, got.Type)
			require.Equal(t, graph.ActorAgent, *got.Type)
			require.NotNil(t, got.ID)
			require.Equal(t, agentID, *got.ID)
		}
	})

	t.Run("migration columns + CHECK + index exist", func(t *testing.T) {
		var n int
		require.NoError(t, db.NewRaw(`
			SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'kb' AND table_name = 'graph_relationships'
			  AND column_name IN ('actor_type', 'actor_id')`).Scan(ctx, &n))
		require.Equal(t, 2, n, "actor_type + actor_id columns must exist")

		var hasCheck bool
		require.NoError(t, db.NewRaw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'chk_graph_relationships_actor_type'
			)`).Scan(ctx, &hasCheck))
		require.True(t, hasCheck, "chk_graph_relationships_actor_type CHECK must exist")

		var hasIdx bool
		require.NoError(t, db.NewRaw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes WHERE schemaname = 'kb' AND indexname = 'idx_graph_relationships_actor'
			)`).Scan(ctx, &hasIdx))
		require.True(t, hasIdx, "idx_graph_relationships_actor partial index must exist")
	})
}

// TestSoftDeleteRestoreBulkActor proves repo soft-delete/restore/bulk paths record
// the acting actor on the tombstone/restored/updated HEAD row (task 1.6.2).
func TestSoftDeleteRestoreBulkActor(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	t.Run("soft delete records deleting actor", func(t *testing.T) {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "DelObj"}, nil)
		require.NoError(t, err)

		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		require.NoError(t, svc.Delete(sctx, projectID, resp.ID, nil, nil, nil))

		got := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorAgent, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, agentID, *got.ID)
	})

	t.Run("restore records restoring actor", func(t *testing.T) {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "RestObj"}, nil)
		require.NoError(t, err)
		require.NoError(t, svc.Delete(ctx, projectID, resp.ID, nil, nil, nil))

		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		_, err = svc.Restore(sctx, projectID, resp.ID, &agentID)
		require.NoError(t, err)

		got := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorAgent, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, agentID, *got.ID)
	})

	t.Run("bulk status update records actor", func(t *testing.T) {
		resp, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{Type: "BulkObj"}, nil)
		require.NoError(t, err)

		agentID := uuid.New()
		sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
		res, err := svc.BulkUpdateStatus(sctx, projectID, &graph.BulkUpdateStatusRequest{
			IDs:    []string{resp.ID.String()},
			Status: "archived",
		}, nil)
		require.NoError(t, err)
		require.Equal(t, 1, res.Success)

		got := readObjectActor(t, ctx, db, resp.CanonicalID)
		require.NotNil(t, got.Type)
		require.Equal(t, graph.ActorAgent, *got.Type)
		require.NotNil(t, got.ID)
		require.Equal(t, agentID, *got.ID)
	})
}

// TestForkBranchCopiesActor proves fork-branch copies carry the source row's
// actor for both objects and relationships (task 1.7.3 / A2).
func TestForkBranchCopiesActor(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	agentID := uuid.New()
	sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)

	src, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{Type: "ForkSrc"}, nil)
	require.NoError(t, err)
	dst, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{Type: "ForkDst"}, nil)
	require.NoError(t, err)
	rel, err := svc.CreateRelationship(sctx, projectID, &graph.CreateGraphRelationshipRequest{
		Type: "fork_rel", SrcID: src.CanonicalID, DstID: dst.CanonicalID,
	}, nil)
	require.NoError(t, err)

	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "forked"})
	require.NoError(t, err)
	require.Equal(t, 2, forkResp.CopiedObjects)
	require.Equal(t, 1, forkResp.CopiedRelationships)

	forkBranchID := uuid.MustParse(forkResp.BranchID)

	// Copied object on the fork branch carries the source actor.
	var objType *string
	var objID *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT actor_type, actor_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id = ? AND type = 'ForkSrc' AND supersedes_id IS NULL`,
		projectID, forkBranchID).Scan(ctx, &objType, &objID))
	require.NotNil(t, objType)
	require.Equal(t, graph.ActorAgent, *objType)
	require.NotNil(t, objID)
	require.Equal(t, agentID, *objID)

	// Copied relationship on the fork branch carries the source actor.
	var relType *string
	var relID *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT actor_type, actor_id FROM kb.graph_relationships
		WHERE project_id = ? AND branch_id = ? AND type = 'fork_rel' AND supersedes_id IS NULL`,
		projectID, forkBranchID).Scan(ctx, &relType, &relID))
	require.NotNil(t, relType)
	require.Equal(t, graph.ActorAgent, *relType)
	require.NotNil(t, relID)
	require.Equal(t, agentID, *relID)

	_ = rel
}

// TestMergeAddedCloneCarriesActor proves the merge "added" clone carries the
// source row's actor (task 1.7.3).
func TestMergeAddedCloneCarriesActor(t *testing.T) {
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "src-added"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))

	agentID := uuid.New()
	sctx := auth.WithActor(ctx, graph.ActorAgent, &agentID)
	_, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{
		Type: "AddedObj", Key: strPtr("added-key"), BranchID: &sourceBranch.ID,
	}, nil)
	require.NoError(t, err)

	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	var typ *string
	var id *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT actor_type, actor_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id IS NULL AND key = 'added-key' AND supersedes_id IS NULL`,
		projectID).Scan(ctx, &typ, &id))
	require.NotNil(t, typ, "cloned object must exist on main after merge")
	require.Equal(t, graph.ActorAgent, *typ)
	require.NotNil(t, id)
	require.Equal(t, agentID, *id)
}

// TestMergeFastForwardCloneCarriesActor proves the fast-forward clone carries the
// source row's actor (task 1.7.3).
func TestMergeFastForwardCloneCarriesActor(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	// v1 on main authored by agent A.
	agentA := uuid.New()
	sctxA := auth.WithActor(ctx, graph.ActorAgent, &agentA)
	_, err := svc.Create(sctxA, projectID, &graph.CreateGraphObjectRequest{
		Type: "FFObj", Key: strPtr("ff-key"),
	}, nil)
	require.NoError(t, err)

	// Fork main → branch (copies v1), then patch on the branch authored by agent B.
	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "ff-branch"})
	require.NoError(t, err)
	forkBranchID := uuid.MustParse(forkResp.BranchID)

	var mainObjID uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT id FROM kb.graph_objects WHERE project_id = ? AND branch_id IS NULL AND key = 'ff-key' AND supersedes_id IS NULL`,
		projectID).Scan(ctx, &mainObjID))

	agentB := uuid.New()
	sctxB := auth.WithActor(ctx, graph.ActorAgent, &agentB)
	_, err = svc.Patch(sctxB, projectID, mainObjID, &graph.PatchGraphObjectRequest{
		BranchID:   &forkBranchID,
		Properties: map[string]any{"v": 2},
	}, nil)
	require.NoError(t, err)

	// Merge the branch → main: fast-forward.
	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: forkBranchID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	var typ *string
	var id *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT actor_type, actor_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id IS NULL AND key = 'ff-key' AND supersedes_id IS NULL`,
		projectID).Scan(ctx, &typ, &id))
	require.NotNil(t, typ)
	require.Equal(t, graph.ActorAgent, *typ)
	require.NotNil(t, id)
	require.Equal(t, agentB, *id, "fast-forwarded main HEAD must carry the source (agent B) actor")
}

// TestProvenanceFilterRealSQL proves the §1.8 filter against real data: an object
// created by agent A then updated by agent B is returned by created(A), updated(B),
// and any(A|B); the (actor_type, actor_id) pair rule holds (same UUID under a
// different actor_type does NOT match).
func TestProvenanceFilterRealSQL(t *testing.T) {
	ctx, _, repo, svc, projectID := setupProvenanceTest(t)

	agentA := uuid.New()
	sctxA := auth.WithActor(ctx, graph.ActorAgent, &agentA)
	created, err := svc.Create(sctxA, projectID, &graph.CreateGraphObjectRequest{
		Type: "ProvObj", Key: strPtr("prov-key"),
	}, nil)
	require.NoError(t, err)

	agentB := uuid.New()
	sctxB := auth.WithActor(ctx, graph.ActorAgent, &agentB)
	_, err = svc.Patch(sctxB, projectID, created.ID, &graph.PatchGraphObjectRequest{
		Properties: map[string]any{"v": 2},
	}, nil)
	require.NoError(t, err)

	canonicalID := created.CanonicalID
	agentType := graph.ActorAgent

	list := func(actorID uuid.UUID, provenance string) []*graph.GraphObject {
		objs, err := repo.List(ctx, graph.ListParams{
			ProjectID:  projectID,
			ActorType:  &agentType,
			ActorID:    &actorID,
			Provenance: provenance,
		})
		require.NoError(t, err)
		return objs
	}

	contains := func(objs []*graph.GraphObject) bool {
		for _, o := range objs {
			if o.CanonicalID == canonicalID {
				return true
			}
		}
		return false
	}

	// created: A yes, B no.
	require.True(t, contains(list(agentA, graph.ProvenanceCreated)), "created(A) must include the object")
	require.False(t, contains(list(agentB, graph.ProvenanceCreated)), "created(B) must NOT include the object")

	// updated: B yes, A no.
	require.True(t, contains(list(agentB, graph.ProvenanceUpdated)), "updated(B) must include the object")
	require.False(t, contains(list(agentA, graph.ProvenanceUpdated)), "updated(A) must NOT include the object")

	// any: both yes.
	require.True(t, contains(list(agentA, graph.ProvenanceAny)), "any(A) must include the object")
	require.True(t, contains(list(agentB, graph.ProvenanceAny)), "any(B) must include the object")

	// Actor-pair rule: same UUID under a different actor_type does NOT match.
	userType := graph.ActorUser
	objs, err := repo.List(ctx, graph.ListParams{
		ProjectID:  projectID,
		ActorType:  &userType,
		ActorID:    &agentA,
		Provenance: graph.ProvenanceAny,
	})
	require.NoError(t, err)
	require.False(t, contains(objs), "same UUID under actor_type=user must NOT match the agent-written object")
}

// TestProvenanceFilterProjectScoped proves the `created` subquery is scoped to
// the requested project: a system-authored version=1 object in a DIFFERENT
// project must not leak into this project's results. A NULL actor_id would
// otherwise match every system row deployment-wide.
func TestProvenanceFilterProjectScoped(t *testing.T) {
	// Note: the outer query already filters project_id and canonical_id is globally
	// unique, so this end-to-end test passes with or without the subquery scoping —
	// it guards behaviour, not the scoping delta. The scoping itself (project_id +
	// branch predicates inside the created subquery) is asserted directly on the
	// rendered SQL in provenance_filter_test.go ("created mode scopes subquery to
	// project and main branch" / "...to a specific branch").
	ctx, db, repo, svc, projectID := setupProvenanceTest(t)

	// A second project in the same DB.
	otherOrgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, otherOrgID, "Other Org"))
	otherProjectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: otherProjectID, OrgID: otherOrgID, Name: "Other Project",
	}, testutil.AdminUser.ID))
	otherPID := uuid.MustParse(otherProjectID)

	sysCtx := auth.WithActor(ctx, graph.ActorSystem, nil)
	own, err := svc.Create(sysCtx, projectID, &graph.CreateGraphObjectRequest{Type: "SysObj", Key: strPtr("own-sys")}, nil)
	require.NoError(t, err)
	other, err := svc.Create(sysCtx, otherPID, &graph.CreateGraphObjectRequest{Type: "SysObj", Key: strPtr("other-sys")}, nil)
	require.NoError(t, err)

	system := graph.ActorSystem
	objs, err := repo.List(ctx, graph.ListParams{
		ProjectID:  projectID,
		ActorType:  &system,
		Provenance: graph.ProvenanceCreated,
	})
	require.NoError(t, err)

	foundOwn := false
	for _, o := range objs {
		if o.CanonicalID == own.CanonicalID {
			foundOwn = true
			continue
		}
		if o.CanonicalID == other.CanonicalID {
			t.Errorf("created(system) leaked the other project's object %s", o.CanonicalID)
		}
	}
	require.True(t, foundOwn, "this project's system-authored object must be returned")
}

// TestMergeConflictCarriesActor proves a conflict-resolved merge version carries
// the source row's actor rather than silently becoming user+NULL.
func TestMergeConflictCarriesActor(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	// v1 on main authored by agent A, with a property key both sides will change.
	agentA := uuid.New()
	sctxA := auth.WithActor(ctx, graph.ActorAgent, &agentA)
	created, err := svc.Create(sctxA, projectID, &graph.CreateGraphObjectRequest{
		Type: "ConfObj", Key: strPtr("conf-key"), Properties: map[string]any{"v": 1},
	}, nil)
	require.NoError(t, err)

	// Fork main → branch (copies v1).
	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "conf-branch"})
	require.NoError(t, err)
	forkBranchID := uuid.MustParse(forkResp.BranchID)

	// Patch the branch to {v:3} (agent B).
	agentB := uuid.New()
	sctxB := auth.WithActor(ctx, graph.ActorAgent, &agentB)
	_, err = svc.Patch(sctxB, projectID, created.ID, &graph.PatchGraphObjectRequest{
		BranchID: &forkBranchID, Properties: map[string]any{"v": 3},
	}, nil)
	require.NoError(t, err)

	// Patch main to {v:2} (agent C), diverging the other way → conflict.
	agentC := uuid.New()
	sctxC := auth.WithActor(ctx, graph.ActorAgent, &agentC)
	_, err = svc.Patch(sctxC, projectID, created.ID, &graph.PatchGraphObjectRequest{Properties: map[string]any{"v": 2}}, nil)
	require.NoError(t, err)

	// Merge with overwrite (mine_no_sim) → conflict, source (branch, agent B) wins.
	_, err = svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
		SourceBranchID: forkBranchID, Execute: true, Policy: "mine_no_sim",
	})
	require.NoError(t, err)

	var typ *string
	var id *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT actor_type, actor_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id IS NULL AND key = 'conf-key' AND supersedes_id IS NULL`,
		projectID).Scan(ctx, &typ, &id))
	require.NotNil(t, typ)
	require.Equal(t, graph.ActorAgent, *typ)
	require.NotNil(t, id)
	require.Equal(t, agentB, *id, "conflict-resolved main HEAD must carry the source (branch/agent B) actor")
}
