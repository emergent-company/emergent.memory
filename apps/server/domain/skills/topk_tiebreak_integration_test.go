package skills

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindRelevant_TopKTieDeterminism verifies that FindRelevant's topK
// truncation is deterministic when multiple skills share the same cosine
// distance. All seeded skills carry an identical description_embedding, so the
// primary distance key is a maximal tie; the secondary `s.id ASC` key must
// then produce a stable, strictly-ascending id sequence across repeated calls.
func TestFindRelevant_TopKTieDeterminism(t *testing.T) {
	db := connectTestDB(t)
	vec := testVector()
	repo := NewRepository(db, slog.Default(), WithEmbedder(&fakeEmbedder{vec: vec}))
	ctx := context.Background()

	orgID, projectID := seedProject(t, db)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM kb.skills WHERE project_id = ?`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM kb.projects WHERE id = ?`, projectID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM kb.orgs WHERE id = ?`, orgID)
	})

	const topK = 5
	const numSkills = topK + 5 // 10 project-scoped skills, all equidistant.

	for i := 0; i < numSkills; i++ {
		s := testSkill("tie-"+uuid.NewString(), &projectID)
		require.NoError(t, repo.Create(ctx, s))
	}

	var first []uuid.UUID
	for run := 0; run < 20; run++ {
		got, err := repo.FindRelevant(ctx, projectID, "", vec, topK)
		require.NoError(t, err)
		require.Len(t, got, topK, "run %d: expected exactly topK skills", run)

		ids := make([]uuid.UUID, len(got))
		for i, sk := range got {
			ids[i] = sk.ID
		}

		// Determinism: every run must return the identical id sequence.
		if run == 0 {
			first = ids
		} else {
			assert.Equal(t, first, ids, "run %d returned a different id sequence", run)
		}

		// Total order: the returned sequence must be strictly ascending by id.
		for i := 1; i < len(ids); i++ {
			assert.True(t, bytes.Compare(ids[i-1][:], ids[i][:]) < 0,
				"run %d: ids not strictly ascending: %s >= %s", run, ids[i-1], ids[i])
		}
	}
}
