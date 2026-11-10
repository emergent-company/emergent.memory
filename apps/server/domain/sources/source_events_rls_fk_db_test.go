package sources

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// createRLSProbeRole creates a NOLOGIN role that is neither the schema owner
// nor a superuser nor BYPASSRLS, so row security is actually enforced when the
// tests SET ROLE to it. Grants are intentionally broad (all kb tables): the
// point is to prove the policies, not the privilege matrix.
func createRLSProbeRole(t *testing.T, ctx context.Context, db bun.IDB, suffix string) string {
	t.Helper()
	role := "rls_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "") + "_" + suffix
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s NOLOGIN", role))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`GRANT USAGE ON SCHEMA kb TO %[1]s;
		 GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA kb TO %[1]s;
		 GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA kb TO %[1]s;`, role))
	require.NoError(t, err)
	return role
}

// probeBunDB opens a dedicated pool whose every connection runs as role. It is
// used to drive the webhook ingress as a non-superuser, non-BYPASSRLS role: a
// green dedupe test as the schema owner would prove nothing about RLS.
func probeBunDB(t *testing.T, ctx context.Context, base *testdb.TestDB, role string) *bun.DB {
	t.Helper()
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(base.Config.Database.User, base.Config.Database.Password),
		Host:   fmt.Sprintf("%s:%d", base.Config.Database.Host, base.Config.Database.Port),
		Path:   "/" + base.Config.Database.Database,
	}
	q := u.Query()
	q.Set("sslmode", base.Config.Database.SSLMode)
	u.RawQuery = q.Encode()

	poolCfg, err := pgxpool.ParseConfig(u.String())
	require.NoError(t, err)
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+role)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	require.NoError(t, err)

	sqldb := stdlib.OpenDBFromPool(pool)
	db := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() {
		_ = db.Close()
		pool.Close()
	})

	// Prove the connection really runs as the probe role: if SET ROLE had been
	// a no-op, the test would silently run as the schema owner and prove
	// nothing about RLS.
	var currentUser, isSuper string
	require.NoError(t, db.NewRaw("SELECT current_user").Scan(ctx, &currentUser))
	require.Equal(t, role, currentUser, "probe pool must run as the non-superuser role")
	require.NoError(t, db.NewRaw("SELECT current_setting('is_superuser')").Scan(ctx, &isSuper))
	require.Equal(t, "off", isSuper, "probe role must not be a superuser")
	return db
}

// TestSourceEventTablesFailClosedWithoutProjectContext proves the 00291
// policies behave as intended when RLS is actually enforced: as a non-superuser
// role, an unset context sees zero rows and cannot write, while a transaction
// that sets app.current_project_id sees and writes that project's rows.
func TestSourceEventTablesFailClosedWithoutProjectContext(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_events_rls")
	defer db.Close()

	require.NoError(t, db.BeginTestTx(ctx))
	defer func() { _ = db.RollbackTestTx() }()
	conn := db.GetDB()

	orgID, projectID, otherProjectID := uuid.New(), uuid.New(), uuid.New()
	agentID, sourceID, otherSourceID := uuid.New(), uuid.New(), uuid.New()
	_, err := conn.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "rls-events-org")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?), (?, ?, ?)`,
		projectID, orgID, "rls-events-project", otherProjectID, orgID, "rls-events-other")
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`,
		agentID, "rls-events-agent", projectID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name) VALUES (?, ?, 'github_app', 'bot-a'), (?, ?, 'github_app', 'bot-b')`,
		sourceID, projectID, otherSourceID, otherProjectID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.source_triggers (project_id, source_id, event_type, agent_id) VALUES (?, ?, 'pull_request', ?)`,
		projectID, sourceID, agentID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.source_events (project_id, source_id, delivery_id, event_type, subject_type, subject_ref) VALUES (?, ?, 'seed-d1', 'pull_request', 'pull_request', 'owner/repo#1')`,
		projectID, sourceID)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.source_event_claims (project_id, source_id, subject_ref, head_sha) VALUES (?, ?, 'owner/repo#1', 'aaaa')`,
		projectID, sourceID)
	require.NoError(t, err)

	role := createRLSProbeRole(t, ctx, conn, "tables")
	// SET LOCAL ROLE is transaction-scoped, so it cannot leak onto the pooled
	// connection after the test transaction rolls back.
	_, err = conn.ExecContext(ctx, fmt.Sprintf("SET LOCAL ROLE %s", role))
	require.NoError(t, err)
	var currentUser string
	require.NoError(t, conn.NewRaw("SELECT current_user").Scan(ctx, &currentUser))
	require.Equal(t, role, currentUser, "the policy probe must run as the non-superuser role")

	count := func(table string) int {
		t.Helper()
		var n int
		require.NoError(t, conn.NewRaw("SELECT count(*) FROM "+table).Scan(ctx, &n))
		return n
	}

	// Unset context: fail closed, no rows from any of the three tables.
	require.Equal(t, 0, count("kb.source_triggers"), "unset context must not see source triggers")
	require.Equal(t, 0, count("kb.source_events"), "unset context must not see source events")
	require.Equal(t, 0, count("kb.source_event_claims"), "unset context must not see source event claims")

	// Unset context also refuses writes (WITH CHECK), rather than failing
	// silently later in the ingress. A failed statement aborts the surrounding
	// transaction, so each expected refusal runs in its own savepoint.
	expectRefused := func(query string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(ctx, "SAVEPOINT rls_probe")
		require.NoError(t, err)
		_, err = conn.ExecContext(ctx, query, args...)
		require.Error(t, err)
		require.Contains(t, err.Error(), "row-level security")
		_, err = conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT rls_probe")
		require.NoError(t, err)
	}
	expectRefused(`INSERT INTO kb.source_triggers (project_id, source_id, event_type, agent_id) VALUES (?, ?, 'issue', ?)`,
		projectID, sourceID, agentID)
	expectRefused(`INSERT INTO kb.source_events (project_id, source_id, delivery_id, event_type, subject_type, subject_ref) VALUES (?, ?, 'rls-d1', 'pull_request', 'pull_request', 'owner/repo#1')`,
		projectID, sourceID)
	expectRefused(`INSERT INTO kb.source_event_claims (project_id, source_id, subject_ref, head_sha) VALUES (?, ?, 'owner/repo#1', 'bbbb')`,
		projectID, sourceID)

	// Transaction-scoped project context: exactly that project's rows, and the
	// write is accepted.
	require.NoError(t, databaseSetProject(ctx, conn, projectID))
	require.Equal(t, 1, count("kb.source_triggers"), "project context must see exactly its trigger")
	require.Equal(t, 1, count("kb.source_events"), "project context must see exactly its event")
	require.Equal(t, 1, count("kb.source_event_claims"), "project context must see exactly its claim")
	_, err = conn.ExecContext(ctx, `INSERT INTO kb.source_events (project_id, source_id, delivery_id, event_type, subject_type, subject_ref) VALUES (?, ?, 'rls-d2', 'pull_request', 'pull_request', 'owner/repo#1')`,
		projectID, sourceID)
	require.NoError(t, err)
	require.Equal(t, 2, count("kb.source_events"))

	// The other project's rows stay invisible.
	require.NoError(t, databaseSetProject(ctx, conn, otherProjectID))
	require.Equal(t, 0, count("kb.source_events"), "project B context must not see project A's events")
}

// databaseSetProject writes the RLS project context into the current
// transaction. It uses set_config(..., true) so it dies with the transaction.
func databaseSetProject(ctx context.Context, db bun.IDB, projectID uuid.UUID) error {
	_, err := db.ExecContext(ctx, "SELECT set_config('app.current_project_id', ?, true)", projectID.String())
	return err
}

// TestDeleteSourceAppRefusedWhileSourceBound proves that deleting an org App
// cannot destroy project sources: the service refuses with an error naming the
// bound source, and the RESTRICT foreign key is the database-level backstop.
func TestDeleteSourceAppRefusedWhileSourceBound(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_app_fk")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID, sourceID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "fk-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "fk-project")
	require.NoError(t, err)
	app := &SourceApp{OrgID: orgID, Provider: "github", AppID: 4242, Slug: "bot", PrivateKeyEncrypted: []byte("k"), WebhookSecretEncrypted: []byte("s")}
	require.NoError(t, repo.CreateSourceApp(ctx, app))
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
		VALUES (?, ?, 'github_app', 'payments-bot', ?, '42', '{"repos":["owner/repo"]}'::jsonb, 'active')`, sourceID, projectID, app.ID)
	require.NoError(t, err)

	// The service refusal names the bound source.
	err = repo.DeleteSourceApp(ctx, app.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "payments-bot")
	require.Contains(t, err.Error(), sourceID.String())

	// The source survives.
	src, err := repo.FindSourceByID(ctx, sourceID)
	require.NoError(t, err)
	require.NotNil(t, src.AppID)
	require.Equal(t, app.ID, *src.AppID)

	// Direct SQL is refused by the RESTRICT foreign key, not just the service.
	_, err = db.DB.ExecContext(ctx, `DELETE FROM kb.source_apps WHERE id = ?`, app.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "sources_app_id_fkey")
	var stillThere int
	require.NoError(t, db.DB.NewRaw(`SELECT count(*) FROM kb.sources WHERE id = ?`, sourceID).Scan(ctx, &stillThere))
	require.Equal(t, 1, stillThere)

	// Detaching the source makes the delete succeed.
	_, err = db.DB.ExecContext(ctx, `UPDATE kb.sources SET app_id = NULL WHERE id = ?`, sourceID)
	require.NoError(t, err)
	require.NoError(t, repo.DeleteSourceApp(ctx, app.ID))
	_, err = repo.FindSourceAppByID(ctx, app.ID)
	require.Error(t, err)
}
