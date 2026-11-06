package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/migrations"
)

// migration00264Up returns the shipped Up SQL with goose annotations stripped,
// so these tests exercise the exact migration rather than a paraphrase.
func migration00264Up(t *testing.T) string {
	t.Helper()
	b, err := migrations.FS.ReadFile("00264_normalise_graph_relationship_types.sql")
	require.NoError(t, err)
	up := strings.SplitN(string(b), "-- +goose Down", 2)[0]
	up = strings.ReplaceAll(up, "-- +goose StatementBegin", "")
	up = strings.ReplaceAll(up, "-- +goose StatementEnd", "")
	up = strings.ReplaceAll(up, "-- +goose Up", "")
	return up
}

type relTypeRow struct {
	ID           uuid.UUID  `bun:"id"`
	CanonicalID  uuid.UUID  `bun:"canonical_id"`
	BranchID     *uuid.UUID `bun:"branch_id"`
	SupersedesID *uuid.UUID `bun:"supersedes_id"`
	Type         string     `bun:"type"`
	Version      int        `bun:"version"`
}

func insertRelTypeRow(t *testing.T, ctx context.Context, db bun.IDB, projectID, id, canonicalID uuid.UUID, relType string, srcID, dstID uuid.UUID, supersedes *uuid.UUID, version int) {
	t.Helper()
	insertRelTypeRowOnBranch(t, ctx, db, projectID, nil, id, canonicalID, relType, srcID, dstID, supersedes, version)
}

