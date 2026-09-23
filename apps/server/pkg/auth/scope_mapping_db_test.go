package auth

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/migrations"
)

// loadAuthTestEnvFiles loads .env and .env.local from the nearest ancestor
// directory containing .env.local, mirroring testutil.SetupTestDB. Without
// this, config.NewConfig would read only the process environment, so a DB
// configured solely in the repo dotenv files would be missed and the tests
// would silently skip (or fall back to the default DSN).
func loadAuthTestEnvFiles() {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		envLocal := filepath.Join(dir, ".env.local")
		if _, statErr := os.Stat(envLocal); statErr == nil {
			_ = godotenv.Load(filepath.Join(dir, ".env"))
			_ = godotenv.Overload(envLocal)
			return
		}
	}
}

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
// real schema (see apps/server/internal/testdb/schema.sql) but omits
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
	`CREATE TABLE core.superadmins (
		user_id uuid NOT NULL REFERENCES core.user_profiles(id) ON DELETE CASCADE,
		role text NOT NULL,
		revoked_at timestamptz
	)`,
	`CREATE TABLE kb.orgs (
		id uuid PRIMARY KEY,
		name text NOT NULL
	)`,
	`CREATE TABLE kb.projects (
		id uuid PRIMARY KEY,
		organization_id uuid NOT NULL,
		name text NOT NULL
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
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Honor repo dotenv configuration like testutil.SetupTestDB so a local/
	// integration database does not depend on the caller re-exporting env.
	loadAuthTestEnvFiles()

	baseCfg, err := config.NewConfig(log)
	if err != nil {
		testdb.SkipOrFatal(t, "skipping: load config: %v", err)
	}

	// Prefer an explicit test DSN over ambient POSTGRES_* so these tests cannot
	// accidentally target a shared / application database.
	if err := testdb.Apply(&baseCfg.Database); err != nil {
		t.Fatalf("apply %s: %v", testdb.URLEnv, err)
	}

	adminCfg := baseCfg.Database
	adminCfg.Database = "postgres"
	adminPool, err := newAuthTestPool(ctx, adminCfg)
	if err != nil {
		testdb.SkipOrFatal(t, "skipping: database unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		testdb.SkipOrFatal(t, "skipping: database unavailable: %v", err)
	}

	dbName := fmt.Sprintf("auth_scope_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		adminPool.Close()
		testdb.SkipOrFatal(t, "skipping: cannot create throwaway database: %v", err)
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
	if got := m.resolveOIDCScopes(ctx, memberID, projectID, rawScopes, nil); !scopesEqual(got, viewerReadOnlyScopes) {
		t.Fatalf("member scopes = %v, want the viewer read-only set", got)
	}

	// The wrong user must yield ZERO scopes, not merely a different set.
	got := m.resolveOIDCScopes(ctx, nonMemberID, projectID, rawScopes, nil)
	if len(got) != 0 {
		t.Fatalf("wrong-user scopes = %v, want none (membership is user-id bound)", got)
	}
}

// A stored membership whose role is the empty string is dirty data, not "no
// membership": it must fail closed (zero scopes) rather than inherit the
// configured default scope set (#736 decision B). Copilot review on #803.
func TestResolveOIDCScopesEmptyStoredRoleFailsClosed(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	projectID := uuid.NewString()
	emptyRoleID := seedAuthTestUser(t, ctx, db)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, '')`,
		projectID, emptyRoleID); err != nil {
		t.Fatalf("seed empty-role membership: %v", err)
	}

	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.db = db // no roleLookup seam: force the real SQL path
	// A configured default must NOT be applied to an empty stored role.
	m.cfg.Zitadel.OIDCDefaultScopes = []string{"data:write"}

	rawScopes := []string{"openid", "profile"}

	got := m.resolveOIDCScopes(ctx, emptyRoleID, projectID, rawScopes, nil)
	if len(got) != 0 {
		t.Fatalf("empty stored role scopes = %v, want none (must fail closed, not the default)", got)
	}

	// Sanity: a genuine non-member (no row) still receives the configured
	// default, so the assertion above is not vacuous.
	nonMemberID := seedAuthTestUser(t, ctx, db)
	if def := m.resolveOIDCScopes(ctx, nonMemberID, projectID, rawScopes, nil); !scopesEqual(def, []string{"data:write"}) {
		t.Fatalf("non-member scopes = %v, want the configured default", def)
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

// --- entitlement-tier SQL binding -------------------------------------------

// TestDBOrgAdminBindsOrgAndUser pins the org_admin query: the membership must
// bind on BOTH the organization and the user, so an org_admin in organization A
// is not an org_admin in B, and a different user of A is not an org_admin.
func TestDBOrgAdminBindsOrgAndUser(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	memberID := seedAuthTestUser(t, ctx, db)
	strangerID := seedAuthTestUser(t, ctx, db)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`,
		orgA, memberID); err != nil {
		t.Fatalf("seed org membership: %v", err)
	}

	m := &Middleware{db: db, cfg: &config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	isAdmin := func(t *testing.T, org, user string) bool {
		t.Helper()
		ok, err := m.dbOrgAdmin(ctx, org, user)
		if err != nil {
			t.Fatalf("dbOrgAdmin(%s, %s): %v", org, user, err)
		}
		return ok
	}

	if !isAdmin(t, orgA, memberID) {
		t.Fatal("org_admin in A should be org_admin in A")
	}
	if isAdmin(t, orgB, memberID) {
		t.Fatal("org_admin in A must not be org_admin in B (org binding)")
	}
	if isAdmin(t, orgA, strangerID) {
		t.Fatal("a different user of A must not be org_admin (user binding)")
	}
}

// TestDBSuperadminRoleBindsUserAndActive pins the superadmin query: only an
// active (non-revoked) row counts, and a superadmin_readonly row is returned as
// its role (so the resolver can refuse it), never conflated with full.
func TestDBSuperadminRoleBindsUserAndActive(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	fullID := seedAuthTestUser(t, ctx, db)
	readonlyID := seedAuthTestUser(t, ctx, db)
	revokedID := seedAuthTestUser(t, ctx, db)
	strangerID := seedAuthTestUser(t, ctx, db)

	for _, m := range []struct{ user, role string }{
		{fullID, RoleSuperadminFull},
		{readonlyID, RoleSuperadminReadonly},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO core.superadmins (user_id, role) VALUES (?, ?)`, m.user, m.role); err != nil {
			t.Fatalf("seed superadmin: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO core.superadmins (user_id, role, revoked_at) VALUES (?, ?, NOW())`,
		revokedID, RoleSuperadminFull); err != nil {
		t.Fatalf("seed revoked superadmin: %v", err)
	}

	m := &Middleware{db: db, cfg: &config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	roleOf := func(t *testing.T, user string) string {
		t.Helper()
		role, err := m.dbSuperadminRole(ctx, user)
		if err != nil {
			t.Fatalf("dbSuperadminRole(%s): %v", user, err)
		}
		return role
	}

	if got := roleOf(t, fullID); got != RoleSuperadminFull {
		t.Fatalf("full role = %q, want %q", got, RoleSuperadminFull)
	}
	if got := roleOf(t, readonlyID); got != RoleSuperadminReadonly {
		t.Fatalf("readonly role = %q, want %q", got, RoleSuperadminReadonly)
	}
	if got := roleOf(t, revokedID); got != "" {
		t.Fatalf("revoked superadmin role = %q, want empty", got)
	}
	if got := roleOf(t, strangerID); got != "" {
		t.Fatalf("stranger role = %q, want empty", got)
	}
}

// TestResolveOIDCScopesOrgAdminEndToEnd exercises the org tier over the real SQL
// path: the declared project's owning org is resolved, and the org_admin
// membership in that org yields the org-administration set while a membership in
// a different org does not.
func TestResolveOIDCScopesOrgAdminEndToEnd(t *testing.T) {
	db := setupAuthDBTest(t)
	ctx := context.Background()

	adminID := seedAuthTestUser(t, ctx, db)

	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?), (?, ?)`, orgA, "A", orgB, "B"); err != nil {
		t.Fatalf("seed orgs: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projID, orgB, "B-project"); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`,
		orgA, adminID); err != nil {
		t.Fatalf("seed org_admin in A: %v", err)
	}

	clearZitadelEnv(t)
	m := newTestMiddleware(t)
	m.db = db // no seams: force the real SQL path
	m.superadminLookup = nil
	m.projectOrgLookup = nil
	m.orgAdminLookup = nil
	m.cfg.Zitadel.TrustTokenScopes = false

	got := m.resolveOIDCScopes(ctx, adminID, projID, []string{"openid"}, nil)
	if len(got) != 0 {
		t.Fatalf("org_admin in A must not grant org scopes for a project in B: %v", got)
	}

	// Move the project to org A: the same principal now resolves the org set.
	if _, err := db.ExecContext(ctx,
		`UPDATE kb.projects SET organization_id = ? WHERE id = ?`, orgA, projID); err != nil {
		t.Fatalf("re-own project: %v", err)
	}
	got = m.resolveOIDCScopes(ctx, adminID, projID, []string{"openid"}, nil)
	wantScopeSet(t, got, []string{"org:read", "org:invite:create", "org:project:create", "org:project:delete"})
}
