package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestBranchWriteToolsExposeBranch pins the catalog half of #317: every verb
// that mutates the graph must accept `branch`, so a branch can hold a complete
// change set. The consolidated families derive their parameter set from these
// definitions, so the same assertion covers `entity-write` and
// `relationship-write`.
func TestBranchWriteToolsExposeBranch(t *testing.T) {
	svc := &Service{}

	for _, name := range []string{
		"entity-create",
		"entity-update",
		"entity-delete",
		"entity-restore",
		"relationship-create",
		"relationship-update",
		"relationship-delete",
	} {
		def := toolDefByName(t, svc, name)
		prop, ok := def.InputSchema.Properties["branch"]
		require.True(t, ok, "%s must expose a branch parameter", name)
		assert.Equal(t, "string", prop.Type, "%s branch parameter type", name)
		assert.NotEmpty(t, prop.Description, "%s branch parameter needs a description", name)
		for _, required := range def.InputSchema.Required {
			assert.NotEqual(t, "branch", required, "%s must not require branch", name)
		}
	}

	for _, name := range []string{"entity-write", "relationship-write"} {
		def := toolDefByName(t, svc, name)
		_, ok := def.InputSchema.Properties["branch"]
		assert.True(t, ok, "consolidated %s must inherit branch from its members", name)
	}
}

// ── DB-backed behaviour ──────────────────────────────────────────────────────
// These prove the write lands where the argument says. They skip when no test
// database is available (connectTestDB → testdb.SetupTestDBOrFail), and the
// rest of the package's DB suite already depends on that same harness.

// seedBranch inserts a manual branch row and returns its id.
func seedBranch(t *testing.T, db bun.IDB, projectID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO kb.branches (id, project_id, name, origin) VALUES (?, ?, ?, 'manual')`,
		id, projectID, name)
	require.NoError(t, err)
	return id
}

// insertBranchGraphObject inserts a HEAD object on branchID and returns its
// physical id (equal to its canonical id at version 1).
func insertBranchGraphObject(t *testing.T, db bun.IDB, projectID string, branchID uuid.UUID, typ, key string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, 1, ?, ?, 'active', '{}'::jsonb, '{}'::text[], now(), now())
	`, id, projectID, branchID, id, typ, key)
	require.NoError(t, err)
	return id
}

