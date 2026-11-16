package projects

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// recordingUnparker is a fake IngestionBudgetUnparker that records the projects
// it was asked to un-park.
type recordingUnparker struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (r *recordingUnparker) UnparkProjectBudget(_ context.Context, projectID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, projectID)
	return r.err
}

func (r *recordingUnparker) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ids...)
}

// TestBudgetCapRelaxed pins the predicate that decides whether a budget change
// can lift a block. A non-positive cap means unlimited (the enforcement gate's
// rule), so 50 -> 0 is a clear, while nil/0 -> 5 introduces a cap (a tightening).
func TestBudgetCapRelaxed(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name    string
		oldCap  *float64
		newCap  *float64
		relaxed bool
	}{
		{name: "raise", oldCap: f(10), newCap: f(50), relaxed: true},
		{name: "clear to zero", oldCap: f(10), newCap: f(0), relaxed: true},
		{name: "clear to nil", oldCap: f(10), newCap: nil, relaxed: true},
		{name: "clear negative", oldCap: f(10), newCap: f(-1), relaxed: true},
		{name: "lower", oldCap: f(50), newCap: f(20), relaxed: false},
		{name: "unchanged", oldCap: f(10), newCap: f(10), relaxed: false},
		{name: "introduce cap where none", oldCap: nil, newCap: f(5), relaxed: false},
		{name: "introduce cap over zero", oldCap: f(0), newCap: f(5), relaxed: false},
		{name: "both uncapped", oldCap: nil, newCap: f(0), relaxed: false},
		{name: "zero to nil", oldCap: f(0), newCap: nil, relaxed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.relaxed, budgetCapRelaxed(tt.oldCap, tt.newCap))
		})
	}
}

// TestUpdateUnparksIngestionOnBudgetRelaxation is the projects half of the #724
// regression: raising or clearing either cap through the project update path must
// call the ingestion un-park hook, while a tightening or a no-op change must not.
func TestUpdateUnparksIngestionOnBudgetRelaxation(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "project_budget_unpark")
	t.Cleanup(tdb.Close)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)
	unparker := &recordingUnparker{}
	svc := NewService(ServiceParams{Repo: repo, Log: log, IngestionUnparker: unparker})

	f := func(v float64) *float64 { return &v }
	seedProject := func(t *testing.T, monthly, weekly *float64) string {
		t.Helper()
		orgID := uuid.NewString()
		_, err := tdb.DB.ExecContext(ctx,
			`INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, ?, now(), now())`,
			orgID, "org-"+orgID)
		require.NoError(t, err)
		projectID := uuid.NewString()
		_, err = tdb.DB.ExecContext(ctx, `INSERT INTO kb.projects
			(id, organization_id, name, budget_usd, budget_weekly_usd, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, now(), now())`,
			projectID, orgID, "proj-"+projectID[:8], monthly, weekly)
		require.NoError(t, err)
		return projectID
	}

	tests := []struct {
		name            string
		monthly, weekly *float64
		req             UpdateProjectRequest
		wantCalls       bool
	}{
		{
			name: "monthly raised", monthly: f(10), weekly: f(2.5),
			req: UpdateProjectRequest{BudgetUSD: f(50)}, wantCalls: true,
		},
		{
			name: "monthly cleared", monthly: f(10), weekly: f(2.5),
			req: UpdateProjectRequest{BudgetUSD: f(0)}, wantCalls: true,
		},
		{
			name: "weekly raised", monthly: f(10), weekly: f(2.5),
			req: UpdateProjectRequest{BudgetWeeklyUSD: f(25)}, wantCalls: true,
		},
		{
			name: "monthly lowered", monthly: f(50), weekly: f(2.5),
			req: UpdateProjectRequest{BudgetUSD: f(20)}, wantCalls: false,
		},
		{
			name: "unchanged", monthly: f(10), weekly: f(2.5),
			req: UpdateProjectRequest{BudgetUSD: f(10)}, wantCalls: false,
		},
		{
			name: "cap introduced", monthly: nil, weekly: nil,
			req: UpdateProjectRequest{BudgetUSD: f(10)}, wantCalls: false,
		},
		{
			name: "unrelated field", monthly: f(10), weekly: f(2.5),
			req: UpdateProjectRequest{ProjectInfo: f2("notes")}, wantCalls: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectID := seedProject(t, tt.monthly, tt.weekly)
			unparker.mu.Lock()
			unparker.ids = nil
			unparker.mu.Unlock()

			_, err := svc.Update(ctx, projectID, tt.req)
			require.NoError(t, err)

			calls := unparker.calls()
			if tt.wantCalls {
				require.Len(t, calls, 1, "a relaxed cap must call the ingestion un-park hook")
				assert.Equal(t, projectID, calls[0])
			} else {
				assert.Empty(t, calls, "a tightening or no-op update must not un-park anything")
			}
		})
	}

	// The un-park is best-effort: a queue hiccup must not fail the budget save
	// (the bounded park delay is the fallback).
	t.Run("unpark failure does not fail the save", func(t *testing.T) {
		projectID := seedProject(t, f(10), f(2.5))
		unparker.mu.Lock()
		unparker.err = assert.AnError
		unparker.ids = nil
		unparker.mu.Unlock()
		t.Cleanup(func() {
			unparker.mu.Lock()
			unparker.err = nil
			unparker.mu.Unlock()
		})

		dto, err := svc.Update(ctx, projectID, UpdateProjectRequest{BudgetUSD: f(50)})
		require.NoError(t, err, "the save must succeed even when the un-park hook fails")
		require.NotNil(t, dto)
	})
}

func f2(s string) *string { return &s }

// TestResetBudgetPeriodUnparksIngestion covers the other admin action that lifts
// a block: restarting a spending window lowers the window's spend to zero without
// changing the cap. The reset path must wake parked ingestion too, or the admin's
// fix would still take the park delay to show up (issue #724, sibling site of the
// cap update).
func TestResetBudgetPeriodUnparksIngestion(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "project_budget_reset_unpark")
	t.Cleanup(tdb.Close)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := NewRepository(tdb.DB, log)
	unparker := &recordingUnparker{}
	svc := NewService(ServiceParams{Repo: repo, Log: log, IngestionUnparker: unparker})

	orgID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, ?, now(), now())`,
		orgID, "org-"+orgID)
	require.NoError(t, err)
	projectID := uuid.NewString()
	_, err = tdb.DB.ExecContext(ctx, `INSERT INTO kb.projects
		(id, organization_id, name, budget_usd, created_at, updated_at)
		VALUES (?, ?, ?, 10, now(), now())`, projectID, orgID, "proj-reset")
	require.NoError(t, err)

	adminID := uuid.NewString()
	// project_memberships.user_id is an FK to core.user_profiles.
	_, err = tdb.DB.ExecContext(ctx, `INSERT INTO core.user_profiles
		(id, zitadel_user_id, first_name, last_name, created_at, updated_at)
		VALUES (?, ?, '', '', now(), now())`, adminID, "zitadel-"+adminID)
	require.NoError(t, err)
	_, err = tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (id, project_id, user_id, role) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), projectID, adminID, RoleProjectAdmin)
	require.NoError(t, err)

	_, err = svc.ResetBudgetPeriod(ctx, projectID, BudgetWindowMonth, adminID)
	require.NoError(t, err)

	calls := unparker.calls()
	require.Len(t, calls, 1, "a window reset must call the ingestion un-park hook")
	assert.Equal(t, projectID, calls[0])
}
