package backups

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// authzFixture holds the seeded identities and resources for the backups
// authorization matrix. All IDs are fresh per test so no two tests share state.
type authzFixture struct {
	orgA     string
	orgB     string
	projectA string
	projectB string

	// resources owned by orgA
	backupA  string
	restoreA string

	// database-level backup (platform tier)
	dbBackup string

	// identity tokens (dynamic e2e-* tokens -> ad-hoc user profiles)
	bareToken          string // authenticated, no membership anywhere
	orgAdminAToken     string // org_admin of orgA
	orgMemberAToken    string // plain member of orgA
	orgAdminBToken     string // org_admin of orgB (foreign tenant)
	projectAdminAToken string // project_admin of projectA
	superadminToken    string // superadmin_full
}

// authzUser is one seeded principal: token string doubles as the zitadel subject.
type authzUser struct {
	token string
	id    string
}

// newAuthzServer builds an isolated Echo instance with ONLY the backups routes
// registered, backed by a throwaway test database and a real auth middleware.
// It returns the DB handle (for seeding) and the Echo router (for requests).
func newAuthzServer(t *testing.T) (*testutil.TestDB, *echo.Echo) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database-backed authz test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "bkupauthz")
	t.Cleanup(testDB.Close)
	db := testDB.DB

	log := slog.Default()

	// Enable object storage so download routes can presign (client-side, no
	// network) and the download tier can be exercised end-to-end in the matrix.
	t.Setenv("STORAGE_ENDPOINT", "http://minio:9000")
	t.Setenv("STORAGE_ACCESS_KEY", "minioadmin")
	t.Setenv("STORAGE_SECRET_KEY", "minioadmin")

	userSvc := auth.NewUserProfileService(db, log)
	mw := auth.NewMiddleware(auth.MiddlewareParams{
		DB:      db,
		Cfg:     testDB.Config,
		Log:     log,
		UserSvc: userSvc,
	})

	storageSvc, _ := storage.NewService(storage.NewConfig(), log)
	repo := NewRepository(db, log)
	creator := NewCreator(db, storageSvc, repo, log)
	importer := NewImporter(db, storageSvc, log)
	restorer := NewRestorer(db, storageSvc, repo, creator, importer, log)
	svc := NewService(repo, creator, restorer, importer, storageSvc, log)
	h := NewHandler(svc, storageSvc, log)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	RegisterRoutes(e, h, mw)

	return testDB, e
}

// seedAuthzFixture creates the identities and resources the matrix exercises.
func seedAuthzFixture(t *testing.T, db *bun.DB) authzFixture {
	t.Helper()
	ctx := context.Background()

	f := authzFixture{
		orgA:               uuid.NewString(),
		orgB:               uuid.NewString(),
		projectA:           uuid.NewString(),
		projectB:           uuid.NewString(),
		backupA:            uuid.NewString(),
		restoreA:           uuid.NewString(),
		dbBackup:           uuid.NewString(),
		bareToken:          "e2e-bk-bare",
		orgAdminAToken:     "e2e-bk-orgadmin-a",
		orgMemberAToken:    "e2e-bk-member-a",
		orgAdminBToken:     "e2e-bk-orgadmin-b",
		projectAdminAToken: "e2e-bk-projectadmin-a",
		superadminToken:    "e2e-bk-superadmin",
	}

	users := []authzUser{
		{token: f.bareToken, id: uuid.NewString()},
		{token: f.orgAdminAToken, id: uuid.NewString()},
		{token: f.orgMemberAToken, id: uuid.NewString()},
		{token: f.orgAdminBToken, id: uuid.NewString()},
		{token: f.projectAdminAToken, id: uuid.NewString()},
		{token: f.superadminToken, id: uuid.NewString()},
	}

	// orgA + projectA + backupA + restoreA; orgB + projectB.
	mustExec(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-a', NOW(), NOW())`, f.orgA)
	mustExec(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-b', NOW(), NOW())`, f.orgB)
	mustExec(t, db, ctx, `INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at) VALUES (?, 'project-a', ?, NOW(), NOW())`, f.projectA, f.orgA)
	mustExec(t, db, ctx, `INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at) VALUES (?, 'project-b', ?, NOW(), NOW())`, f.projectB, f.orgB)

	// backup in orgA (projectA), status ready.
	mustExec(t, db, ctx, `INSERT INTO kb.backups (id, organization_id, project_id, project_name, storage_key, status, progress, created_at) VALUES (?, ?, ?, 'project-a', 'backups/x/y/backup.zip', 'ready', 100, NOW())`, f.backupA, f.orgA, f.projectA)

	// restore in orgA.
	mustExec(t, db, ctx, `INSERT INTO kb.restores (id, organization_id, backup_id, mode, status, progress, created_at) VALUES (?, ?, ?, 'clone', 'pending', 0, NOW())`, f.restoreA, f.orgA, f.backupA)

	// database backup (platform tier).
	mustExec(t, db, ctx, `INSERT INTO kb.database_backups (id, status, storage_key, created_at) VALUES (?, 'completed', 'db-backups/0001.dump', NOW())`, f.dbBackup)

	// Identities.
	for _, u := range users {
		mustExec(t, db, ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())`, u.id, u.token)
	}

	// orgA memberships: org_admin + plain member.
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`, f.orgA, userIDOf(f.orgAdminAToken, users))
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'member', NOW())`, f.orgA, userIDOf(f.orgMemberAToken, users))
	// orgB: foreign org_admin.
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`, f.orgB, userIDOf(f.orgAdminBToken, users))

	// projectA membership: project_admin.
	mustExec(t, db, ctx, `INSERT INTO kb.project_memberships (project_id, user_id, role, created_at) VALUES (?, ?, 'project_admin', NOW())`, f.projectA, userIDOf(f.projectAdminAToken, users))

	// superadmin_full grant.
	mustExec(t, db, ctx, `INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, userIDOf(f.superadminToken, users))

	return f
}

func userIDOf(token string, users []authzUser) string {
	for _, u := range users {
		if u.token == token {
			return u.id
		}
	}
	return ""
}

func mustExec(t *testing.T, db *bun.DB, ctx context.Context, query string, args ...any) {
	t.Helper()
	if _, err := db.NewRaw(query, args...).Exec(ctx); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

// do issues an HTTP request against the router with an optional bearer token.
func do(e *echo.Echo, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// doJSON issues a request with a JSON body and an optional bearer token.
func doJSON(e *echo.Echo, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}