func insertRelTypeRowOnBranch(t *testing.T, ctx context.Context, db bun.IDB, projectID uuid.UUID, branchID *uuid.UUID, id, canonicalID uuid.UUID, relType string, srcID, dstID uuid.UUID, supersedes *uuid.UUID, version int) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, type, src_id, dst_id, properties, canonical_id, supersedes_id, version)
		VALUES (?, ?, ?, ?, ?, ?, '{}'::jsonb, ?, ?, ?)
	`, id, projectID, branchID, relType, srcID, dstID, canonicalID, supersedes, version)
	require.NoError(t, err)
}

func loadRelTypeRows(t *testing.T, ctx context.Context, db bun.IDB, projectID, srcID, dstID uuid.UUID) []relTypeRow {
	t.Helper()
	var rows []relTypeRow
	require.NoError(t, db.NewRaw(`
		SELECT id, canonical_id, branch_id, supersedes_id, type, version
		FROM kb.graph_relationships
		WHERE project_id = ? AND src_id = ? AND dst_id = ?
		ORDER BY version ASC, id ASC
	`, projectID, srcID, dstID).Scan(ctx, &rows))
	return rows
}

// TestMigration00264_NormalisesTypesAndResolvesCollision is the mutation test
// for #175. The fixture holds two HEAD rows for the same edge that differ only
// in casing, each with a version behind it. After the migration:
//   - no row carries a non-normalised type (history included);
//   - the pair is merged per the documented rule (lowest id survives as the
//     only HEAD; the other chain is re-pointed and superseded, not deleted);
//   - the survivor's history still lists every version.
func TestMigration00264_NormalisesTypesAndResolvesCollision(t *testing.T) {
	ctx, db, repo, _, projectID := setupProvenanceTest(t)

	srcID, dstID := uuid.New(), uuid.New()

	// Make the survivor deterministic: lowest id among the two HEADs.
	headA, headB := uuid.New(), uuid.New()
	if headA.String() > headB.String() {
		headA, headB = headB, headA
	}
	survivorHead, loserHead := headA, headB
	survivorCanonical, loserCanonical := uuid.New(), uuid.New()

	// Survivor chain: v1 superseded by the v2 HEAD.
	survivorOld := uuid.New()
	insertRelTypeRow(t, ctx, db, projectID, survivorOld, survivorCanonical, "KNOWS", srcID, dstID, &survivorHead, 1)
	insertRelTypeRow(t, ctx, db, projectID, survivorHead, survivorCanonical, "knows", srcID, dstID, nil, 2)

	// Loser chain: v1 superseded by the v2 HEAD; its type collides after
	// lowering with the survivor's already-normalised HEAD.
	loserOld := uuid.New()
	insertRelTypeRow(t, ctx, db, projectID, loserOld, loserCanonical, "KNOWS", srcID, dstID, &loserHead, 1)
	insertRelTypeRow(t, ctx, db, projectID, loserHead, loserCanonical, "KNOWS", srcID, dstID, nil, 2)

	// The fixture must actually exercise the defect: three rows carry a
	// non-normalised type and the two HEADs collide once lowered.
	var preNonNormalised int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND type <> lower(type)
	`, projectID).Scan(ctx, &preNonNormalised))
	assert.Equal(t, 3, preNonNormalised)
	var preCollidingKeys int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM (
			SELECT 1 FROM kb.graph_relationships
			WHERE project_id = ? AND supersedes_id IS NULL
			GROUP BY branch_id, lower(type), src_id, dst_id
			HAVING count(*) > 1
		) x
	`, projectID).Scan(ctx, &preCollidingKeys))
	assert.Equal(t, 1, preCollidingKeys, "fixture does not contain the colliding HEAD pair")

	up := migration00264Up(t)
	_, err := db.ExecContext(ctx, up)
	require.NoError(t, err)

	// Rerun: the migration must be idempotent and must not move the merge.
	_, err = db.ExecContext(ctx, up)
	require.NoError(t, err)

	var nonNormalised int
	require.NoError(t, db.NewRaw(
		`SELECT count(*) FROM kb.graph_relationships WHERE type <> lower(type)`,
	).Scan(ctx, &nonNormalised))
	assert.Equal(t, 0, nonNormalised, "rows still carry a non-normalised type")

	rows := loadRelTypeRows(t, ctx, db, projectID, srcID, dstID)
	require.Len(t, rows, 4, "the merge deleted a history row")

	heads := 0
	for _, r := range rows {
		assert.Equal(t, "knows", r.Type)
		if r.SupersedesID == nil {
			heads++
			assert.Equal(t, survivorHead, r.ID, "lowest-id HEAD must survive")
			assert.Equal(t, survivorCanonical, r.CanonicalID)
			assert.Equal(t, 2, r.Version)
		}
	}
	assert.Equal(t, 1, heads, "the colliding pair left more than one HEAD")

	// The loser HEAD now points at the survivor.
	for _, r := range rows {
		if r.ID == loserHead {
			require.NotNil(t, r.SupersedesID)
			assert.Equal(t, survivorHead, *r.SupersedesID)
		}
	}

	// History is read by canonical_id; every version is still listed, with
	// distinct version numbers (the losing chain was shifted above v2).
	history, err := repo.GetRelationshipHistory(ctx, projectID, survivorCanonical)
	require.NoError(t, err)
	require.Len(t, history, 4, "history lost a version")
	versions := map[int]bool{}
	for _, h := range history {
		versions[h.Version] = true
		assert.Equal(t, "knows", h.Type)
	}
	assert.Len(t, versions, 4, "merged versions must be distinct: %v", versions)
	assert.True(t, versions[1] && versions[2] && versions[3] && versions[4], "unexpected version set: %v", versions)

	var headsForKey int
	require.NoError(t, db.NewRaw(`
		SELECT count(*) FROM kb.graph_relationships
		WHERE project_id = ? AND branch_id IS NULL AND type = 'knows' AND src_id = ? AND dst_id = ?
		  AND supersedes_id IS NULL
	`, projectID, srcID, dstID).Scan(ctx, &headsForKey))
	assert.Equal(t, 1, headsForKey)
}

// TestMigration00264_KeepsBranchForkedHeadsApart is the regression test for the
// cross-branch merge that the collision repair used to perform. A branch fork
// copies the source row's canonical_id verbatim (repository.go:3921), so a
// fork of the LOSING chain carries the loser's canonical_id even though that
// branch has no collision of its own. The repair must only re-point the losing
// chain on the branch where the collision actually happened.
//
// Fixture: main has a colliding pair (survivor canonical C, loser canonical D);
// branch B has a real fork of the loser, preserving canonical D. Before the
// branch-scoped UPDATE, the branch row was rewritten to canonical C and bumped
// from version 1 to 3, folding the branch's HEAD into main's chain (which is
// all GetRelationshipHistory can see, repository.go:2144).
func TestMigration00264_KeepsBranchForkedHeadsApart(t *testing.T) {
	ctx, db, repo, _, projectID := setupProvenanceTest(t)

	srcID, dstID := uuid.New(), uuid.New()
	branchID := uuid.New()

	// Make the survivor deterministic: lowest id among the two main HEADs.
	headA, headB := uuid.New(), uuid.New()
	if headA.String() > headB.String() {
		headA, headB = headB, headA
	}
	survivorHead, loserHead := headA, headB
	survivorCanonical, loserCanonical := uuid.New(), uuid.New()

	// Survivor chain on main: v1 superseded by the v2 HEAD.
	survivorOld := uuid.New()
	insertRelTypeRow(t, ctx, db, projectID, survivorOld, survivorCanonical, "KNOWS", srcID, dstID, &survivorHead, 1)
	insertRelTypeRow(t, ctx, db, projectID, survivorHead, survivorCanonical, "knows", srcID, dstID, nil, 2)

	// Losing chain on main: v1 superseded by the v2 HEAD.
	loserOld := uuid.New()
	insertRelTypeRow(t, ctx, db, projectID, loserOld, loserCanonical, "KNOWS", srcID, dstID, &loserHead, 1)
	insertRelTypeRow(t, ctx, db, projectID, loserHead, loserCanonical, "KNOWS", srcID, dstID, nil, 2)

	// A branch fork of the losing HEAD. Real forks preserve canonical_id and
	// start again at version 1 (repository.go:3921-3923). This branch has no
	// collision: it holds a single HEAD for the key.
	branchHead := uuid.New()
	insertRelTypeRowOnBranch(t, ctx, db, projectID, &branchID, branchHead, loserCanonical, "KNOWS", srcID, dstID, nil, 1)

	_, err := db.ExecContext(ctx, migration00264Up(t))
	require.NoError(t, err)

	// Rerun: idempotent, and must not move the branch fork a second time.
	_, err = db.ExecContext(ctx, migration00264Up(t))
	require.NoError(t, err)

	rows := loadRelTypeRows(t, ctx, db, projectID, srcID, dstID)
	require.Len(t, rows, 5, "the merge deleted or duplicated a row")

	var branchRows []relTypeRow
	mainHeads := 0
	for _, r := range rows {
		assert.Equal(t, "knows", r.Type)
		if r.BranchID != nil {
			require.Equal(t, branchID, *r.BranchID)
			branchRows = append(branchRows, r)
			continue
		}
		if r.SupersedesID == nil {
			mainHeads++
			assert.Equal(t, survivorHead, r.ID, "lowest-id main HEAD must survive")
			assert.Equal(t, survivorCanonical, r.CanonicalID)
			assert.Equal(t, 2, r.Version)
		}
	}
	assert.Equal(t, 1, mainHeads, "the colliding pair left more than one main HEAD")

	// The branch fork keeps its own canonical identity, version and HEAD status.
	require.Len(t, branchRows, 1, "branch fork disappeared")
	fork := branchRows[0]
	assert.Equal(t, branchHead, fork.ID)
	assert.Equal(t, loserCanonical, fork.CanonicalID, "branch fork was re-pointed at the survivor")
	assert.Equal(t, 1, fork.Version, "branch fork was version-shifted")
	assert.Nil(t, fork.SupersedesID, "a branch fork must stay a HEAD on its branch")

	// Read side is canonical-only, so this is the observable symptom: the
	// branch fork must still be the single version under the loser's canonical,
	// not a phantom version of the survivor's chain.
	loserHistory, err := repo.GetRelationshipHistory(ctx, projectID, loserCanonical)
	require.NoError(t, err)
	require.Len(t, loserHistory, 1, "loser canonical history lost its branch fork")
	assert.Equal(t, branchHead, loserHistory[0].ID)
	assert.Equal(t, 1, loserHistory[0].Version)

	survivorHistory, err := repo.GetRelationshipHistory(ctx, projectID, survivorCanonical)
	require.NoError(t, err)
	for _, h := range survivorHistory {
		assert.Nil(t, h.BranchID, "a branch fork leaked into the survivor's main history")
	}
	require.Len(t, survivorHistory, 4, "merged main chain should have 4 versions")
}

// TestMigration00264_NormalisesToWriteContract pins the migration's transform
// to the actual write contract (normalizeRelationType), not to a weaker
// "type <> lower(type)" check. For each original type the expected value is
// produced by writing the same string through Service.CreateRelationship, so
// the SQL and the Go implementation cannot drift.
//
// The camelCase case is the one prod actually holds: POC_WorksWith must become
// poc_works_with, not lower()'s poc_workswith, otherwise a normalised write of
// the same type still mints a twin.
func TestMigration00264_NormalisesToWriteContract(t *testing.T) {
	ctx, db, _, svc, projectID := setupProvenanceTest(t)

	newObject := func(prefix string) uuid.UUID {
		t.Helper()
		obj, err := svc.Create(ctx, projectID, &graph.CreateGraphObjectRequest{
			Type: "Person",
			Key:  strPtr(prefix + "-" + uuid.NewString()),
		}, nil)
		require.NoError(t, err)
		return obj.CanonicalID
	}

	// Reference endpoints for the real write path; legacy endpoints for the
	// rows the migration has to rewrite.
	refSrc, refDst := newObject("reltype-contract-ref-src"), newObject("reltype-contract-ref-dst")
	legacySrc, legacyDst := newObject("reltype-contract-legacy-src"), newObject("reltype-contract-legacy-dst")

	legacyTypes := []string{"POC_WorksWith", "works with", "camelCase"}
	type legacyRow struct {
		id       uuid.UUID
		expected string
	}
	legacyRows := make(map[string]legacyRow, len(legacyTypes))

	for _, legacyType := range legacyTypes {
		ref, err := svc.CreateRelationship(ctx, projectID, &graph.CreateGraphRelationshipRequest{
			Type:       legacyType,
			SrcID:      refSrc,
			DstID:      refDst,
			Properties: map[string]any{},
		}, nil)
		require.NoError(t, err)

		legacyID := uuid.New()
		insertRelTypeRow(t, ctx, db, projectID, legacyID, uuid.New(), legacyType, legacySrc, legacyDst, nil, 1)
		legacyRows[legacyType] = legacyRow{id: legacyID, expected: ref.Type}
	}

	// Lock the concrete values as well: this is the contract the prod row needs.
	assert.Equal(t, "poc_works_with", legacyRows["POC_WorksWith"].expected)
	assert.Equal(t, "works_with", legacyRows["works with"].expected)
	assert.Equal(t, "camel_case", legacyRows["camelCase"].expected)

	_, err := db.ExecContext(ctx, migration00264Up(t))
	require.NoError(t, err)

	for legacyType, row := range legacyRows {
		var stored string
		require.NoError(t, db.NewRaw(
			`SELECT type FROM kb.graph_relationships WHERE id = ?`, row.id,
		).Scan(ctx, &stored))
		assert.Equal(t, row.expected, stored, "migration did not match normalizeRelationType for %q", legacyType)
		assert.NotContains(t, stored, " ", "%q still contains a space", legacyType)
		assert.Equal(t, strings.ToLower(stored), stored, "%q still contains uppercase", legacyType)
	}
}
