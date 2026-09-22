package auth

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/migrations"
)

// This file holds the database-backed half of the #749 coverage: the SQL
// user-id binding in dbProjectRole and the 00165 idempotency/org-scoping
// contract. Neither can be pinned at the unit level without a real query —
// the unit-level test seam bypasses the SQL entirely.
//
// Like every *_db_test.go in this repo, these tests skip under -short and when
// Postgres is unreachable, so they run in local/integration runs, not the
// default CI unit job. They never touch the configured application database:
// the harness creates (and drops) a uniquely named throwaway database on the
// configured server.

// authDBTestDDL is the minimal schema needed to exercise the two SQL paths
// under test: the project/organization membership tables and the
// core.user_profiles rows their user_id foreign keys reference. It mirrors the
// column types, the user FK, and the (project_id, user_id) uniqueness of the
// real schema (see apps/server/internal/testutil/schema.sql) but omits
// unrelated tables and RLS policies.
var authDBTestDDL = []string{
	`CREATE SCHEMA IF NOT EXISTS core`,
	`CREATE SCHEMA IF NOT EXISTS kb`,
	`CREATE TABLE core.user_profiles (
		id uuid PRIMARY KEY,
		zitadel_user_id text NOT NULL UNIQUE,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE kb.project_memberships (
		id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		project_id uuid NOT NULL,
		user_id uuid NOT NULL REFERENCES core.user_profiles(id) ON DELETE CASCADE,
		role text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now(),
		CONSTRAINT project_memberships_project_id_user_id_key UNIQUE (project_id, user_id)
	)`,
	`CREATE TABLE kb.organization_memberships (
		id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		organization_id uuid NOT NULL,
		user_id uuid NOT NULL REFERENCES core.user_profiles(id) ON DELETE CASCADE,
		role text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now(),
		CONSTRAINT organization_memberships_organization_id_user_id_key UNIQUE (organization_id, user_id)
	)`,
}

// setupAuthDBTest creates a throwaway database on the configured Postgres
// server, applies authDBTestDDL, and returns a bun handle to it. The database
// is dropped on cleanup.
func setupAuthDBTest(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	baseCfg, err := config.NewConfig(log)
	if err != nil {
		t.Skipf("skipping: load config: %v", err)
	}

	adminCfg := baseCfg.Database
	adminCfg.Database = "postgres"
	adminPool, err := newAuthTestPool(ctx, adminCfg)
	if err != nil {
		t.Skipf("skipping: database unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		t.Skipf("skipping: database unavailable: %v", err)
	}

	dbName := fmt.Sprintf("auth_scope_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		adminPool.Close()
		t.Skipf("skipping: cannot create throwaway database: %v", err)
	}

	testCfg := baseCfg.Database
	testCfg.Database = dbName
	pool, err := newAuthTestPool(ctx, testCfg)
	if err != nil {
		dropAuthTestDB(ctx, adminPool, dbName)
		t.Fatalf("connect to throwaway database %s: %v", dbName, err)
	}

	sqldb := stdlib.OpenDBFromPool(pool)
	db := bun.NewDB(sqldb, pgdialect.New())

	t.Cleanup(func() {
		_ = db.Close()
		pool.Close()
		dropAuthTestDB(context.Background(), adminPool, dbName)
		adminPool.Close()
	})

	for _, stmt := range authDBTestDDL {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply test ddl: %v\n%s", err, stmt)
		}
	}

	return db
}

func newAuthTestPool(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, err
	}
	poolCfg.MaxConns = 4
	poolCfg.ConnConfig.ConnectTimeout = 5 * time.Second
	return pgxpool.NewWithConfig(ctx, poolCfg)
}

func dropAuthTestDB(ctx context.Context, adminPool *pgxpool.Pool, name string) {
	_, _ = adminPool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
	_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", name))
}

// seedAuthTestUser inserts a core.user_profiles row and returns its id.
func seedAuthTestUser(t *testing.T, ctx context.Context, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`, id, "zitadel-"+id); err != nil {
		t.Fatalf("seed user profile: %v", err)
	}
	return id
}

// --- user-id binding in the membership lookup ------------------------------

// dbProjectRole must bind on the user id, not merely on the project. A user
// with no membership in a project returns "" even though other users ARE
// members of that project, and a membership in one project does not leak into
// another. Each negative probe is designed to fail if the corresponding WHERE
// column is dropped or mis-bound.
func TestDBProjectRoleBindsProjectAndUser(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	projectA := uuid.NewString()
	projectB := uuid.NewString()
	memberID := seedAuthTestUser(t, ctx, db)   // member of projectA (viewer)
	siblingID := seedAuthTestUser(t, ctx, db)  // a second member of projectA (admin)
	strangerID := seedAuthTestUser(t, ctx, db) // member of neither project

	for _, m := range []struct {
		project, user, role string
	}{
		{projectA, memberID, RoleProjectViewer},
		{projectA, siblingID, RoleProjectAdmin},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, ?)`,
			m.project, m.user, m.role); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}

	m := &Middleware{db: db, cfg: &config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	roleOf := func(t *testing.T, project, user string) string {
		t.Helper()
		role, err := m.dbProjectRole(ctx, project, user)
		if err != nil {
			t.Fatalf("dbProjectRole(%s, %s): %v", project, user, err)
		}
		return role
	}

	t.Run("correct user in the declared project resolves the role", func(t *testing.T) {
		if got := roleOf(t, projectA, memberID); got != RoleProjectViewer {
			t.Fatalf("role = %q, want %q", got, RoleProjectViewer)
		}
	})

	// User-id binding: a stranger is not a member of projectA even though two
	// other users are. Dropping the user_id predicate would return one of their
	// rows (LIMIT 1) and fail here.
	t.Run("a different user of the same project is not a member", func(t *testing.T) {
		if got := roleOf(t, projectA, strangerID); got != "" {
			t.Fatalf("role = %q, want empty: membership lookup must bind on user_id", got)
		}
	})

	// Project binding: memberID's projectA membership must not leak into
	// projectB. Dropping the project_id predicate would return the projectA
	// role and fail here.
	t.Run("a membership in another project does not leak", func(t *testing.T) {
		if got := roleOf(t, projectB, memberID); got != "" {
			t.Fatalf("role = %q, want empty: membership lookup must bind on project_id", got)
		}
	})
}

