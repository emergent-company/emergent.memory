package blueprints

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/superadmin"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// newAuthzHandler builds a Handler wired to a real superadmin repository over a
// fresh hermetic DB (CRUD/status paths under test only touch s.repo, so the
// nil cross-domain deps are safe). Returns the DB handle for seeding.
func newAuthzHandler(t *testing.T) (*Handler, bun.IDB) {
	t.Helper()
	db := connectTestDB(t)
	repo := NewRepository(db, testLogger())
	sa := superadmin.NewRepository(db)
	svc := NewService(ServiceParams{Repo: repo, Superadmin: sa, Log: testLogger()})
	return NewHandler(svc), db
}

// seedSuperadmin grants an active superadmin_full row for userID. The
// core.superadmins.user_id FK references core.user_profiles, so the profile is
// seeded first.
func seedSuperadmin(t *testing.T, ctx context.Context, db bun.IDB, userID string) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test Superadmin")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO core.superadmins (user_id, role) VALUES (?, 'superadmin_full')`, userID)
	require.NoError(t, err)
}

// newAuthzCtx builds an echo context with the authenticated user injected
// (what RequireAuth leaves behind for auth.MustGetUser) and an optional JSON body.
// The user is injected into BOTH the echo context (for auth.MustGetUser) and the
// request context.Context (for the service-layer shared helper, which reads via
// auth.RequireUser) — mirroring RequireAuth's InjectAuthContext.
func newAuthzCtx(t *testing.T, method, target string, body []byte, user *auth.AuthUser) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if user != nil {
		c.Set(string(auth.UserContextKey), user)
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
		c.SetRequest(req)
	}
	return c, rec
}

// assertForbidden asserts err is an *apperror.Error with HTTPStatus 403.
func assertForbidden(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var apErr *apperror.Error
	require.True(t, errors.As(err, &apErr), "expected *apperror.Error, got %T: %v", err, err)
	assert.Equal(t, http.StatusForbidden, apErr.HTTPStatus, "status = %d, want 403", apErr.HTTPStatus)
}

// createBody marshals a minimal create-blueprint request (global: no projectId).
func createBody(t *testing.T, name string) []byte {
	t.Helper()
	b, err := json.Marshal(CreateBlueprintRequest{
		Name:     name,
		Version:  "1.0.0",
		Manifest: json.RawMessage(`{"kind":"test"}`),
	})
	require.NoError(t, err)
	return b
}

// TestHandler_GlobalCatalogueWrites_RequireSuperadmin is the #1021 fail-first: a
// plain project member mutating the global catalogue (create/publish/deprecate/
// fork-without-project) must be denied 403, while an active superadmin_full
// succeeds. A project member's OWN private writes are untouched (no over-fix).
func TestHandler_GlobalCatalogueWrites_RequireSuperadmin(t *testing.T) {
	h, db := newAuthzHandler(t)
	ctx := context.Background()

	member := uuid.NewString()
	super := uuid.NewString()
	seedSuperadmin(t, ctx, db, super)

	// --- global create: member 403, superadmin 201 ---------------------------
	t.Run("global create denied for member", func(t *testing.T) {
		c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints", createBody(t, uniqueName("g-create")), &auth.AuthUser{ID: member})
		assertForbidden(t, h.CreateBlueprint(c))
	})

	t.Run("global create allowed for superadmin", func(t *testing.T) {
		c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints", createBody(t, uniqueName("g-create-sa")), &auth.AuthUser{ID: super})
		require.NoError(t, h.CreateBlueprint(c))
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	// --- project-private create stays allowed for a member -------------------
	t.Run("private create allowed for member", func(t *testing.T) {
		proj := uuid.NewString()
		c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints", createBody(t, uniqueName("p-create")), &auth.AuthUser{ID: member, ProjectID: proj})
		require.NoError(t, h.CreateBlueprint(c))
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	// --- global lifecycle transitions: member 403, superadmin OK -------------
	// Seed a global draft directly through the repo (the seed path is the
	// gateway/superadmin, not the handler), then prove the handler denies
	// member writes to it.
	seedGlobal := func(t *testing.T) *Blueprint {
		bp := &Blueprint{
			Name:      uniqueName("g-life"),
			Version:   "1.0.0",
			Status:    StatusDraft,
			Manifest:  json.RawMessage(`{"kind":"life"}`),
			ProjectID: nil, // global
		}
		require.NoError(t, h.svc.repo.Create(ctx, bp))
		return bp
	}

	t.Run("global publish denied for member", func(t *testing.T) {
		bp := seedGlobal(t)
		c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/"+bp.ID+"/publish", nil, &auth.AuthUser{ID: member})
		c.SetParamNames("id")
		c.SetParamValues(bp.ID)
		assertForbidden(t, h.PublishBlueprint(c))
	})

	t.Run("global deprecate denied for member", func(t *testing.T) {
		bp := seedGlobal(t)
		c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/"+bp.ID+"/deprecate", nil, &auth.AuthUser{ID: member})
		c.SetParamNames("id")
		c.SetParamValues(bp.ID)
		assertForbidden(t, h.DeprecateBlueprint(c))
	})

	t.Run("global fork (no project) denied for member", func(t *testing.T) {
		bp := seedGlobal(t)
		body, _ := json.Marshal(NewVersionRequest{Version: "2.0.0"})
		c, _ := newAuthzCtx(t, http.MethodPost, "/api/blueprints/"+bp.ID+"/versions", body, &auth.AuthUser{ID: member})
		c.SetParamNames("id")
		c.SetParamValues(bp.ID)
		assertForbidden(t, h.NewVersion(c))
	})

	t.Run("global publish allowed for superadmin", func(t *testing.T) {
		bp := seedGlobal(t)
		c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/"+bp.ID+"/publish", nil, &auth.AuthUser{ID: super})
		c.SetParamNames("id")
		c.SetParamValues(bp.ID)
		require.NoError(t, h.PublishBlueprint(c))
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("fork global into private stays allowed for member", func(t *testing.T) {
		bp := seedGlobal(t)
		proj := uuid.NewString()
		body, _ := json.Marshal(NewVersionRequest{Version: "2.1.0"})
		c, rec := newAuthzCtx(t, http.MethodPost, "/api/blueprints/"+bp.ID+"/versions", body, &auth.AuthUser{ID: member, ProjectID: proj})
		c.SetParamNames("id")
		c.SetParamValues(bp.ID)
		require.NoError(t, h.NewVersion(c))
		assert.Equal(t, http.StatusCreated, rec.Code)
	})
}