// insertBranchRelationship inserts a HEAD relationship on branchID.
func insertBranchRelationship(t *testing.T, db bun.IDB, projectID string, branchID uuid.UUID, relType, srcID, dstID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO kb.graph_relationships
			(id, project_id, canonical_id, supersedes_id, version, type, src_id, dst_id,
			 properties, branch_id, deleted_at, created_at)
		VALUES (?, ?, ?, NULL, 1, ?, ?, ?, '{}'::jsonb, ?, NULL, now())
	`, id, projectID, id, relType, srcID, dstID, branchID)
	require.NoError(t, err)
	return id
}

type graphObjectHeadRow struct {
	ID           uuid.UUID  `bun:"id"`
	BranchID     *uuid.UUID `bun:"branch_id"`
	DeletedAt    *time.Time `bun:"deleted_at"`
	Version      int        `bun:"version"`
	SupersedesID *uuid.UUID `bun:"supersedes_id"`
}

func objectHeads(t *testing.T, db bun.IDB, projectID, canonicalID string) []graphObjectHeadRow {
	t.Helper()
	var rows []graphObjectHeadRow
	err := db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "branch_id", "deleted_at", "version", "supersedes_id").
		Where("project_id = ? AND canonical_id = ? AND supersedes_id IS NULL", projectID, canonicalID).
		Scan(context.Background(), &rows)
	require.NoError(t, err)
	return rows
}

func objectHeadForBranch(rows []graphObjectHeadRow, branchID *uuid.UUID) *graphObjectHeadRow {
	for i := range rows {
		row := &rows[i]
		if branchID == nil {
			if row.BranchID == nil {
				return row
			}
			continue
		}
		if row.BranchID != nil && *row.BranchID == *branchID {
			return row
		}
	}
	return nil
}

type graphRelationshipHeadRow struct {
	ID           uuid.UUID              `bun:"id"`
	BranchID     *uuid.UUID             `bun:"branch_id"`
	DeletedAt    *time.Time             `bun:"deleted_at"`
	Version      int                    `bun:"version"`
	SupersedesID *uuid.UUID             `bun:"supersedes_id"`
	Properties   map[string]interface{} `bun:"properties,type:jsonb"`
}

func relationshipHeads(t *testing.T, db bun.IDB, projectID, canonicalID string) []graphRelationshipHeadRow {
	t.Helper()
	var rows []graphRelationshipHeadRow
	err := db.NewSelect().
		TableExpr("kb.graph_relationships").
		Column("id", "branch_id", "deleted_at", "version", "supersedes_id", "properties").
		Where("project_id = ? AND canonical_id = ? AND supersedes_id IS NULL", projectID, canonicalID).
		Scan(context.Background(), &rows)
	require.NoError(t, err)
	return rows
}

func relationshipHeadForBranch(rows []graphRelationshipHeadRow, branchID *uuid.UUID) *graphRelationshipHeadRow {
	for i := range rows {
		row := &rows[i]
		if branchID == nil {
			if row.BranchID == nil {
				return row
			}
			continue
		}
		if row.BranchID != nil && *row.BranchID == *branchID {
			return row
		}
	}
	return nil
}

func TestEntityDeleteWithoutBranchWritesMain(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	entityID := insertGraphObject(t, db, projectID, "Person", "del-main")

	res, err := svc.executeDeleteEntity(context.Background(), projectID, map[string]any{"entity_id": entityID})
	require.NoError(t, err)
	assert.Contains(t, toolResultText(t, res, nil), entityID)

	heads := objectHeads(t, db, projectID, entityID)
	main := objectHeadForBranch(heads, nil)
	require.NotNil(t, main, "main head must exist")
	assert.NotNil(t, main.DeletedAt, "delete without branch must tombstone the main head")
}

func TestEntityDeleteWithBranchWritesBranchOnly(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	entityID := insertGraphObject(t, db, projectID, "Person", "del-branch")
	branchID := seedBranch(t, db, projectID, "plan/delete")

	_, err := svc.executeDeleteEntity(context.Background(), projectID, map[string]any{
		"entity_id": entityID,
		"branch":    "plan/delete",
	})
	require.NoError(t, err)

	heads := objectHeads(t, db, projectID, entityID)
	main := objectHeadForBranch(heads, nil)
	require.NotNil(t, main, "main head must still exist")
	assert.Nil(t, main.DeletedAt, "a branch delete must not tombstone the main head")

	branch := objectHeadForBranch(heads, &branchID)
	require.NotNil(t, branch, "a branch tombstone must be created on the branch")
	assert.NotNil(t, branch.DeletedAt, "the branch tombstone must be deleted")
}

func TestEntityRestoreWithoutBranchWritesMain(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	entityID := insertGraphObject(t, db, projectID, "Person", "restore-main")

	_, err := svc.executeDeleteEntity(context.Background(), projectID, map[string]any{"entity_id": entityID})
	require.NoError(t, err)
	_, err = svc.executeRestoreEntity(context.Background(), projectID, map[string]any{"entity_id": entityID})
	require.NoError(t, err)

	main := objectHeadForBranch(objectHeads(t, db, projectID, entityID), nil)
	require.NotNil(t, main)
	assert.Nil(t, main.DeletedAt, "restore without branch must clear the main tombstone")
	assert.GreaterOrEqual(t, main.Version, 3)
}

func TestEntityRestoreWithBranchWritesBranchOnly(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	entityID := insertGraphObject(t, db, projectID, "Person", "restore-branch")
	branchID := seedBranch(t, db, projectID, "plan/restore")

	_, err := svc.executeDeleteEntity(context.Background(), projectID, map[string]any{
		"entity_id": entityID,
		"branch":    "plan/restore",
	})
	require.NoError(t, err)

	_, err = svc.executeRestoreEntity(context.Background(), projectID, map[string]any{
		"entity_id": entityID,
		"branch":    "plan/restore",
	})
	require.NoError(t, err)

	heads := objectHeads(t, db, projectID, entityID)
	main := objectHeadForBranch(heads, nil)
	require.NotNil(t, main)
	assert.Nil(t, main.DeletedAt, "restoring on a branch must not touch the main head")

	branch := objectHeadForBranch(heads, &branchID)
	require.NotNil(t, branch)
	assert.Nil(t, branch.DeletedAt, "the branch head must be restored")
}

func TestRelationshipCreateWithoutBranchWritesMain(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	src := insertGraphObject(t, db, projectID, "Person", "rel-main-src")
	dst := insertGraphObject(t, db, projectID, "Person", "rel-main-dst")

	res, err := svc.executeBatchCreateRelationships(context.Background(), projectID, map[string]any{
		"relationships": []any{map[string]any{"type": "KNOWS", "source_id": src, "target_id": dst}},
	})
	require.NoError(t, err)

	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Results []struct {
				Relationship struct {
					ID string `json:"id"`
				} `json:"relationship"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res, nil)), &out))
	require.True(t, out.OK)
	require.Len(t, out.Data.Results, 1)
	relID := out.Data.Results[0].Relationship.ID

	heads := relationshipHeads(t, db, projectID, relID)
	require.NotNil(t, relationshipHeadForBranch(heads, nil), "relationship without branch must be written on main")
}

