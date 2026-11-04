package notifications_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// resolveResourceAuthzFixture holds the seeded identities and resources for the
// resolve-resource authorization matrix. All IDs are fresh per test.
type resolveResourceAuthzFixture struct {
	orgA     string
	orgB     string
	projectA string
	projectB string

	memberAToken string // plain member of orgA (owns projectA)
	memberBToken string // plain member of orgB (foreign tenant)
	memberAID    string
	memberBID    string

	branchA string // related_resource_id of the actionable notification in projectA
	notifA  string // actionable notification id in projectA
}

// newResolveResourceAuthzServer builds an isolated Echo instance with ONLY the
// notifications routes registered, backed by a throwaway test database and a
// real auth middleware (so AuthorizeProject performs real org-membership
// lookups). Mirrors the invites/backups authz harnesses.
func newResolveResourceAuthzServer(t *testing.T) (*testdb.TestDB, *echo.Echo) {
	t.Helper()
	ctx := context.Background()
	testDB := testdb.SetupTestDBOrFail(t, ctx, "notif_resolve_resource_authz")
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

	repo := notifications.NewRepository(db, log)
	svc := notifications.NewService(repo, nil, log)
	h := notifications.NewHandler(svc, mw)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	notifications.RegisterRoutes(e, h, mw)

	return testDB, e
}

// seedResolveResourceAuthzFixture creates the two organizations/projects, the
// two members, and one actionable notification in projectA.
func seedResolveResourceAuthzFixture(t *testing.T, db *bun.DB) resolveResourceAuthzFixture {
	t.Helper()
	ctx := context.Background()

	f := resolveResourceAuthzFixture{
		orgA:         uuid.NewString(),
		orgB:         uuid.NewString(),
		projectA:     uuid.NewString(),
		projectB:     uuid.NewString(),
		memberAToken: "e2e-notif-member-a",
		memberBToken: "e2e-notif-member-b",
		memberAID:    uuid.NewString(),
		memberBID:    uuid.NewString(),
		branchA:      uuid.NewString(),
	}

	mustExecResolve(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-a', NOW(), NOW())`, f.orgA)
	mustExecResolve(t, db, ctx, `INSERT INTO kb.orgs (id, name, created_at, updated_at) VALUES (?, 'org-b', NOW(), NOW())`, f.orgB)
	mustExecResolve(t, db, ctx, `INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at) VALUES (?, 'project-a', ?, NOW(), NOW())`, f.projectA, f.orgA)
	mustExecResolve(t, db, ctx, `INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at) VALUES (?, 'project-b', ?, NOW(), NOW())`, f.projectB, f.orgB)

	mustExecResolve(t, db, ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())`, f.memberAID, f.memberAToken)
	mustExecResolve(t, db, ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at) VALUES (?, ?, NOW(), NOW())`, f.memberBID, f.memberBToken)

	// orgA member (owns projectA); orgB member (foreign tenant).
	mustExecResolve(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'member', NOW())`, f.orgA, f.memberAID)
	mustExecResolve(t, db, ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'member', NOW())`, f.orgB, f.memberBID)

	// One actionable, unresolved notification in projectA referencing branchA.
	rt := "graph_branch"
	rid := f.branchA
	created, err := notifications.NewRepository(db, slog.Default()).Create(ctx, &notifications.Notification{
		UserID:              f.memberAID,
		ProjectID:           &f.projectA,
		Title:               "review ready",
		Message:             "m",
		RelatedResourceType: &rt,
		RelatedResourceID:   &rid,
		RequiresAction:      true,
	})
	require.NoError(t, err)
	f.notifA = created.ID

	return f
}

func mustExecResolve(t *testing.T, db *bun.DB, ctx context.Context, query string, args ...any) {
	t.Helper()
	if _, err := db.NewRaw(query, args...).Exec(ctx); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

// doResolveResource issues POST /api/notifications/resolve-resource with the
// given JSON body and bearer token.
func doResolveResource(e *echo.Echo, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/notifications/resolve-resource", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// notificationActionStatus returns whether the notification row exists and its
// action_status (nil when still unresolved). Existence and the nullable status
// are reported separately so a NULL action_status — the correct post-condition
// for a refused resolve — is never mistaken for a missing row.
func notificationActionStatus(t *testing.T, db *bun.DB, id string) (*string, bool) {
	t.Helper()
	ctx := context.Background()

	var exists bool
	err := db.NewRaw(`SELECT EXISTS(SELECT 1 FROM kb.notifications WHERE id = ?)`, id).Scan(ctx, &exists)
	require.NoError(t, err)

	var status *string
	err = db.NewRaw(`SELECT action_status FROM kb.notifications WHERE id = ?`, id).Scan(ctx, &status)
	require.NoError(t, err)

	return status, exists
}

// TestResolveResourceAuthz pins the #2041 fix: the resolve-resource endpoint
// must authorize the body-supplied projectId against the caller, not trust it.
// A member of the addressed project's owning org succeeds; a foreign caller is
// refused 403 and no rows are updated.
func TestResolveResourceAuthz(t *testing.T) {
	t.Run("same-project caller succeeds", func(t *testing.T) {
		testDB, e := newResolveResourceAuthzServer(t)
		f := seedResolveResourceAuthzFixture(t, testDB.DB)

		body := `{"resourceType":"graph_branch","resourceId":"` + f.branchA + `","projectId":"` + f.projectA + `"}`
		rec := doResolveResource(e, f.memberAToken, body)
		require.Equal(t, http.StatusOK, rec.Code, "member of the project's org must resolve its own notifications (body: %s)", rec.Body.String())

		status, exists := notificationActionStatus(t, testDB.DB, f.notifA)
		require.True(t, exists, "notification must still exist")
		require.NotNil(t, status, "same-project resolve must mark the notification resolved")
		require.Equal(t, "resolved", *status)
	})

	t.Run("foreign-project caller rejected", func(t *testing.T) {
		testDB, e := newResolveResourceAuthzServer(t)
		f := seedResolveResourceAuthzFixture(t, testDB.DB)

		body := `{"resourceType":"graph_branch","resourceId":"` + f.branchA + `","projectId":"` + f.projectA + `"}`
		rec := doResolveResource(e, f.memberBToken, body)
		require.Equal(t, http.StatusForbidden, rec.Code, "a foreign caller must be refused (body: %s)", rec.Body.String())

		status, exists := notificationActionStatus(t, testDB.DB, f.notifA)
		require.True(t, exists, "notification must still exist")
		require.Nil(t, status, "a refused resolve must not update any rows")
	})
}
