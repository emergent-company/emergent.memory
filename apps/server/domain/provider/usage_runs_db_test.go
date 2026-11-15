package provider

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// usageTestLog discards the repository's structured logging.
func usageTestLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedUsageProject creates the org+project pair a usage row references.
func seedUsageProject(t *testing.T, ctx context.Context, db bun.IDB) (projectID, orgID string) {
	t.Helper()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Usage Org")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "Usage Project")
	require.NoError(t, err)
	return projectID, orgID
}

// seedUsageAgent creates an agent in the project and returns its id.
func seedUsageAgent(t *testing.T, ctx context.Context, db bun.IDB, projectID, name string) string {
	t.Helper()
	agentID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'react', '@daily', ?)`,
		agentID, name, projectID)
	require.NoError(t, err)
	return agentID
}

// seedUsageRun creates a finished agent run reporting the given status.
func seedUsageRun(t *testing.T, ctx context.Context, db bun.IDB, agentID, status string, startedAt time.Time) string {
	t.Helper()
	runID := uuid.NewString()
	duration := 4200
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.agent_runs (id, agent_id, status, started_at, completed_at, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		runID, agentID, status, startedAt, startedAt.Add(time.Duration(duration)*time.Millisecond), duration)
	require.NoError(t, err)
	return runID
}

// seedUsageEvent records one usage event, optionally attributed to a run (a nil
// runID is exactly how direct API/embedding spend is recorded).
func seedUsageEvent(t *testing.T, ctx context.Context, db bun.IDB, projectID, orgID string, runID *string, cost float64, at time.Time) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO kb.llm_usage_events
		   (project_id, org_id, provider, model, text_input_tokens, output_tokens, cached_tokens,
		    estimated_cost_usd, run_id, created_at)
		 VALUES (?, ?, 'google-ai', 'gemini-2.5-pro', 1000, 200, 50, ?, ?, ?)`,
		projectID, orgID, cost, runID, at)
	require.NoError(t, err)
}

func strPtr(s string) *string { return &s }

// TestGetProjectUsageByAgent_AttributesRunsAndKeepsUnattributedSpend proves the
// breakdown a reader can reconcile with the range total: every attributed
// dollar lands on the agent that ran, and spend with no run behind it is
// reported rather than dropped.
func TestGetProjectUsageByAgent_AttributesRunsAndKeepsUnattributedSpend(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "usagebyagent")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	repo := NewRepository(db, usageTestLog())
	projectID, orgID := seedUsageProject(t, ctx, db)

	coding := seedUsageAgent(t, ctx, db, projectID, "coding")
	auditor := seedUsageAgent(t, ctx, db, projectID, "ui-auditor")

	now := time.Now().UTC().Truncate(time.Second)
	r1 := seedUsageRun(t, ctx, db, coding, "completed", now.Add(-2*time.Hour))
	r2 := seedUsageRun(t, ctx, db, coding, "completed", now.Add(-90*time.Minute))
	r3 := seedUsageRun(t, ctx, db, auditor, "failed", now.Add(-30*time.Minute))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(r1), 1.00, now.Add(-2*time.Hour))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(r2), 2.00, now.Add(-90*time.Minute))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(r3), 0.50, now.Add(-30*time.Minute))
	// Direct API / embedding spend: no run, so no agent.
	seedUsageEvent(t, ctx, db, projectID, orgID, nil, 0.25, now.Add(-10*time.Minute))
	// Outside the requested range: must not appear, and must not be counted.
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(r1), 7.00, now.AddDate(0, 0, -40))

	since := now.AddDate(0, 0, -1)
	rows, err := repo.GetProjectUsageByAgent(ctx, projectID, &since, nil)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	// Highest cost first.
	require.Equal(t, coding, rows[0].AgentID)
	require.Equal(t, "coding", rows[0].AgentName)
	require.EqualValues(t, 2, rows[0].RunCount, "two distinct runs, three events would be a join fan-out")
	require.InDelta(t, 3.00, rows[0].EstimatedCostUSD, 1e-9)

	require.Equal(t, auditor, rows[1].AgentID)
	require.EqualValues(t, 1, rows[1].RunCount)
	require.InDelta(t, 0.50, rows[1].EstimatedCostUSD, 1e-9)

	require.Equal(t, "", rows[2].AgentID, "unattributed spend is its own row")
	require.Equal(t, "", rows[2].AgentName)
	require.EqualValues(t, 0, rows[2].RunCount)
	require.InDelta(t, 0.25, rows[2].EstimatedCostUSD, 1e-9)

	// The breakdown must add up to the summary the same range reports.
	summary, err := repo.GetProjectUsageSummary(ctx, projectID, &since, nil)
	require.NoError(t, err)
	var breakdown, total float64
	for _, row := range rows {
		breakdown += row.EstimatedCostUSD
	}
	for _, row := range summary {
		total += row.EstimatedCostUSD
	}
	require.InDelta(t, total, breakdown, 1e-9, "per-agent rows must reconcile with the range total")
}

