package projects

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestRepositoryUpdate_LeavesCycleAnchorsAlone reproduces the race that makes
// the anchor columns a write hazard: a settings save loads the row, an admin's
// reset lands, and the save writes the loaded model back. If Update wrote the
// anchors it would put the stale (pre-reset) value back and silently move the
// cycle — the one thing only the reset endpoint may do.
//
// The test holds a stale copy across a real anchor write, which is the whole
// point: an end-to-end PATCH reloads after the reset and would pass either way.
func TestRepositoryUpdate_LeavesCycleAnchorsAlone(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "project_anchor_update")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	repo := NewRepository(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID := uuid.NewString()
	projectID := uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Anchor Org").Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(
		`INSERT INTO kb.projects (id, organization_id, name, budget_usd, budget_weekly_usd)
		 VALUES (?, ?, 'Anchor Project', 10, 2.5)`,
		projectID, orgID,
	).Exec(ctx)
	require.NoError(t, err)

	// A settings save that loaded the project before the reset.
	stale, err := repo.GetByID(ctx, projectID, false)
	require.NoError(t, err)
	require.Nil(t, stale.BudgetWeekAnchor, "precondition: no anchor yet")

	// The admin resets the weekly window in between.
	resetAt := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, repo.ResetBudgetWindowAnchor(ctx, projectID, "week", resetAt))

	// The settings save now writes its stale copy back.
	stale.Name = "Anchor Project Renamed"
	require.NoError(t, repo.Update(ctx, stale))

	var row struct {
		Name       string     `bun:"name"`
		WeekAnchor *time.Time `bun:"budget_week_anchor"`
	}
	require.NoError(t, db.NewRaw(
		`SELECT name, budget_week_anchor FROM kb.projects WHERE id = ?`, projectID,
	).Scan(ctx, &row))

	require.Equal(t, "Anchor Project Renamed", row.Name, "the save must still apply its own change")
	require.NotNil(t, row.WeekAnchor, "a settings save must not clear the anchor a reset just set")
	require.WithinDuration(t, resetAt, *row.WeekAnchor, time.Second,
		"a settings save must not move the cycle boundary")
}
