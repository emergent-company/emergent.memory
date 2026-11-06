package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

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
	SupersedesID *uuid.UUID `bun:"supersedes_id"`
	Type         string     `bun:"type"`
	Version      int        `bun:"version"`
}

func insertRelTypeRow(t *testing.T, ctx context.Context, db bun.IDB, projectID, id, canonicalID uuid.UUID, relType string, srcID, dstID uuid.UUID, supersedes *uuid.UUID, version int) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, type, src_id, dst_id, properties, canonical_id, supersedes_id, version)
		VALUES (?, ?, NULL, ?, ?, ?, '{}'::jsonb, ?, ?, ?)
	`, id, projectID, relType, srcID, dstID, canonicalID, supersedes, version)
	require.NoError(t, err)
}

func loadRelTypeRows(t *testing.T, ctx context.Context, db bun.IDB, projectID, srcID, dstID uuid.UUID) []relTypeRow {
	t.Helper()
	var rows []relTypeRow
	require.NoError(t, db.NewRaw(`
		SELECT id, canonical_id, supersedes_id, type, version
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

// TestMigration00264_KeepsBranchForkedHeadsApart records the shape prod actually
// has: the same (project, lower(type), src, dst) can legitimately appear once on
// main and once on a branch. The unique indexes are branch-scoped, so the
// migration must lower both without merging them.
func TestMigration00264_KeepsBranchForkedHeadsApart(t *testing.T) {
	ctx, db, _, _, projectID := setupProvenanceTest(t)

	srcID, dstID := uuid.New(), uuid.New()
	branchID := uuid.New()
	mainHead, branchHead := uuid.New(), uuid.New()

	insertRelTypeRow(t, ctx, db, projectID, mainHead, mainHead, "EXPOSES", srcID, dstID, nil, 1)
	_, err := db.ExecContext(ctx, `
		INSERT INTO kb.graph_relationships
			(id, project_id, branch_id, type, src_id, dst_id, properties, canonical_id, supersedes_id, version)
		VALUES (?, ?, ?, 'exposes', ?, ?, '{}'::jsonb, ?, NULL, 1)
	`, branchHead, projectID, branchID, srcID, dstID, branchHead)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, migration00264Up(t))
	require.NoError(t, err)

	rows := loadRelTypeRows(t, ctx, db, projectID, srcID, dstID)
	require.Len(t, rows, 2)
	heads := 0
	for _, r := range rows {
		assert.Equal(t, "exposes", r.Type)
		assert.Nil(t, r.SupersedesID, "a branch fork must stay a HEAD on its branch")
		if r.SupersedesID == nil {
			heads++
		}
	}
	assert.Equal(t, 2, heads)
}
