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

	"github.com/emergent-company/emergent.memory/domain/branches"
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

	// Patch main to {v:2} (agent C) BEFORE forking. A branch_id-less Patch
	// resolves the target via GetByID, which matches (id OR canonical_id) across
	// branches; once a fork shares the same canonical_id it can resolve to the
	// branch HEAD instead of main. Patching main first keeps this step deterministic.
	agentC := uuid.New()
	sctxC := auth.WithActor(ctx, graph.ActorAgent, &agentC)
	_, err = svc.Patch(sctxC, projectID, created.ID, &graph.PatchGraphObjectRequest{Properties: map[string]any{"v": 2}}, nil)
	require.NoError(t, err)

	// Fork main → branch (copies {v:2}).
	forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "conf-branch"})
	require.NoError(t, err)
	forkBranchID := uuid.MustParse(forkResp.BranchID)

	// Patch the branch to {v:3} (agent B), diverging the other way → conflict.
	agentB := uuid.New()
	sctxB := auth.WithActor(ctx, graph.ActorAgent, &agentB)
	_, err = svc.Patch(sctxB, projectID, created.ID, &graph.PatchGraphObjectRequest{
		BranchID: &forkBranchID, Properties: map[string]any{"v": 3},
	}, nil)
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

// recordingBranchNotificationResolver records every ResolveByBranch call so a
// test can assert whether (and for which branch) the resolver fired.
type recordingBranchNotificationResolver struct {
	calls []string
}

func (r *recordingBranchNotificationResolver) ResolveByBranch(_ context.Context, _, branchID string) error {
	r.calls = append(r.calls, branchID)
	return nil
}

// TestMergeBranchResolvesNotificationsOnlyOnFullMerge pins the merge-notification
// contract: review-ready notifications are resolved and merged_at is stamped only
// when the merge actually closes the source branch (nothing left for review, no
// relationship conflicts). A partial merge keeps the branch open, so it must
// neither resolve the notifications nor stamp merged_at.
func TestMergeBranchResolvesNotificationsOnlyOnFullMerge(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	newRepo := func(db bun.IDB) *graph.Repository {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		cfg := &config.Config{}
		cfg.Graph.MaxListLimit = 100_000
		return graph.NewRepository(db, log, cfg)
	}

	newService := func(db bun.IDB, repo *graph.Repository, resolver graph.BranchNotificationResolver) *graph.Service {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)
		return svc.WithBranchNotificationResolver(resolver)
	}

	mergedAtOf := func(db bun.IDB, branchID uuid.UUID) *time.Time {
		var mergedAt *time.Time
		err := db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, branchID).Scan(ctx, &mergedAt)
		require.NoError(t, err)
		return mergedAt
	}

	seedOrgProject := func(db bun.IDB, orgName, projName string) uuid.UUID {
		orgID := uuid.NewString()
		require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, orgName))
		projectID := uuid.NewString()
		require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
			ID: projectID, OrgID: orgID, Name: projName,
		}, testutil.AdminUser.ID))
		return uuid.MustParse(projectID)
	}

	t.Run("full merge resolves notifications and stamps merged_at", func(t *testing.T) {
		tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_merge_notify_full")
		t.Cleanup(tdb.Close)
		db := tdb.GetDB()
		projectID := seedOrgProject(db, "Merge Notify Full Org", "Merge Notify Full Project")

		repo := newRepo(db)
		resolver := &recordingBranchNotificationResolver{}
		svc := newService(db, repo, resolver)

		sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: projectID, Name: "full-branch"}
		require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
		_, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "FullObj", Key: strPtr("full-key"), BranchID: &sourceBranch.ID,
		}, nil)
		require.NoError(t, err)

		resp, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
			SourceBranchID: sourceBranch.ID, Execute: true, Policy: "enrich_no_sim",
		})
		require.NoError(t, err)
		require.Equal(t, 0, resp.LeftForReviewCount)

		require.Len(t, resolver.calls, 1, "full merge must resolve the branch notifications")
		require.Equal(t, sourceBranch.ID.String(), resolver.calls[0])
		require.NotNil(t, mergedAtOf(db, sourceBranch.ID), "full merge must stamp merged_at")
	})

	t.Run("partial merge leaves notifications and merged_at alone", func(t *testing.T) {
		tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_merge_notify_partial")
		t.Cleanup(tdb.Close)
		db := tdb.GetDB()
		projectID := seedOrgProject(db, "Merge Notify Partial Org", "Merge Notify Partial Project")

		repo := newRepo(db)
		resolver := &recordingBranchNotificationResolver{}
		svc := newService(db, repo, resolver)

		// Create an object on main, diverge it on a fork so the merge has a
		// canonical conflict; "partial" policy (preserve_target) leaves it for review.
		created, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "PartialConf", Key: strPtr("partial-conf-key"), Properties: map[string]any{"v": 1},
		}, nil)
		require.NoError(t, err)
		_, err = svc.Patch(ctx, projectID, created.ID, &graph.PatchGraphObjectRequest{Properties: map[string]any{"v": 2}}, nil)
		require.NoError(t, err)

		forkResp, err := svc.ForkBranch(ctx, projectID, nil, &graph.ForkBranchRequest{Name: "partial-branch"})
		require.NoError(t, err)
		forkBranchID := uuid.MustParse(forkResp.BranchID)
		_, err = svc.Patch(ctx, projectID, created.ID, &graph.PatchGraphObjectRequest{
			BranchID: &forkBranchID, Properties: map[string]any{"v": 3},
		}, nil)
		require.NoError(t, err)

		resp, err := svc.MergeBranch(ctx, projectID, nil, &graph.BranchMergeRequest{
			SourceBranchID: forkBranchID, Execute: true, Policy: "partial",
		})
		require.NoError(t, err)
		require.Equal(t, 1, resp.LeftForReviewCount, "conflict must be left for review")

		require.Empty(t, resolver.calls, "partial merge must not resolve the branch notifications")
		require.Nil(t, mergedAtOf(db, forkBranchID), "partial merge must not stamp merged_at")
	})
}