// TestGetProjectUsageRuns_LogOrdersByActivityAndPages proves the run log's
// order, its range/agent filter, and that the page and the count behind it use
// the same scope.
func TestGetProjectUsageRuns_LogOrdersByActivityAndPages(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "usageruns")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	repo := NewRepository(db, usageTestLog())
	projectID, orgID := seedUsageProject(t, ctx, db)

	coding := seedUsageAgent(t, ctx, db, projectID, "coding")
	auditor := seedUsageAgent(t, ctx, db, projectID, "ui-auditor")

	now := time.Now().UTC().Truncate(time.Second)
	older := seedUsageRun(t, ctx, db, coding, "completed", now.Add(-3*time.Hour))
	newer := seedUsageRun(t, ctx, db, coding, "completed", now.Add(-2*time.Hour))
	auditorRun := seedUsageRun(t, ctx, db, auditor, "completed", now.Add(-90*time.Minute))

	// The older run's activity is extended past the newer one's, so ordering by
	// run start and ordering by last usage event give different answers — the
	// log is a cost log and must use the latter.
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(older), 1.00, now.Add(-3*time.Hour))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(older), 1.00, now.Add(-20*time.Minute))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(newer), 2.00, now.Add(-2*time.Hour))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(auditorRun), 0.40, now.Add(-90*time.Minute))
	// A run with spend outside the range.
	outside := seedUsageRun(t, ctx, db, coding, "completed", now.AddDate(0, 0, -40))
	seedUsageEvent(t, ctx, db, projectID, orgID, strPtr(outside), 5.00, now.AddDate(0, 0, -40))

	since := now.AddDate(0, 0, -1)
	scope := usageRunScope{projectID: projectID, since: &since}

	total, err := repo.CountProjectUsageRuns(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, 3, total)

	rows, err := repo.GetProjectUsageRuns(ctx, scope, 10, 0)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, older, rows[0].RunID, "most recent activity first, not most recently started")
	require.EqualValues(t, 2, rows[0].EventCount)
	require.InDelta(t, 2.00, rows[0].EstimatedCostUSD, 1e-9)
	require.Equal(t, "coding", rows[0].AgentName)
	require.Equal(t, "completed", rows[0].Status)
	require.InDelta(t, 200.0, float64(rows[0].TotalOutput)/2, 1e-9)

	// Paging walks the same order without repeating a run.
	page2, err := repo.GetProjectUsageRuns(ctx, scope, 1, 1)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Equal(t, auditorRun, page2[0].RunID)

	// The agent filter narrows the log and its count together.
	auditScope := usageRunScope{projectID: projectID, since: &since, agentID: auditor}
	auditTotal, err := repo.CountProjectUsageRuns(ctx, auditScope)
	require.NoError(t, err)
	require.Equal(t, 1, auditTotal)
	auditRows, err := repo.GetProjectUsageRuns(ctx, auditScope, 10, 0)
	require.NoError(t, err)
	require.Len(t, auditRows, 1)
	require.Equal(t, auditorRun, auditRows[0].RunID)

	// A run with no usage events has no line in a cost log.
	quiet := seedUsageRun(t, ctx, db, coding, "completed", now.Add(-10*time.Minute))
	afterQuiet, err := repo.CountProjectUsageRuns(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, 3, afterQuiet, "run %s spent nothing and must not appear", quiet)
}
