package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

// newEchoCtx builds an echo context with an authenticated user.
func newEchoCtx(t *testing.T, method, target string, body []byte, user *auth.AuthUser) (echo.Context, *httptest.ResponseRecorder) {
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
	}
	return c, rec
}

func decodeResponse[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// seedSuperadminUser creates a core.user_profiles row (FK target of
// core.superadmins) and inserts a superadmin grant with the given role. A raw
// INSERT is used so the test controls the role directly — the repository helper
// (GrantSuperadminToUser) hardcodes superadmin_readonly.
func seedSuperadminUser(t *testing.T, db bun.IDB, userID, role string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO core.superadmins (user_id, role, granted_by) VALUES (?, ?, ?)`,
		userID, role, userID)
	require.NoError(t, err)
}

func TestHandler_ListProjectSkills_MergedAndShadowed(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	h := NewHandler(repo, slog.Default(), nil)
	ctx := context.Background()

	orgID, projectID := seedProject(t, db)
	shadowName := "deploy-" + uuid.NewString()

	// Global skills.
	globalDeploy := testSkill(shadowName, nil)
	globalOnly := testSkill("globalonly-"+uuid.NewString(), nil)
	require.NoError(t, repo.Create(ctx, globalDeploy))
	require.NoError(t, repo.Create(ctx, globalOnly))

	// Org-scoped skill (orgID only, distinct name — the schema's global unique
	// index covers all rows with project_id IS NULL, so org rows cannot share a
	// name with global rows).
	orgOnly := testSkill("orgonly-"+uuid.NewString(), nil)
	orgOnly.OrgID = &orgID
	require.NoError(t, repo.Create(ctx, orgOnly))

	// Project-scoped skills (projectID only). The project-scoped "deploy"
	// shadows the global one for this project (separate unique index).
	projectDeploy := testSkill(shadowName, &projectID)
	projectOnly := testSkill("projectonly-"+uuid.NewString(), &projectID)
	require.NoError(t, repo.Create(ctx, projectDeploy))
	require.NoError(t, repo.Create(ctx, projectOnly))

	user := &auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID}
	c, rec := newEchoCtx(t, http.MethodGet, "/api/projects/"+projectID+"/skills", nil, user)
	c.SetParamNames("projectId")
	c.SetParamValues(projectID)
	// Provide org context so org-scoped skills are included in the merge.
	c.SetRequest(c.Request().WithContext(auth.ContextWithOrgID(c.Request().Context(), orgID)))

	require.NoError(t, h.ListProjectSkills(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	resp := decodeResponse[ListSkillsResponse](t, rec)
	byName := make(map[string]*SkillDTO, len(resp.Data))
	for _, dto := range resp.Data {
		byName[dto.Name] = dto
	}

	// Union of project + org + global scopes. (The shared dev DB contains
	// pre-existing global skills, so we assert on the skills we created.)
	for _, want := range []string{shadowName, globalOnly.Name, orgOnly.Name, projectOnly.Name} {
		assert.Contains(t, byName, want, "merged project list must include %q", want)
	}

	// The shadowed name appears exactly once and the project-scoped version wins.
	deploy := byName[shadowName]
	require.NotNil(t, deploy)
	assert.Equal(t, projectID, *deploy.ProjectID, "project-scoped skill must shadow the global version")
}

func TestHandler_CreateGlobalSkill_SuperadminGated(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	saRepo := superadmin.NewRepository(db)
	h := NewHandler(repo, slog.Default(), saRepo)

	t.Run("non-superadmin rejected", func(t *testing.T) {
		user := &auth.AuthUser{ID: uuid.NewString()}
		body, err := json.Marshal(CreateSkillDTO{
			Name:        "global-" + uuid.NewString(),
			Description: "desc",
			Content:     "content",
		})
		require.NoError(t, err)

		c, _ := newEchoCtx(t, http.MethodPost, "/api/skills", body, user)
		err = h.CreateGlobalSkill(c)
		require.Error(t, err)
		var apErr *apperror.Error
		require.ErrorAs(t, err, &apErr)
		assert.Equal(t, http.StatusForbidden, apErr.HTTPStatus, "non-superadmin global create must be rejected with 403")
	})

	t.Run("superadmin_readonly rejected", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadminUser(t, db, userID, auth.RoleSuperadminReadonly)

		user := &auth.AuthUser{ID: userID}
		body, err := json.Marshal(CreateSkillDTO{
			Name:        "global-" + uuid.NewString(),
			Description: "desc",
			Content:     "content",
		})
		require.NoError(t, err)

		c, _ := newEchoCtx(t, http.MethodPost, "/api/skills", body, user)
		err = h.CreateGlobalSkill(c)
		require.Error(t, err)
		var apErr *apperror.Error
		require.ErrorAs(t, err, &apErr)
		assert.Equal(t, http.StatusForbidden, apErr.HTTPStatus, "superadmin_readonly global create must be rejected with 403")
	})

	t.Run("superadmin_full accepted", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadminUser(t, db, userID, auth.RoleSuperadminFull)

		user := &auth.AuthUser{ID: userID}
		body, err := json.Marshal(CreateSkillDTO{
			Name:        "global-" + uuid.NewString(),
			Description: "desc",
			Content:     "content",
		})
		require.NoError(t, err)

		c, rec := newEchoCtx(t, http.MethodPost, "/api/skills", body, user)
		require.NoError(t, h.CreateGlobalSkill(c))
		assert.Equal(t, http.StatusCreated, rec.Code)

		dto := decodeResponse[SkillDTO](t, rec)
		assert.Equal(t, "global", dto.Scope)
	})

	t.Run("no superadmin module configured denies create (fail closed)", func(t *testing.T) {
		ungated := NewHandler(repo, slog.Default(), nil)
		user := &auth.AuthUser{ID: uuid.NewString()}
		body, err := json.Marshal(CreateSkillDTO{
			Name:        "global-" + uuid.NewString(),
			Description: "desc",
			Content:     "content",
		})
		require.NoError(t, err)

		c, _ := newEchoCtx(t, http.MethodPost, "/api/skills", body, user)
		err = ungated.CreateGlobalSkill(c)
		require.Error(t, err)
		var apErr *apperror.Error
		require.ErrorAs(t, err, &apErr)
		assert.Equal(t, http.StatusForbidden, apErr.HTTPStatus, "nil superadmin module must fail closed (403), not fall back to authenticated create")
	})
}

func TestHandler_GlobalSkillUpdateDelete_SuperadminGated(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	saRepo := superadmin.NewRepository(db)
	h := NewHandler(repo, slog.Default(), saRepo)
	ctx := context.Background()

	newGlobalSkill := func(t *testing.T) *Skill {
		t.Helper()
		s := testSkill("global-"+uuid.NewString(), nil)
		require.NoError(t, repo.Create(ctx, s))
		return s
	}

	updateBody := func(t *testing.T) []byte {
		t.Helper()
		b, err := json.Marshal(map[string]any{"content": "updated content"})
		require.NoError(t, err)
		return b
	}

	assertForbidden := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		var apErr *apperror.Error
		require.ErrorAs(t, err, &apErr)
		assert.Equal(t, http.StatusForbidden, apErr.HTTPStatus)
	}

	t.Run("plain user update rejected", func(t *testing.T) {
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: uuid.NewString()}
		c, _ := newEchoCtx(t, http.MethodPatch, "/api/skills/"+s.ID.String(), updateBody(t), user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		assertForbidden(t, h.UpdateGlobalSkill(c))
	})

	t.Run("plain user delete rejected", func(t *testing.T) {
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: uuid.NewString()}
		c, _ := newEchoCtx(t, http.MethodDelete, "/api/skills/"+s.ID.String(), nil, user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		assertForbidden(t, h.DeleteGlobalSkill(c))
	})

	t.Run("superadmin_readonly update rejected", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadminUser(t, db, userID, auth.RoleSuperadminReadonly)
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: userID}
		c, _ := newEchoCtx(t, http.MethodPatch, "/api/skills/"+s.ID.String(), updateBody(t), user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		assertForbidden(t, h.UpdateGlobalSkill(c))
	})

	t.Run("superadmin_readonly delete rejected", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadminUser(t, db, userID, auth.RoleSuperadminReadonly)
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: userID}
		c, _ := newEchoCtx(t, http.MethodDelete, "/api/skills/"+s.ID.String(), nil, user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		assertForbidden(t, h.DeleteGlobalSkill(c))
	})

	t.Run("superadmin_full update succeeds", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadminUser(t, db, userID, auth.RoleSuperadminFull)
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: userID}
		c, rec := newEchoCtx(t, http.MethodPatch, "/api/skills/"+s.ID.String(), updateBody(t), user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		require.NoError(t, h.UpdateGlobalSkill(c))
		assert.Equal(t, http.StatusOK, rec.Code)
		dto := decodeResponse[SkillDTO](t, rec)
		assert.Equal(t, "updated content", dto.Content)
	})

	t.Run("superadmin_full delete succeeds", func(t *testing.T) {
		userID := uuid.NewString()
		seedSuperadminUser(t, db, userID, auth.RoleSuperadminFull)
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: userID}
		c, rec := newEchoCtx(t, http.MethodDelete, "/api/skills/"+s.ID.String(), nil, user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		require.NoError(t, h.DeleteGlobalSkill(c))
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("nil superadmin module update denied (fail closed)", func(t *testing.T) {
		ungated := NewHandler(repo, slog.Default(), nil)
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: uuid.NewString()}
		c, _ := newEchoCtx(t, http.MethodPatch, "/api/skills/"+s.ID.String(), updateBody(t), user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		assertForbidden(t, ungated.UpdateGlobalSkill(c))
	})

	t.Run("nil superadmin module delete denied (fail closed)", func(t *testing.T) {
		ungated := NewHandler(repo, slog.Default(), nil)
		s := newGlobalSkill(t)
		user := &auth.AuthUser{ID: uuid.NewString()}
		c, _ := newEchoCtx(t, http.MethodDelete, "/api/skills/"+s.ID.String(), nil, user)
		c.SetParamNames("id")
		c.SetParamValues(s.ID.String())
		assertForbidden(t, ungated.DeleteGlobalSkill(c))
	})
}

func TestHandler_UpdateSkill_PartialPatch(t *testing.T) {
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	h := NewHandler(repo, slog.Default(), nil)
	ctx := context.Background()

	_, pid := seedProject(t, db)
	s := testSkill("patch-"+uuid.NewString(), &pid)
	require.NoError(t, repo.Create(ctx, s))

	user := &auth.AuthUser{ID: uuid.NewString(), ProjectID: pid}
	body, err := json.Marshal(map[string]any{"content": "patched content only"})
	require.NoError(t, err)

	c, rec := newEchoCtx(t, http.MethodPatch, "/api/skills/"+s.ID.String(), body, user)
	c.SetParamNames("id")
	c.SetParamValues(s.ID.String())
	require.NoError(t, h.UpdateSkill(c))
	assert.Equal(t, http.StatusOK, rec.Code)

	dto := decodeResponse[SkillDTO](t, rec)
	assert.Equal(t, "patched content only", dto.Content)
	assert.Equal(t, s.Name, dto.Name, "name must be preserved on partial patch")
	assert.Equal(t, s.Description, dto.Description, "description must be preserved on partial patch")
	assert.Equal(t, computeContentHash("patched content only"), dto.Metadata.ContentHash, "content_hash must track the new content")
}