// TestMergeBranchExactKeyNoSimResolvesConflict pins the exact-key same-entity
// contract for presets whose SimilarityAction is empty ("manual",
// "enrich_no_sim", "mine_no_sim"): a source object that shares a normalized
// type+key with a target HEAD is classified by content, not "similar" — a
// genuine property conflict resolves through CanonicalConflictAction (here
// "enrich"), never through the empty similarity action ("no auto-action").
// Before the fix, the exact-key path reclassified the object "similar" and, with
// an empty similarity action, left it for review, so the duplicate canonical was
// never collapsed into the target.
func TestMergeBranchExactKeyNoSimResolvesConflict(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_exact_key_nosim")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	resolver := &recordingBranchNotificationResolver{}
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil).
		WithBranchNotificationResolver(resolver)

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Exact Key Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Exact Key Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	mergedAtOf := func(branchID uuid.UUID) *time.Time {
		var mergedAt *time.Time
		require.NoError(t, db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, branchID).Scan(ctx, &mergedAt))
		return mergedAt
	}

	// Target (main) already has an entity with this type+key.
	_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type:       "Service",
		Key:        strPtr("payment-service"),
		Properties: map[string]any{"name": "Payments"},
	}, nil)
	require.NoError(t, err)

	// Source branch extracts the SAME entity (same type+key) as a fresh object —
	// a distinct canonical ID whose "name" conflicts with main's.
	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "exact-key-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type:       "Service",
		Key:        strPtr("payment-service"),
		BranchID:   &sourceBranch.ID,
		Properties: map[string]any{"name": "Payment Service", "owner": "billing"},
	}, nil)
	require.NoError(t, err)

	// enrich_no_sim → SimilarityAction == "", SimilarityEnabled == false, but
	// CanonicalConflictAction == "enrich". The exact-key match must resolve as a
	// canonical conflict (enriched), not "similar" (left for review).
	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID,
		Execute:        true,
		Policy:         "enrich_no_sim",
	})
	require.NoError(t, err)

	require.Zero(t, resp.SimilarCount, "exact-key match must never be classified 'similar'")
	require.Zero(t, resp.LeftForReviewCount,
		"exact-key match must resolve through CanonicalConflictAction, not be left for review")

	// The reclassified object must be flagged "conflict" + "enriched" in the
	// summary, matching applyMerge (target wins conflicts, source adds new keys).
	var foundConflict bool
	for _, o := range resp.Objects {
		if o.Status == "conflict" && o.Resolution == "enriched" {
			foundConflict = true
		}
	}
	require.True(t, foundConflict, "reclassified object must carry status=conflict, resolution=enriched")

	// The merge fully resolved → the branch is closed and its notification
	// resolved.
	require.NotEmpty(t, resolver.calls, "a full merge must resolve the branch notifications")
	require.NotNil(t, mergedAtOf(sourceBranch.ID), "a full merge must stamp merged_at")
}

