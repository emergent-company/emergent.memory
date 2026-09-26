package apitoken

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// crudFixture wires a throwaway DB + the real auth middleware + the apitoken
// routes so the token-CRUD authorization can be exercised end-to-end. It reuses
// the test tokens/subjects declared by the mint-surface fixture (mintMemberToken
// = e2e-test-user, mintStrangerToken = read-only).
type crudFixture struct {
	e          *echo.Echo
	svc        *Service
	memberID   string
	strangerID string
	projectA   string
	projectB   string
}

func newCrudFixture(t *testing.T) *crudFixture {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "apitoken_crud_membership")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	ctx := context.Background()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	memberID := uuid.NewString()
	strangerID := uuid.NewString()
	orgA := uuid.NewString()
	orgB := uuid.NewString()
	projectA := uuid.NewString()
	projectB := uuid.NewString()

	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?), (?, ?, ?)`,
		memberID, mintMemberSub, "Member", strangerID, mintStrangerSub, "Stranger")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?), (?, ?)`, orgA, "A", orgB, "B")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?), (?, ?, ?)`,
		projectA, orgA, "PA", projectB, orgB, "PB")
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`,
		orgA, memberID)
	require.NoError(t, err)

	userSvc := auth.NewUserProfileService(db, log)
	m := auth.NewMiddleware(auth.MiddlewareParams{DB: db, Cfg: tdb.Config, Log: log, UserSvc: userSvc})

	e := echo.New()
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)

	repo := NewRepository(db, log)
	svc := NewService(db, repo, nil, log)
	RegisterRoutes(e, NewHandler(svc, userSvc), m)

	return &crudFixture{
		e:          e,
		svc:        svc,
		memberID:   memberID,
		strangerID: strangerID,
		projectA:   projectA,
		projectB:   projectB,
	}
}

// request issues method against path with an optional Bearer token and returns
// the recorder. body is sent verbatim; when non-empty it is sent as JSON.
func (f *crudFixture) request(t *testing.T, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

// seedProjectToken mints a project-bound token owned by userID in projectID via
// the real service path and returns its ID (the raw token value is never
// returned to the test, only the ID is used).
func (f *crudFixture) seedProjectToken(t *testing.T, projectID, userID string, scopes []string) string {
	t.Helper()
	dto, err := f.svc.Create(context.Background(), projectID, userID,
		"crud-"+uuid.NewString(), scopes)
	require.NoError(t, err)
	return dto.ID
}

// mintAccountTokenWithScopes mints a project-unbound account token owned by
// userID carrying the given scopes and returns the raw token (used only as a
// bearer; never logged).
func (f *crudFixture) mintAccountTokenWithScopes(t *testing.T, userID string, scopes []string) string {
	t.Helper()
	dto, err := f.svc.CreateAccountToken(context.Background(), userID,
		"account-"+uuid.NewString(), scopes)
	require.NoError(t, err)
	return dto.Token
}

// seedBoundToken mints a project-bound emt_* token for projectID carrying the
// given scopes and returns the raw token (used only as a bearer; never logged).
func (f *crudFixture) seedBoundToken(t *testing.T, projectID string, scopes []string) string {
	t.Helper()
	dto, err := f.svc.Create(context.Background(), projectID, f.memberID,
		"bound-"+uuid.NewString(), scopes)
	require.NoError(t, err)
	return dto.Token
}

// tokenCRUDRoutes enumerates the five project-scoped token CRUD routes with a
// path builder for the addressed token id (ignored by List).
var tokenCRUDRoutes = []struct {
	name   string
	method string
	path   func(projectID, tokenID string) string
	body   string
}{
	{"List", http.MethodGet, func(p, _ string) string { return "/api/projects/" + p + "/tokens" }, ""},
	{"Get", http.MethodGet, func(p, t string) string { return "/api/projects/" + p + "/tokens/" + t }, ""},
	{"UpdateScopes", http.MethodPatch, func(p, t string) string { return "/api/projects/" + p + "/tokens/" + t }, `{"scopes":["data:read"]}`},
	{"Revoke", http.MethodDelete, func(p, t string) string { return "/api/projects/" + p + "/tokens/" + t }, ""},
	{"Regenerate", http.MethodPost, func(p, t string) string { return "/api/projects/" + p + "/tokens/" + t + "/regenerate" }, ""},
}

// TestTokenCRUDEnforcesMembership is the fail-first reproducer for issue #878
// (session class): every token-CRUD route must deny a non-member session caller
// with 403 and allow a member of the owning org with 200. Each subtest seeds a
// fresh token so the mutating routes (UpdateScopes/Revoke/Regenerate) never
// interfere with one another.
func TestTokenCRUDEnforcesMembership(t *testing.T) {
	f := newCrudFixture(t)

	for _, r := range tokenCRUDRoutes {
		t.Run(r.name, func(t *testing.T) {
			tokenID := f.seedProjectToken(t, f.projectA, f.memberID, []string{"data:read"})
			path := r.path(f.projectA, tokenID)

			rec := f.request(t, r.method, path, mintMemberToken, r.body)
			require.Equal(t, http.StatusOK, rec.Code, "member session %s must succeed", r.name)

			rec = f.request(t, r.method, path, mintStrangerToken, r.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "non-member session %s must be 403", r.name)

			rec = f.request(t, r.method, path, "", r.body)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "unauthenticated %s must be 401", r.name)
		})
	}
}