// End-to-end over the real SQL path (no roleLookup seam): a user with no
// membership in the declared project receives ZERO scopes, while the actual
// member receives the mapped viewer set. This is the assertion the #747
// reviewer found missing: the prior suite only pinned the project dimension.
func TestResolveOIDCScopesWrongUserHasNoMembership(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	projectID := uuid.NewString()
	memberID := seedAuthTestUser(t, ctx, db)
	nonMemberID := seedAuthTestUser(t, ctx, db)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, ?)`,
		projectID, memberID, RoleProjectViewer); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	// Keep the zero-scopes assertion deterministic: an ambient
	// ZITADEL_OIDC_DEFAULT_SCOPES would otherwise be returned for the
	// non-member by resolveOIDCScopes' default-set branch.
	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.db = db // no roleLookup seam: force the real SQL path

	rawScopes := []string{"openid", "profile"}

	// Sanity: the member resolves the viewer set through the real query.
	if got := m.resolveOIDCScopes(ctx, memberID, projectID, rawScopes); !scopesEqual(got, viewerReadOnlyScopes) {
		t.Fatalf("member scopes = %v, want the viewer read-only set", got)
	}

	// The wrong user must yield ZERO scopes, not merely a different set.
	got := m.resolveOIDCScopes(ctx, nonMemberID, projectID, rawScopes)
	if len(got) != 0 {
		t.Fatalf("wrong-user scopes = %v, want none (membership is user-id bound)", got)
	}
}

// --- migration 00165 idempotency -------------------------------------------

// migration00165UpUpdate extracts the UPDATE statement from 00165's Up section.
// The trailing COMMENT is excluded so that RowsAffected reflects the row
// rewrite, which is what the idempotency assertion checks. This assumes the Up
// section has a single data-modifying statement before the COMMENT; a second
// one added after it would not be exercised here.
func migration00165UpUpdate(t *testing.T) string {
	t.Helper()
	raw, err := migrations.FS.ReadFile("00165_normalize_project_membership_owner_role.sql")
	if err != nil {
		t.Fatalf("read migration 00165: %v", err)
	}
	src := string(raw)

	start := strings.Index(src, "-- +goose Up")
	if start < 0 {
		t.Fatal("migration 00165 is missing the goose Up marker")
	}
	body := src[start:]
	if i := strings.Index(body, "-- +goose Down"); i >= 0 {
		body = body[:i]
	}
	if i := strings.Index(body, "COMMENT ON COLUMN"); i >= 0 {
		body = body[:i]
	}
	if !strings.Contains(body, "UPDATE") {
		t.Fatal("migration 00165 Up section does not contain the UPDATE statement")
	}
	return body
}

// Applying 00165 twice must be stable: the first run rewrites the legacy
// project 'owner' role to project_admin, the second is a no-op. The org-level
// 'owner' role (where owner IS valid) must never be touched.
func TestMigration00165IdempotentAndOrgScoped(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	projectID := uuid.NewString()
	orgID := uuid.NewString()
	userID := seedAuthTestUser(t, ctx, db)

	// Seed the legacy state the migration exists to fix.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, 'owner')`,
		projectID, userID); err != nil {
		t.Fatalf("seed legacy project membership: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'owner')`,
		orgID, userID); err != nil {
		t.Fatalf("seed org membership: %v", err)
	}

	up := migration00165UpUpdate(t)

	first, err := db.ExecContext(ctx, up)
	if err != nil {
		t.Fatalf("first apply of 00165: %v", err)
	}
	firstRows, err := first.RowsAffected()
	if err != nil {
		t.Fatalf("first RowsAffected: %v", err)
	}
	if firstRows != 1 {
		t.Fatalf("first apply rewrote %d rows, want 1", firstRows)
	}

	second, err := db.ExecContext(ctx, up)
	if err != nil {
		t.Fatalf("second apply of 00165: %v", err)
	}
	secondRows, err := second.RowsAffected()
	if err != nil {
		t.Fatalf("second RowsAffected: %v", err)
	}
	if secondRows != 0 {
		t.Fatalf("second apply rewrote %d rows, want 0 (00165 must be idempotent)", secondRows)
	}

	// End state is stable and org-scoped.
	projectRole := scanRole(t, ctx, db,
		`SELECT role FROM kb.project_memberships WHERE project_id = ? AND user_id = ?`, projectID, userID)
	if projectRole != RoleProjectAdmin {
		t.Fatalf("project role = %q, want %q", projectRole, RoleProjectAdmin)
	}
	orgRole := scanRole(t, ctx, db,
		`SELECT role FROM kb.organization_memberships WHERE organization_id = ? AND user_id = ?`, orgID, userID)
	if orgRole != "owner" {
		t.Fatalf("org role = %q, want %q (00165 must not touch organization_memberships)", orgRole, "owner")
	}
}

func scanRole(t *testing.T, ctx context.Context, db *bun.DB, query string, args ...any) string {
	t.Helper()
	var role string
	if err := db.NewRaw(query, args...).Scan(ctx, &role); err != nil {
		t.Fatalf("scan role: %v", err)
	}
	return role
}