// TestMergeBranchSameEntityReExtractionCollapsesCanonical reproduces the #1186
// re-extraction backfill: CreateOrUpdate on a staging branch mints a NEW
// canonical for an entity that already exists on main (the branch-scoped
// upsert lookup misses it). The merge must recognize the exact (type,key) match
// as the same entity and fast-forward it into main's canonical, leaving ONE
// canonical on main and no "similar"/left-for-review leftover.
func TestMergeBranchSameEntityReExtractionCollapsesCanonical(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_same_entity_reextract")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Same Entity Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Same Entity Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	// Seed the entity on main.
	mainObj, _, err := svc.CreateOrUpdate(ctx, pid, &graph.CreateGraphObjectRequest{
		Type:       "Person",
		Key:        strPtr("barry-farber"),
		Properties: map[string]any{"name": "Barry Farber"},
	}, nil)
	require.NoError(t, err)
	mainCanonical := mainObj.CanonicalID

	// Re-extract the SAME entity onto a staging branch with additive properties
	// (a schema-change backfill adds a field). The branch-scoped upsert lookup
	// misses main's object and mints a NEW canonical — the #1186 bug.
	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "reextract-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	_, _, err = svc.CreateOrUpdate(ctx, pid, &graph.CreateGraphObjectRequest{
		Type:       "Person",
		Key:        strPtr("barry-farber"),
		BranchID:   &sourceBranch.ID,
		Properties: map[string]any{"name": "Barry Farber", "role": "radio host"},
	}, nil)
	require.NoError(t, err)

	// Merge with the auto-mode default policy.
	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID,
		Execute:        true,
		Policy:         "partial",
	})
	require.NoError(t, err)

	require.Zero(t, resp.SimilarCount, "re-extraction of an existing entity must not be 'similar'")
	require.Zero(t, resp.LeftForReviewCount, "re-extraction of an existing entity must not be left for review")
	require.Equal(t, 1, resp.FastForwardCount, "re-extraction must fast-forward into the existing canonical")

	// Exactly ONE canonical on main for this (type, key) — no duplicate.
	var mainCanonicals int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.graph_objects
		 WHERE project_id = ? AND type = 'Person' AND key = 'barry-farber'
		   AND branch_id IS NULL AND supersedes_id IS NULL`, pid,
	).Scan(ctx, &mainCanonicals))
	require.Equal(t, 1, mainCanonicals, "merge must collapse to a single main canonical")

	// The surviving canonical is main's original one, and the branch source row
	// is stamped as merged into it.
	var headCanonical uuid.UUID
	require.NoError(t, db.NewRaw(
		`SELECT canonical_id FROM kb.graph_objects
		 WHERE project_id = ? AND type = 'Person' AND key = 'barry-farber'
		   AND branch_id IS NULL AND supersedes_id IS NULL`, pid,
	).Scan(ctx, &headCanonical))
	require.Equal(t, mainCanonical, headCanonical, "surviving canonical must be main's original")

	var mergedTo *uuid.UUID
	require.NoError(t, db.NewRaw(
		`SELECT merged_to_canonical_id FROM kb.graph_objects
		 WHERE project_id = ? AND branch_id = ? AND key = 'barry-farber'
		   AND supersedes_id IS NULL`, pid, sourceBranch.ID,
	).Scan(ctx, &mergedTo))
	require.NotNil(t, mergedTo, "branch source must be stamped merged_to")
	require.Equal(t, mainCanonical, *mergedTo, "branch source must point at main's canonical")
}

// TestMergeBranchExactKeyIdenticalDuplicateMintsNoVersion pins the cheapest
// exact-key same-entity case: the re-extraction produced identical content and
// name. classifySameEntity reports fast_forward (the cross-canonical mapping
// still has to be recorded), but applyMerge must absorb the duplicate without
// minting a redundant version on the target — and the branch must still close.
func TestMergeBranchExactKeyIdenticalDuplicateMintsNoVersion(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_exact_key_identical")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Exact Identical Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Exact Identical Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	name := "Central Perk"
	props := map[string]any{"name": "Central Perk"}
	target, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Place", Key: strPtr("central-perk"), Name: &name, Properties: props,
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "identical-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	src, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Place", Key: strPtr("central-perk"), Name: &name,
		BranchID: &sourceBranch.ID, Properties: props,
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial",
	})
	require.NoError(t, err)

	require.Equal(t, 0, resp.AddedCount, "an exact-key duplicate must not be added as a new entity")
	require.Equal(t, 0, resp.SimilarCount, "exact-key identity must not be deferred to similarity")
	require.Equal(t, 1, resp.FastForwardCount, "an exact-key duplicate is classified fast_forward")
	require.Equal(t, 0, resp.LeftForReviewCount, "an identical duplicate must not wait for review")
	require.NotNil(t, resp.AppliedObjects)
	require.Equal(t, 0, *resp.AppliedObjects, "an identical duplicate writes no new version")

	var status string
	for _, o := range resp.Objects {
		if o.CanonicalID == src.CanonicalID {
			status = o.Status
		}
	}
	require.Equal(t, "fast_forward", status, "the duplicate must be classified against the existing entity")

	// The main graph still has exactly one head for the entity, on the ORIGINAL
	// canonical and at version 1: no redundant version was minted.
	var mainVersion int
	var mainCanonical uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT version, canonical_id FROM kb.graph_objects
		WHERE project_id = ? AND key = 'central-perk' AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid).Scan(ctx, &mainVersion, &mainCanonical))
	require.Equal(t, target.CanonicalID, mainCanonical)
	require.Equal(t, 1, mainVersion, "an identical duplicate must not write another version")

	// The source row is still recorded in the merge ledger, and the branch closes.
	var mergedTo *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT merged_to_canonical_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id = ? AND canonical_id = ? AND supersedes_id IS NULL`,
		pid, sourceBranch.ID, src.CanonicalID).Scan(ctx, &mergedTo))
	require.NotNil(t, mergedTo, "the duplicate source must be stamped merged_to")
	require.Equal(t, target.CanonicalID, *mergedTo)

	var mergedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, sourceBranch.ID).Scan(ctx, &mergedAt))
	require.NotNil(t, mergedAt, "absorbing an identical duplicate must close the branch")
}