// TestTokenCRUDAccountTokenBoundedByMembership is the fail-first reproducer for
// the account-token class (issue #883/#878): a project-unbound account token is
// bounded by its owner's membership. A member's account token reaches the CRUD
// surface (200); a non-member's account token is refused (403), and a member's
// account token is refused for a foreign project (403).
func TestTokenCRUDAccountTokenBoundedByMembership(t *testing.T) {
	f := newCrudFixture(t)

	memberAccount := f.mintAccountTokenWithScopes(t, f.memberID, []string{"data:read"})
	strangerAccount := f.mintAccountTokenWithScopes(t, f.strangerID, []string{"data:read"})

	for _, r := range tokenCRUDRoutes {
		t.Run(r.name, func(t *testing.T) {
			tokenID := f.seedProjectToken(t, f.projectA, f.memberID, []string{"data:read"})
			path := r.path(f.projectA, tokenID)

			rec := f.request(t, r.method, path, memberAccount, r.body)
			require.Equal(t, http.StatusOK, rec.Code, "member's account token %s must succeed", r.name)

			rec = f.request(t, r.method, path, strangerAccount, r.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "stranger's account token %s must be 403", r.name)
		})
	}

	// A member's account token must be refused for a foreign project (project B).
	foreignPath := tokenCRUDRoutes[0].path(f.projectB, uuid.NewString()) // List on project B
	rec := f.request(t, http.MethodGet, foreignPath, memberAccount, "")
	require.Equal(t, http.StatusForbidden, rec.Code, "account token enumerating a foreign project must be 403")
}

// TestTokenCRUDProjectBoundTokenUnchanged confirms the project-bound emt_* token
// behaviour is unchanged by the fix: a project-A-bound token reaches project A's
// CRUD surface (200), while a project-B-bound token is refused (403) by the
// token↔project binding, and a project-A-bound token listing a caller-supplied
// foreign :projectId is also refused (403).
func TestTokenCRUDProjectBoundTokenUnchanged(t *testing.T) {
	f := newCrudFixture(t)

	// A project-A-bound token reaches project A's CRUD surface.
	boundA := f.seedBoundToken(t, f.projectA, []string{"data:read"})

	rec := f.request(t, http.MethodGet, "/api/projects/"+f.projectA+"/tokens", boundA, "")
	require.Equal(t, http.StatusOK, rec.Code, "project-A-bound token listing project A must succeed")

	// A project-B-bound token is refused for project A (binding mismatch).
	boundB := f.seedBoundToken(t, f.projectB, []string{"data:read"})
	rec = f.request(t, http.MethodGet, "/api/projects/"+f.projectA+"/tokens", boundB, "")
	require.Equal(t, http.StatusForbidden, rec.Code, "cross-project emt_* token must be 403")

	// A project-A-bound token cannot enumerate a caller-supplied foreign
	// :projectId — RequireProjectTokenScope rejects the mismatch before List runs.
	rec = f.request(t, http.MethodGet, "/api/projects/"+f.projectB+"/tokens", boundA, "")
	require.Equal(t, http.StatusForbidden, rec.Code, "bound token addressing a foreign :projectId must be 403")
}
