package invites_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/invites"
	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// authzFixture holds the seeded identities and resources for the invites
// authorization matrix. All IDs are fresh per test so no two tests share state.
type authzFixture struct {
	orgA        string
	orgB        string
	inviteA     string // pending invite in orgA (org_admin-A revoke target)
	inviteSuper string // pending invite in orgA (superadmin revoke target)

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

// newAuthzServer builds an isolated Echo instance with ONLY the invites routes
// registered, backed by a throwaway test database, a real auth middleware, and a
// real orgs repository (for the server-side membership/role lookups).
func newAuthzServer(t *testing.T) (*testutil.TestDB, *echo.Echo) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database-backed authz test in short mode")
	}
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "invitesauthz")
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

	orgsRepo := orgs.NewRepository(db, log)
	svc := invites.NewService(db, nil, testDB.Config, log)
	h := invites.NewHandler(svc, testDB.Config, mw, orgsRepo, db)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	invites.RegisterRoutes(e, h, mw)

	return testDB, e
}

// seedAuthzFixture creates the identities and resources the matrix exercises.
func seedAuthzFixture(t *testing.T, db *bun.DB) authzFixture {
	t.Helper()
	ctx := context.Background()

	f := authzFixture{
		orgA:            uuid.NewString(),
		orgB:            uuid.NewString(),
		inviteA:         uuid.NewString(),
		inviteSuper:     uuid.NewString(),
		bareToken:       "e2e-inv-bare",
		orgAdminAToken:  "e2e-inv-orgadmin-a",
		memberAToken:    "e2e-inv-member-a",
		orgAdminBToken:  "e2e-inv-orgadmin-b",
		superadminToken: "e2e-inv-superadmin",
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

	// A pending org-scoped invite in orgA.
	mustExec(t, db, ctx, `INSERT INTO kb.invites (id, organization_id, email, role, token, status, created_at) VALUES (?, ?, 'invitee@example.com', 'project_user', 'tok-a', 'pending', NOW())`, f.inviteA, f.orgA)
	mustExec(t, db, ctx, `INSERT INTO kb.invites (id, organization_id, email, role, token, status, created_at) VALUES (?, ?, 'invitee2@example.com', 'project_user', 'tok-s', 'pending', NOW())`, f.inviteSuper, f.orgA)

	for _, u := range users {
		mustExec(t, db, ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())`, u.id, u.token)
	}

	// orgA memberships: org_admin + plain member.
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`, f.orgA, userIDOf(f.orgAdminAToken, users))
	mustExec(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'member', NOW())`, f.orgA, userIDOf(f.memberAToken, users))
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