// TestMergeBranchExactKeyFastForwardUnionsLabels pins the fast-forward label
// contract for a same-entity re-extraction: the merge writes onto the existing
// target entity, so it must keep the target's curated labels while adding the
// source's — a re-extraction must never drop a human-curated label. (An
// ordinary same-canonical fast-forward keeps replace semantics so explicit
// branch label edits still apply.)
func TestMergeBranchExactKeyFastForwardUnionsLabels(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_exact_key_labels")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Exact Labels Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Exact Labels Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	name := "Barry Farber"
	target, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		Labels:     []string{"main-label"},
		Properties: map[string]any{"name": "Barry Farber"},
	}, nil)
	require.NoError(t, err)

	// The re-extraction carries a distinct label and an additive property, so it
	// is a genuine fast-forward — not the identical-duplicate absorb path.
	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "labels-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	src, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("barry-farber"), Name: &name,
		BranchID:   &sourceBranch.ID,
		Labels:     []string{"source-label"},
		Properties: map[string]any{"name": "Barry Farber", "occupation": "radio host"},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.FastForwardCount)
	require.Equal(t, 0, resp.LeftForReviewCount)
	require.Equal(t, 0, resp.AddedCount)
	require.Equal(t, 0, resp.SimilarCount)

	var mainVersion int
	var mainCanonical uuid.UUID
	var mainLabels string
	var mainProps map[string]any
	require.NoError(t, db.NewRaw(`
		SELECT version, canonical_id, labels::text AS labels, properties
		FROM kb.graph_objects
		WHERE project_id = ? AND key = 'barry-farber' AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid).Scan(ctx, &mainVersion, &mainCanonical, &mainLabels, &mainProps))
	require.Equal(t, target.CanonicalID, mainCanonical, "the existing main entity must be updated, not replaced")
	require.Equal(t, 2, mainVersion, "a genuine fast-forward mints a new version")
	require.Equal(t, "radio host", mainProps["occupation"])
	require.Contains(t, mainLabels, "main-label", "the target's curated label must survive a re-extraction")
	require.Contains(t, mainLabels, "source-label", "the source's label must be unioned in")

	var mergedTo *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT merged_to_canonical_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id = ? AND canonical_id = ? AND supersedes_id IS NULL`,
		pid, sourceBranch.ID, src.CanonicalID).Scan(ctx, &mergedTo))
	require.NotNil(t, mergedTo)
	require.Equal(t, target.CanonicalID, *mergedTo)
}

