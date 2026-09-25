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

// Fixed test tokens and their subject IDs (see pkg/auth.checkTestToken). The
// member token maps to a profile we seed as an org member; the stranger token
// maps to a profile we seed with no membership anywhere.
const (
	mintMemberToken   = "e2e-test-user" // sub test-admin-user
	mintStrangerToken = "read-only"     // sub test-user-read-only
	mintMemberSub     = "test-admin-user"
	mintStrangerSub   = "test-user-read-only"
)

// mintFixture wires a throwaway DB + the real auth middleware + the apitoken
// routes so the mint-surface authorization can be exercised end-to-end.
type mintFixture struct {
	e          *echo.Echo
	svc        *Service
	memberID   string
	strangerID string
	projectA   string
	projectB   string
}

func newMintFixture(t *testing.T) *mintFixture {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "apitoken_mint_membership")
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

	// Users. The member profile's subject matches mintMemberToken; the stranger
	// profile's subject matches mintStrangerToken.
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?), (?, ?, ?)`,
		memberID, mintMemberSub, "Member", strangerID, mintStrangerSub, "Stranger")
	require.NoError(t, err)

	// Two orgs, each owning one project.
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.orgs (id, name) VALUES (?, ?), (?, ?)`, orgA, "A", orgB, "B")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?), (?, ?, ?)`,
		projectA, orgA, "PA", projectB, orgB, "PB")
	require.NoError(t, err)

	// The member belongs to org A (owner of project A) and no other org.
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

	return &mintFixture{
		e:          e,
		svc:        svc,
		memberID:   memberID,
		strangerID: strangerID,
		projectA:   projectA,
		projectB:   projectB,
	}
}

// mint POSTs body to path with an optional Bearer token and returns the recorder.
func (f *mintFixture) mint(t *testing.T, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

// mintBoundToken mints a project-bound emt_* token for projectID via the real
// service path and returns the raw token (never logged, only used as a bearer).
func (f *mintFixture) mintBoundToken(t *testing.T, projectID string) string {
	t.Helper()
	dto, err := f.svc.Create(context.Background(), projectID, f.memberID,
		"bound-"+uuid.NewString(), []string{"data:read"})
	require.NoError(t, err)
	return dto.Token
}

// mintAccountToken mints a project-unbound account token owned by userID via the
// real service path and returns the raw token (never logged, only used as a
// bearer).
func (f *mintFixture) mintAccountToken(t *testing.T, userID string) string {
	t.Helper()
	dto, err := f.svc.CreateAccountToken(context.Background(), userID,
		"account-"+uuid.NewString(), []string{"agents:write"})
	require.NoError(t, err)
	return dto.Token
}

// TestMintRoutesEnforceMembership is the fail-first reproducer for issue #870:
// both credential-mint routes must enforce project membership. A member of the
// addressed project's owning org mints successfully (201); a non-member is
// refused (403); an unauthenticated caller is refused (401); and a project-bound
// emt_* token for a different project is refused (403).
func TestMintRoutesEnforceMembership(t *testing.T) {
	f := newMintFixture(t)

	routes := []struct {
		name string
		path string
		body string
	}{
		{"device-tokens", "/api/projects/" + f.projectA + "/device-tokens", `{"name":"device"}`},
		{"webhook-trigger-tokens", "/api/projects/" + f.projectA + "/webhook-trigger-tokens", `{"name":"webhook"}`},
	}

	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			rec := f.mint(t, r.path, mintMemberToken, r.body)
			require.Equal(t, http.StatusCreated, rec.Code, "member mint must succeed")

			rec = f.mint(t, r.path, mintStrangerToken, r.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "non-member mint must be 403")

			rec = f.mint(t, r.path, "", r.body)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "unauthenticated mint must be 401")

			boundToken := f.mintBoundToken(t, f.projectB)
			rec = f.mint(t, r.path, boundToken, r.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "cross-project emt_* token must be 403")
		})
	}
}

// TestAccountTokenMintRequiresMembership is the fail-first reproducer for issue
// #877: a project-unbound account token must only mint a project-scoped
// credential for a project whose owning org its owner is a member of. A member's
// account token mints for its own project (201) and is refused for a foreign
// project (403). The device/webhook mint routes share the same guard shape and
// are asserted alongside the primary token route.
func TestAccountTokenMintRequiresMembership(t *testing.T) {
	f := newMintFixture(t)

	memberAccount := f.mintAccountToken(t, f.memberID)
	strangerAccount := f.mintAccountToken(t, f.strangerID)

	routes := []struct {
		name string
		path string
		body string
	}{
		{"tokens", "/api/projects/" + f.projectA + "/tokens", `{"name":"acct-token","scopes":["agents:write"]}`},
		{"device-tokens", "/api/projects/" + f.projectA + "/device-tokens", `{"name":"acct-device"}`},
		{"webhook-trigger-tokens", "/api/projects/" + f.projectA + "/webhook-trigger-tokens", `{"name":"acct-webhook"}`},
	}

	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			// The member's account token mints for a project its owner belongs to.
			rec := f.mint(t, r.path, memberAccount, r.body)
			require.Equal(t, http.StatusCreated, rec.Code, "account token for a member-owned project must mint 201")

			// A stranger's account token cannot mint for the member's project.
			rec = f.mint(t, r.path, strangerAccount, r.body)
			require.Equal(t, http.StatusForbidden, rec.Code, "account token for a foreign project must be 403")
		})
	}

	// Fail-first: the member's account token must be refused for a foreign
	// project (project B, owned by an org the member does not belong to).
	rec := f.mint(t, "/api/projects/"+f.projectB+"/tokens", memberAccount, `{"name":"foreign","scopes":["agents:write"]}`)
	require.Equal(t, http.StatusForbidden, rec.Code, "account token minting for a foreign project must be 403")
}
