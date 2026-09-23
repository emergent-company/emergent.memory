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
	ctx := context.Background()

	// seedSuperadminUser creates a core.user_profiles row (FK target of
	// core.superadmins) and inserts a superadmin grant with the given role. A
	// raw INSERT is used so the test controls the role directly — the repository
	// helper (GrantSuperadminToUser) hardcodes superadmin_readonly.
	seedSuperadminUser := func(t *testing.T, userID, role string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
			userID, "zitadel-"+userID, "Test User")
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO core.superadmins (user_id, role, granted_by) VALUES (?, ?, ?)`,
			userID, role, userID)
		require.NoError(t, err)
	}

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
		seedSuperadminUser(t, userID, auth.RoleSuperadminReadonly)

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
		seedSuperadminUser(t, userID, auth.RoleSuperadminFull)

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

	t.Run("no superadmin module configured falls back to authenticated create", func(t *testing.T) {
		ungated := NewHandler(repo, slog.Default(), nil)
		user := &auth.AuthUser{ID: uuid.NewString()}
		body, err := json.Marshal(CreateSkillDTO{
			Name:        "global-" + uuid.NewString(),
			Description: "desc",
			Content:     "content",
		})
		require.NoError(t, err)

		c, rec := newEchoCtx(t, http.MethodPost, "/api/skills", body, user)
		require.NoError(t, ungated.CreateGlobalSkill(c))
		assert.Equal(t, http.StatusCreated, rec.Code)
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
