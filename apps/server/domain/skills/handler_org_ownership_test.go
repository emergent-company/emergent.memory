package skills

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// seedOrgMember creates a user profile and an org_admin membership for orgID,
// returning the user ID. The membership row is the source of authorization
// truth for the org/project skill guards.
func seedOrgMember(t *testing.T, db bun.IDB, orgID string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`,
		orgID, userID)
	require.NoError(t, err)
	return userID
}

// withRequestUser embeds the authenticated user in the request context (the
// source auth.RequireUser reads), mirroring InjectAuthContext.
func withRequestUser(c echo.Context, user *auth.AuthUser) {
	c.SetRequest(c.Request().WithContext(auth.ContextWithUser(c.Request().Context(), user)))
}

// newOrgSkillHandler builds a Handler over a fresh DB and seeds orgA+projectA,
// returning the handler, DB handle, org ID and project ID.
func newOrgSkillHandler(t *testing.T) (*Handler, bun.IDB, string, string) {
	t.Helper()
	db := connectTestDB(t)
	repo := NewRepository(db, slog.Default())
	h := NewHandler(repo, slog.Default(), nil)
	orgID, projectID := seedProject(t, db)
	return h, db, orgID, projectID
}

func orgCreateBody(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(CreateSkillDTO{Name: "orgskill-" + uuid.NewString(), Description: "d", Content: "c"})
	require.NoError(t, err)
	return b
}

func orgUpdateBody(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"content": "updated"})
	require.NoError(t, err)
	return b
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	require.Error(t, err)
	var apErr *apperror.Error
	require.ErrorAs(t, err, &apErr)
	assert.Equal(t, want, apErr.HTTPStatus)
}

// TestHandler_OrgSkill_NoUserUnauthorized: with no authenticated user the org
// skill routes fail closed with 401.
func TestHandler_OrgSkill_NoUserUnauthorized(t *testing.T) {
	h, _, orgA, _ := newOrgSkillHandler(t)

	c, _ := newEchoCtx(t, http.MethodPost, "/api/orgs/"+orgA+"/skills", orgCreateBody(t), nil)
	c.SetParamNames("orgId")
	c.SetParamValues(orgA)
	assertStatus(t, h.CreateOrgSkill(c), http.StatusUnauthorized)
}

// TestHandler_OrgSkill_NonMemberForbidden: a member of org A cannot create,
// list, update or delete skills in org B (cross-tenant IDOR, issue #849).
func TestHandler_OrgSkill_NonMemberForbidden(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	// A second org the caller does not belong to, plus a skill in it.
	orgB := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	require.NoError(t, err)
	skillB := testSkill("skill-b-"+uuid.NewString(), nil)
	skillB.OrgID = &orgB
	require.NoError(t, h.repo.Create(ctx, skillB))

	attacker := seedOrgMember(t, db, orgA) // member of orgA only

	t.Run("create in foreign org forbidden", func(t *testing.T) {
		c, _ := newEchoCtx(t, http.MethodPost, "/api/orgs/"+orgB+"/skills", orgCreateBody(t), &auth.AuthUser{ID: attacker})
		withRequestUser(c, &auth.AuthUser{ID: attacker})
		c.SetParamNames("orgId")
		c.SetParamValues(orgB)
		assertStatus(t, h.CreateOrgSkill(c), http.StatusForbidden)
	})

	t.Run("list foreign org forbidden", func(t *testing.T) {
		c, _ := newEchoCtx(t, http.MethodGet, "/api/orgs/"+orgB+"/skills", nil, &auth.AuthUser{ID: attacker})
		withRequestUser(c, &auth.AuthUser{ID: attacker})
		c.SetParamNames("orgId")
		c.SetParamValues(orgB)
		assertStatus(t, h.ListOrgSkills(c), http.StatusForbidden)
	})

	t.Run("update foreign org skill forbidden", func(t *testing.T) {
		c, _ := newEchoCtx(t, http.MethodPatch, "/api/orgs/"+orgB+"/skills/"+skillB.ID.String(), orgUpdateBody(t), &auth.AuthUser{ID: attacker})
		withRequestUser(c, &auth.AuthUser{ID: attacker})
		c.SetParamNames("orgId", "id")
		c.SetParamValues(orgB, skillB.ID.String())
		assertStatus(t, h.UpdateOrgSkill(c), http.StatusForbidden)
	})

	t.Run("delete foreign org skill forbidden", func(t *testing.T) {
		c, _ := newEchoCtx(t, http.MethodDelete, "/api/orgs/"+orgB+"/skills/"+skillB.ID.String(), nil, &auth.AuthUser{ID: attacker})
		withRequestUser(c, &auth.AuthUser{ID: attacker})
		c.SetParamNames("orgId", "id")
		c.SetParamValues(orgB, skillB.ID.String())
		assertStatus(t, h.DeleteOrgSkill(c), http.StatusForbidden)
	})
}

// TestHandler_OrgSkill_MemberCreateUpdateDelete: a member of org A can create,
// update and delete skills in their own org (own-resource success).
func TestHandler_OrgSkill_MemberCreateUpdateDelete(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)

	member := seedOrgMember(t, db, orgA)

	// Create.
	c, rec := newEchoCtx(t, http.MethodPost, "/api/orgs/"+orgA+"/skills", orgCreateBody(t), &auth.AuthUser{ID: member})
	withRequestUser(c, &auth.AuthUser{ID: member})
	c.SetParamNames("orgId")
	c.SetParamValues(orgA)
	require.NoError(t, h.CreateOrgSkill(c))
	assert.Equal(t, http.StatusCreated, rec.Code)
	created := decodeResponse[SkillDTO](t, rec)
	assert.Equal(t, orgA, *created.OrgID)

	// Update.
	u, urec := newEchoCtx(t, http.MethodPatch, "/api/orgs/"+orgA+"/skills/"+created.ID, orgUpdateBody(t), &auth.AuthUser{ID: member})
	withRequestUser(u, &auth.AuthUser{ID: member})
	u.SetParamNames("orgId", "id")
	u.SetParamValues(orgA, created.ID)
	require.NoError(t, h.UpdateOrgSkill(u))
	assert.Equal(t, http.StatusOK, urec.Code)
	assert.Equal(t, "updated", decodeResponse[SkillDTO](t, urec).Content)

	// Delete.
	d, drec := newEchoCtx(t, http.MethodDelete, "/api/orgs/"+orgA+"/skills/"+created.ID, nil, &auth.AuthUser{ID: member})
	withRequestUser(d, &auth.AuthUser{ID: member})
	d.SetParamNames("orgId", "id")
	d.SetParamValues(orgA, created.ID)
	require.NoError(t, h.DeleteOrgSkill(d))
	assert.Equal(t, http.StatusNoContent, drec.Code)
}

// TestHandler_OrgSkill_CrossOrgSkillNotFound: a member of org A addressing a
// skill that belongs to org B through the org A route gets 404 (not 403), so the
// response does not leak the skill's existence.
func TestHandler_OrgSkill_CrossOrgSkillNotFound(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	orgB := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	require.NoError(t, err)
	skillB := testSkill("skill-b-"+uuid.NewString(), nil)
	skillB.OrgID = &orgB
	require.NoError(t, h.repo.Create(ctx, skillB))

	member := seedOrgMember(t, db, orgA)

	// Address org B's skill through the org A route.
	c, _ := newEchoCtx(t, http.MethodPatch, "/api/orgs/"+orgA+"/skills/"+skillB.ID.String(), orgUpdateBody(t), &auth.AuthUser{ID: member})
	withRequestUser(c, &auth.AuthUser{ID: member})
	c.SetParamNames("orgId", "id")
	c.SetParamValues(orgA, skillB.ID.String())
	assertStatus(t, h.UpdateOrgSkill(c), http.StatusNotFound)
}

// TestHandler_ProjectSkill_NonMemberForbidden: a member of org A cannot create a
// skill in a project owned by org B (project-scoped skill routes, issue #849).
func TestHandler_ProjectSkill_NonMemberForbidden(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	orgB := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	require.NoError(t, err)
	projectB := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectB, orgB, "proj-b")
	require.NoError(t, err)

	attacker := seedOrgMember(t, db, orgA)

	c, _ := newEchoCtx(t, http.MethodPost, "/api/projects/"+projectB+"/skills", orgCreateBody(t), &auth.AuthUser{ID: attacker})
	withRequestUser(c, &auth.AuthUser{ID: attacker})
	c.SetParamNames("projectId")
	c.SetParamValues(projectB)
	assertStatus(t, h.CreateProjectSkill(c), http.StatusForbidden)
}

// TestHandler_ProjectSkill_CrossProjectSkillNotFound: a member of org A
// addressing a skill in a foreign project through their own project route gets
// 404 (no existence oracle).
func TestHandler_ProjectSkill_CrossProjectSkillNotFound(t *testing.T) {
	h, db, orgA, projectA := newOrgSkillHandler(t)
	ctx := context.Background()

	member := seedOrgMember(t, db, orgA)

	orgB := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	require.NoError(t, err)
	projectB := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectB, orgB, "proj-b")
	require.NoError(t, err)
	skillB := testSkill("pskill-b-"+uuid.NewString(), &projectB)
	require.NoError(t, h.repo.Create(ctx, skillB))

	c, _ := newEchoCtx(t, http.MethodPatch, "/api/projects/"+projectA+"/skills/"+skillB.ID.String(), orgUpdateBody(t), &auth.AuthUser{ID: member})
	withRequestUser(c, &auth.AuthUser{ID: member})
	c.SetParamNames("projectId", "id")
	c.SetParamValues(projectA, skillB.ID.String())
	assertStatus(t, h.UpdateProjectSkill(c), http.StatusNotFound)
}