// TestMergeBranchExactKeyConflictNotRecordedMerged keeps the safety valve: an
// exact-key re-extraction whose shared property values genuinely disagree is a
// conflict. Under "partial" it must stay on the branch for a human, the target
// must keep its single version, and the source row must NOT be recorded as
// merged.
func TestMergeBranchExactKeyConflictNotRecordedMerged(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_exact_key_conflict_unmerged")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	orgID := uuid.NewString()
	require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, "Exact Conflict Org"))
	projectID := uuid.NewString()
	require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID: projectID, OrgID: orgID, Name: "Exact Conflict Project",
	}, testutil.AdminUser.ID))
	pid := uuid.MustParse(projectID)

	name := "Chandler Bing"
	target, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("chandler-bing"), Name: &name,
		Properties: map[string]any{"name": "Chandler Bing", "job": "statistician"},
	}, nil)
	require.NoError(t, err)

	sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "conflict-src"}
	require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
	src, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
		Type: "Person", Key: strPtr("chandler-bing"), Name: &name,
		BranchID:   &sourceBranch.ID,
		Properties: map[string]any{"name": "Chandler Bing", "job": "advertising copywriter"},
	}, nil)
	require.NoError(t, err)

	resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
		SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial",
	})
	require.NoError(t, err)

	require.Equal(t, 1, resp.ConflictCount)
	require.Equal(t, 1, resp.LeftForReviewCount)
	require.NotNil(t, resp.AppliedObjects)
	require.Equal(t, 0, *resp.AppliedObjects)

	var status, resolution string
	for _, o := range resp.Objects {
		if o.CanonicalID == src.CanonicalID {
			status, resolution = o.Status, o.Resolution
		}
	}
	require.Equal(t, "conflict", status)
	require.Equal(t, "skipped", resolution)

	// The target keeps its single version.
	var mainVersion int
	var mainCanonical uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT version, canonical_id FROM kb.graph_objects
		WHERE project_id = ? AND key = 'chandler-bing' AND branch_id IS NULL AND supersedes_id IS NULL`,
		pid).Scan(ctx, &mainVersion, &mainCanonical))
	require.Equal(t, target.CanonicalID, mainCanonical)
	require.Equal(t, 1, mainVersion, "an unresolved conflict must not write a version")

	// The source row is NOT recorded as merged, and the branch stays open.
	var mergedTo *uuid.UUID
	require.NoError(t, db.NewRaw(`
		SELECT merged_to_canonical_id FROM kb.graph_objects
		WHERE project_id = ? AND branch_id = ? AND canonical_id = ? AND supersedes_id IS NULL`,
		pid, sourceBranch.ID, src.CanonicalID).Scan(ctx, &mergedTo))
	require.Nil(t, mergedTo, "an unresolved conflict must not be recorded as merged")

	var mergedAt *time.Time
	require.NoError(t, db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, sourceBranch.ID).Scan(ctx, &mergedAt))
	require.Nil(t, mergedAt, "an unresolved conflict must keep the branch open")
}

// TestMergeBranchPartialNewestResolvesSameEntityDrift pins the opt-in
// same-entity drift policy (#1222). Under the default partial policy a
// re-extraction of an existing entity whose properties drifted is a conflict
// preserved on the target, so the branch never closes and its newer values stay
// invisible. partial_newest resolves that drift in favour of the newer source
// extraction for non-identity properties, while identity (name) drift and
// ordinary canonical conflicts are still left for review.
func TestMergeBranchPartialNewestResolvesSameEntityDrift(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "integration test requires database")
	}
	ctx := context.Background()

	tdb := testdb.SetupTestDBOrFail(t, ctx, "graph_partial_newest")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}
	cfg.Graph.MaxListLimit = 100_000
	repo := graph.NewRepository(db, log, cfg)
	svc := graph.NewService(repo, log, nil, nil, nil, nil, nil, graph.NoopEventSink{}, branches.NewStore(db), nil)

	seedProject := func(t *testing.T, name string) uuid.UUID {
		t.Helper()
		orgID := uuid.NewString()
		require.NoError(t, testutil.CreateTestOrganization(ctx, db, orgID, name+" Org"))
		projectID := uuid.NewString()
		require.NoError(t, testutil.CreateTestProject(ctx, db, testutil.TestProject{
			ID: projectID, OrgID: orgID, Name: name + " Project",
		}, testutil.AdminUser.ID))
		return uuid.MustParse(projectID)
	}
	branchMergedAt := func(t *testing.T, branchID uuid.UUID) *time.Time {
		t.Helper()
		var mergedAt *time.Time
		require.NoError(t, db.NewRaw(`SELECT merged_at FROM kb.branches WHERE id = ?`, branchID).Scan(ctx, &mergedAt))
		return mergedAt
	}
	mainHead := func(t *testing.T, projectID uuid.UUID, key string) (version int, name, role string) {
		t.Helper()
		require.NoError(t, db.NewRaw(`
			SELECT version, coalesce(name, ''), coalesce(properties->>'role', '')
			FROM kb.graph_objects
			WHERE project_id = ? AND type = 'Person' AND key = ? AND branch_id IS NULL AND supersedes_id IS NULL`,
			projectID, key).Scan(ctx, &version, &name, &role))
		return version, name, role
	}

	t.Run("non-identity drift takes the newer value and closes the branch", func(t *testing.T) {
		pid := seedProject(t, "Partial Newest Drift")
		name := "Barry Farber"

		_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: &name,
			Properties: map[string]any{"name": name, "role": "radio host"},
		}, nil)
		require.NoError(t, err)

		sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "drift-src"}
		require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
		src, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: &name,
			BranchID:   &sourceBranch.ID,
			Properties: map[string]any{"name": name, "role": "author", "born": 1927},
		}, nil)
		require.NoError(t, err)

		resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
			SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial_newest",
		})
		require.NoError(t, err)

		require.Equal(t, 1, resp.ConflictCount, "the property drift is still classified as a conflict")
		require.Zero(t, resp.LeftForReviewCount, "non-identity drift must not be left for review")
		require.NotNil(t, resp.AppliedObjects)
		require.Equal(t, 1, *resp.AppliedObjects)

		var resolution string
		for _, o := range resp.Objects {
			if o.CanonicalID == src.CanonicalID {
				resolution = o.Resolution
			}
		}
		require.Equal(t, "newest", resolution)

		version, gotName, role := mainHead(t, pid, "barry-farber")
		require.Equal(t, 2, version, "the drift resolution must write a new main version")
		require.Equal(t, "Barry Farber", gotName, "the entity title is identity and must not change")
		require.Equal(t, "author", role, "the newer extraction value must win")
		require.NotNil(t, branchMergedAt(t, sourceBranch.ID), "a fully resolved drift merge must close the branch")

		// The source row is recorded as collapsed into the main canonical.
		var mergedTo *uuid.UUID
		require.NoError(t, db.NewRaw(`
			SELECT merged_to_canonical_id FROM kb.graph_objects
			WHERE project_id = ? AND branch_id = ? AND canonical_id = ? AND supersedes_id IS NULL`,
			pid, sourceBranch.ID, src.CanonicalID).Scan(ctx, &mergedTo))
		require.NotNil(t, mergedTo)
	})

	t.Run("identity (name) drift still goes to review", func(t *testing.T) {
		pid := seedProject(t, "Partial Newest Identity")
		name := "Barry Farber"

		_, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: &name,
			Properties: map[string]any{"name": name, "role": "radio host"},
		}, nil)
		require.NoError(t, err)

		sourceBranch := &graph.Branch{ID: uuid.New(), ProjectID: pid, Name: "identity-src"}
		require.NoError(t, repo.CreateBranch(ctx, sourceBranch))
		_, err = svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: strPtr("Barry Farber (radio)"),
			BranchID:   &sourceBranch.ID,
			Properties: map[string]any{"name": "Barry Farber (radio)", "role": "author"},
		}, nil)
		require.NoError(t, err)

		resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
			SourceBranchID: sourceBranch.ID, Execute: true, Policy: "partial_newest",
		})
		require.NoError(t, err)
		require.Equal(t, 1, resp.LeftForReviewCount, "name drift is identity drift and must be reviewed")

		version, gotName, role := mainHead(t, pid, "barry-farber")
		require.Equal(t, 1, version, "an identity conflict must not write a version")
		require.Equal(t, "Barry Farber", gotName)
		require.Equal(t, "radio host", role)
		require.Nil(t, branchMergedAt(t, sourceBranch.ID), "an identity conflict must keep the branch open")
	})

	t.Run("ordinary canonical conflict is still preserved", func(t *testing.T) {
		pid := seedProject(t, "Partial Newest Canonical")
		name := "Barry Farber"

		created, err := svc.Create(ctx, pid, &graph.CreateGraphObjectRequest{
			Type: "Person", Key: strPtr("barry-farber"), Name: &name,
			Properties: map[string]any{"name": name, "v": 1},
		}, nil)
		require.NoError(t, err)
		_, err = svc.Patch(ctx, pid, created.ID, &graph.PatchGraphObjectRequest{Properties: map[string]any{"v": 2}}, nil)
		require.NoError(t, err)

		forkResp, err := svc.ForkBranch(ctx, pid, nil, &graph.ForkBranchRequest{Name: "canonical-branch"})
		require.NoError(t, err)
		forkBranchID := uuid.MustParse(forkResp.BranchID)
		_, err = svc.Patch(ctx, pid, created.ID, &graph.PatchGraphObjectRequest{
			BranchID: &forkBranchID, Properties: map[string]any{"v": 3},
		}, nil)
		require.NoError(t, err)

		resp, err := svc.MergeBranch(ctx, pid, nil, &graph.BranchMergeRequest{
			SourceBranchID: forkBranchID, Execute: true, Policy: "partial_newest",
		})
		require.NoError(t, err)
		require.Equal(t, 1, resp.LeftForReviewCount,
			"a canonical-id divergence is a real conflict, not re-extraction drift")
		require.Nil(t, branchMergedAt(t, forkBranchID))

		version, _, _ := mainHead(t, pid, "barry-farber")
		require.Equal(t, 2, version, "a preserved canonical conflict must not write a version")
	})
}
