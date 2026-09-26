package skills

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestHandler_GetSkill_GlobalReadableByAnyAuthenticatedUser asserts the global
// (platform catalogue) tier stays readable by any authenticated caller with no
// membership — mirroring ListGlobalSkills and the blueprints read posture.
func TestHandler_GetSkill_GlobalReadableByAnyAuthenticatedUser(t *testing.T) {
	h, _, _, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	global := testSkill("global-read-"+uuid.NewString(), nil)
	require.NoError(t, h.repo.Create(ctx, global))

	// A caller who belongs to no org at all (foreign to every tier).
	stranger := &auth.AuthUser{ID: uuid.NewString()}
	c, rec := newEchoCtx(t, http.MethodGet, "/api/skills/"+global.ID.String(), nil, stranger)
	withRequestUser(c, stranger)
	c.SetParamNames("id")
	c.SetParamValues(global.ID.String())

	require.NoError(t, h.GetSkill(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	dto := decodeResponse[SkillDTO](t, rec)
	assert.Equal(t, global.ID.String(), dto.ID)
	assert.Equal(t, "global", dto.Scope)
}

// TestHandler_GetSkill_OrgScopedReadableByMember asserts an org member can read
// their own org's skill, and a cross-org stranger gets 404 (existence leak
// avoidance, not 403).
func TestHandler_GetSkill_OrgScopedReadableByMember(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	orgSkill := testSkill("org-read-"+uuid.NewString(), nil)
	orgSkill.OrgID = &orgA
	require.NoError(t, h.repo.Create(ctx, orgSkill))

	member := &auth.AuthUser{ID: seedOrgMember(t, db, orgA)}
	c, rec := newEchoCtx(t, http.MethodGet, "/api/skills/"+orgSkill.ID.String(), nil, member)
	withRequestUser(c, member)
	c.SetParamNames("id")
	c.SetParamValues(orgSkill.ID.String())

	require.NoError(t, h.GetSkill(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	dto := decodeResponse[SkillDTO](t, rec)
	assert.Equal(t, "org", dto.Scope)
	assert.Equal(t, orgA, *dto.OrgID)
}

// TestHandler_GetSkill_OrgScopedCrossOrgNotFound asserts a caller with no
// authority over the addressed org-scoped skill is refused with 404 (so the
// response does not leak the skill's existence), not 403.
func TestHandler_GetSkill_OrgScopedCrossOrgNotFound(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	orgB := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	require.NoError(t, err)
	orgSkill := testSkill("org-cross-"+uuid.NewString(), nil)
	orgSkill.OrgID = &orgB
	require.NoError(t, h.repo.Create(ctx, orgSkill))

	attacker := &auth.AuthUser{ID: seedOrgMember(t, db, orgA)} // member of orgA only
	c, _ := newEchoCtx(t, http.MethodGet, "/api/skills/"+orgSkill.ID.String(), nil, attacker)
	withRequestUser(c, attacker)
	c.SetParamNames("id")
	c.SetParamValues(orgSkill.ID.String())
	assertStatus(t, h.GetSkill(c), http.StatusNotFound)
}

// TestHandler_GetSkill_ProjectScopedReadableByMember asserts a project member
// (org member) can read their project's skill.
func TestHandler_GetSkill_ProjectScopedReadableByMember(t *testing.T) {
	h, db, orgA, projectA := newOrgSkillHandler(t)
	ctx := context.Background()

	projSkill := testSkill("proj-read-"+uuid.NewString(), &projectA)
	require.NoError(t, h.repo.Create(ctx, projSkill))

	member := &auth.AuthUser{ID: seedOrgMember(t, db, orgA)}
	c, rec := newEchoCtx(t, http.MethodGet, "/api/skills/"+projSkill.ID.String(), nil, member)
	withRequestUser(c, member)
	c.SetParamNames("id")
	c.SetParamValues(projSkill.ID.String())

	require.NoError(t, h.GetSkill(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	dto := decodeResponse[SkillDTO](t, rec)
	assert.Equal(t, "project", dto.Scope)
	assert.Equal(t, projectA, *dto.ProjectID)
}

// TestHandler_GetSkill_ProjectScopedCrossProjectNotFound asserts a caller with
// no authority over the addressed project-scoped skill is refused with 404.
func TestHandler_GetSkill_ProjectScopedCrossProjectNotFound(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	orgB := uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgB, "org-b")
	require.NoError(t, err)
	projectB := uuid.NewString()
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectB, orgB, "proj-b")
	require.NoError(t, err)
	projSkill := testSkill("proj-cross-"+uuid.NewString(), &projectB)
	require.NoError(t, h.repo.Create(ctx, projSkill))

	attacker := &auth.AuthUser{ID: seedOrgMember(t, db, orgA)} // member of orgA only
	c, _ := newEchoCtx(t, http.MethodGet, "/api/skills/"+projSkill.ID.String(), nil, attacker)
	withRequestUser(c, attacker)
	c.SetParamNames("id")
	c.SetParamValues(projSkill.ID.String())
	assertStatus(t, h.GetSkill(c), http.StatusNotFound)
}

// TestHandler_GetSkill_UnknownIDNotFound asserts an unknown id yields 404 (the
// pre-existing FindByID contract), unaffected by the new read guard.
func TestHandler_GetSkill_UnknownIDNotFound(t *testing.T) {
	h, db, orgA, _ := newOrgSkillHandler(t)

	member := &auth.AuthUser{ID: seedOrgMember(t, db, orgA)}
	c, _ := newEchoCtx(t, http.MethodGet, "/api/skills/"+uuid.NewString(), nil, member)
	withRequestUser(c, member)
	c.SetParamNames("id")
	c.SetParamValues(uuid.NewString())
	assertStatus(t, h.GetSkill(c), http.StatusNotFound)
}

// TestHandler_GetSkill_NoUserUnauthorized asserts the read guard fails closed
// with 401 when no authenticated user is present (defense-in-depth; the route's
// RequireAuth guard normally guarantees one).
func TestHandler_GetSkill_NoUserUnauthorized(t *testing.T) {
	h, _, orgA, _ := newOrgSkillHandler(t)
	ctx := context.Background()

	orgSkill := testSkill("org-nouser-"+uuid.NewString(), nil)
	orgSkill.OrgID = &orgA
	require.NoError(t, h.repo.Create(ctx, orgSkill))

	c, _ := newEchoCtx(t, http.MethodGet, "/api/skills/"+orgSkill.ID.String(), nil, nil)
	c.SetParamNames("id")
	c.SetParamValues(orgSkill.ID.String())
	assertStatus(t, h.GetSkill(c), http.StatusUnauthorized)
}