func TestRelationshipCreateWithBranchWritesBranchOnly(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	branchID := seedBranch(t, db, projectID, "plan/rel-create")
	src := insertBranchGraphObject(t, db, projectID, branchID, "Person", "rel-branch-src")
	dst := insertBranchGraphObject(t, db, projectID, branchID, "Person", "rel-branch-dst")

	res, err := svc.executeBatchCreateRelationships(context.Background(), projectID, map[string]any{
		"relationships": []any{map[string]any{"type": "KNOWS", "source_id": src, "target_id": dst}},
		"branch":        "plan/rel-create",
	})
	require.NoError(t, err)

	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Results []struct {
				Relationship struct {
					ID string `json:"id"`
				} `json:"relationship"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res, nil)), &out))
	require.True(t, out.OK)
	require.Len(t, out.Data.Results, 1)
	relID := out.Data.Results[0].Relationship.ID

	heads := relationshipHeads(t, db, projectID, relID)
	require.Nil(t, relationshipHeadForBranch(heads, nil), "a branch relationship must not be written on main")
	branchHead := relationshipHeadForBranch(heads, &branchID)
	require.NotNil(t, branchHead, "the relationship must live on the branch")
	assert.Nil(t, branchHead.DeletedAt)
}

func TestRelationshipUpdateWithoutBranchWritesMain(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	src := insertGraphObject(t, db, projectID, "Person", "rel-up-main-src")
	dst := insertGraphObject(t, db, projectID, "Person", "rel-up-main-dst")
	relID := insertRelationship(t, db, projectID, "KNOWS", src, dst)

	_, err := svc.executeUpdateRelationship(context.Background(), projectID, map[string]any{
		"relationship_id": relID,
		"properties":      map[string]any{"note": "main"},
	})
	require.NoError(t, err)

	main := relationshipHeadForBranch(relationshipHeads(t, db, projectID, relID), nil)
	require.NotNil(t, main)
	assert.Equal(t, 2, main.Version)
	assert.Equal(t, "main", main.Properties["note"])
}

func TestRelationshipUpdateWithBranchWritesBranchOnly(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	branchID := seedBranch(t, db, projectID, "plan/rel-update")
	src := insertBranchGraphObject(t, db, projectID, branchID, "Person", "rel-up-branch-src")
	dst := insertBranchGraphObject(t, db, projectID, branchID, "Person", "rel-up-branch-dst")
	relID := insertBranchRelationship(t, db, projectID, branchID, "KNOWS", src, dst)

	_, err := svc.executeUpdateRelationship(context.Background(), projectID, map[string]any{
		"relationship_id": relID,
		"properties":      map[string]any{"note": "branch"},
		"branch":          "plan/rel-update",
	})
	require.NoError(t, err)

	heads := relationshipHeads(t, db, projectID, relID)
	require.Nil(t, relationshipHeadForBranch(heads, nil), "updating on a branch must not create a main version")
	branchHead := relationshipHeadForBranch(heads, &branchID)
	require.NotNil(t, branchHead)
	assert.Equal(t, 2, branchHead.Version)
	assert.Equal(t, "branch", branchHead.Properties["note"])
}

func TestRelationshipDeleteWithoutBranchWritesMain(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	src := insertGraphObject(t, db, projectID, "Person", "rel-del-main-src")
	dst := insertGraphObject(t, db, projectID, "Person", "rel-del-main-dst")
	relID := insertRelationship(t, db, projectID, "KNOWS", src, dst)

	_, err := svc.executeDeleteRelationship(context.Background(), projectID, map[string]any{"relationship_id": relID})
	require.NoError(t, err)

	main := relationshipHeadForBranch(relationshipHeads(t, db, projectID, relID), nil)
	require.NotNil(t, main)
	assert.NotNil(t, main.DeletedAt, "delete without branch must tombstone the main relationship")
}

func TestRelationshipDeleteWithBranchWritesBranchOnly(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	branchID := seedBranch(t, db, projectID, "plan/rel-delete")
	src := insertBranchGraphObject(t, db, projectID, branchID, "Person", "rel-del-branch-src")
	dst := insertBranchGraphObject(t, db, projectID, branchID, "Person", "rel-del-branch-dst")
	relID := insertBranchRelationship(t, db, projectID, branchID, "KNOWS", src, dst)

	_, err := svc.executeDeleteRelationship(context.Background(), projectID, map[string]any{
		"relationship_id": relID,
		"branch":          "plan/rel-delete",
	})
	require.NoError(t, err)

	heads := relationshipHeads(t, db, projectID, relID)
	require.Nil(t, relationshipHeadForBranch(heads, nil), "a branch delete must not create a main tombstone")
	branchHead := relationshipHeadForBranch(heads, &branchID)
	require.NotNil(t, branchHead)
	assert.NotNil(t, branchHead.DeletedAt, "the branch relationship must be tombstoned")
}

// TestEntityBranchTombstoneAppliesOnMerge pins the merge half of #317 for
// objects: a tombstone created on a branch is classified "deleted" by the merge
// machinery and applied to the target head. This is why entity-delete needs no
// special merge path — it creates an ordinary version row on the branch.
func TestEntityBranchTombstoneAppliesOnMerge(t *testing.T) {
	db := connectTestDB(t)
	_, projectID := seedProject(t, db)
	svc := newEntityCreateDedupService(t, db)
	entityID := insertGraphObject(t, db, projectID, "Person", "merge-delete")
	branchID := seedBranch(t, db, projectID, "plan/merge-delete")

	_, err := svc.executeDeleteEntity(context.Background(), projectID, map[string]any{
		"entity_id": entityID,
		"branch":    "plan/merge-delete",
	})
	require.NoError(t, err)

	// Before the merge: main is live, branch carries the tombstone.
	before := objectHeads(t, db, projectID, entityID)
	require.NotNil(t, before)
	mainBefore := objectHeadForBranch(before, nil)
	require.NotNil(t, mainBefore)
	require.Nil(t, mainBefore.DeletedAt)

	projectUUID, err := uuid.Parse(projectID)
	require.NoError(t, err)
	merge, err := svc.graphService.MergeBranch(context.Background(), projectUUID, nil, &graph.BranchMergeRequest{
		SourceBranchID: branchID,
		Execute:        true,
		Policy:         "manual",
	})
	require.NoError(t, err)
	require.True(t, merge.Applied)
	require.NotNil(t, merge.DeletedCount)
	require.Equal(t, 1, *merge.DeletedCount)

	// After the merge: the main head is the branch tombstone's decision.
	mainAfter := objectHeadForBranch(objectHeads(t, db, projectID, entityID), nil)
	require.NotNil(t, mainAfter)
	assert.NotNil(t, mainAfter.DeletedAt, "merging the branch must apply the tombstone to main")
}
