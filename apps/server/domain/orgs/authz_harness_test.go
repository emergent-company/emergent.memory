package orgs_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// authzFixture holds the seeded identities and resources for the orgs
// authorization matrix. All IDs are fresh per test so no two tests share state.
type authzFixture struct {
	orgA   string
	orgB   string
	orgDel string // disposable org owned by orgAdminA (delete target)

	// identity tokens (dynamic e2e-* tokens -> ad-hoc user profiles)
	bareToken       string // authenticated, no membership anywhere
	orgAdminAToken  string // org_admin of orgA
	memberAToken    string // plain member of orgA
	orgAdminBToken  string // org_admin of orgB (foreign tenant)
	superadminToken string // superadmin_full (platform)
}

// authzUser is one seeded principal: token string doubles as the zitadel subject.
type authzUser struct {
	token string
	id    string
}

// newAuthzServer builds an isolated Echo instance with ONLY the orgs routes
// registered, backed by a throwaway test database and a real auth middleware.
func newAuthzServer(t *testing.T) (*testutil.TestDB, *echo.Echo) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database-backed authz test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "orgsauthz")
	t.Cleanup(testDB.Close)
	db := testDB.DB

	log := slog.Default()

	userSvc := auth.NewUserProfileService(db, log)
	mw := auth.NewMiddleware(auth.MiddlewareParams{
		DB:      db,
		Cfg:     testDB.Config,
		Log:     log,
		UserSvc: userSvc,
	})

	repo := orgs.NewRepository(db, log)
	svc := orgs.NewService(orgs.ServiceParams{Repo: repo, Log: log})
	h := orgs.NewHandler(svc)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	orgs.RegisterRoutes(e, h, mw)

	return testDB, e
}

// seedAuthzFixture creates the identities and resources the matrix exercises.
func seedAuthzFixture(t *testing.T, db *bun.DB) authzFixture {
	t.Helper()
	ctx := context.Background()

	f := authzFixture{
		orgA:            uuid.NewString(),
		orgB:            uuid.NewString(),
		orgDel:          uuid.NewString(),
		bareToken:       "e2e-orgs-bare",
		orgAdminAToken:  "e2e-orgs-orgadmin-a",
		memberAToken:    "e2e-orgs-member-a",
		orgAdminBToken:  "e2e-orgs-orgadmin-b",
		superadminToken: "e2e-orgs-superadmin",
	}

	users := []authzUser{
		{token: f.bareToken, id: uuid.NewString()},
		{token: f.orgAdminAToken, id: uuid.NewString()},
		{token: f.memberAToken, id: uuid.NewString()},
		{token: f.orgAdminBToken, id: uuid.NewString()},
		{token: f.superadminToken, id: uuid.NewString()},
	}

	mustExec(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-a', NOW(), NOW())`, f.orgA)
	mustExec(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-b', NOW(), NOW())`, f.orgB)
	mustExec(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-del', NOW(), NOW())`, f.orgDel)

	// A tool setting override in orgA so the org_admin DELETE path can succeed.
	mustExec(t, db, ctx, `INSERT INTO kb.org_tool_settings (org_id, tool_name, enabled, config, updated_at) VALUES (?, 'web', true, '{}'::jsonb, NOW())`, f.orgA)

	for _, u := range users {
		mustExec(t, db, ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())`, u.id, u.token)
	}

	// orgA memberships: org_admin + plain member.
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`, f.orgA, userIDOf(f.orgAdminAToken, users))
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'member', NOW())`, f.orgA, userIDOf(f.memberAToken, users))
	// orgDel memberships (delete target): org_admin + plain member, so both a
	// legitimate admin and a same-org plain member can be exercised.
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`, f.orgDel, userIDOf(f.orgAdminAToken, users))
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'member', NOW())`, f.orgDel, userIDOf(f.memberAToken, users))
	// orgB: foreign org_admin.
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`, f.orgB, userIDOf(f.orgAdminBToken, users))

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
