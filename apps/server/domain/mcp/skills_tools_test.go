package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// connectTestDB opens a throwaway test database owned by this test, so each
// test runs against a uniquely-named database it drops on cleanup. Skips when
// unavailable or in short mode; fails when REQUIRE_DB is set.
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "Skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "mcp_skills")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// fakeEmbedder implements skills.Embedder for tests.
type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

func testVector() []float32 {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i%5) / 10
	}
	return v
}

func uniqueName(prefix string) string {
	return prefix + "-" + uuid.NewString()
}

// seedProject creates a real org + project row (kb.skills has FK constraints on
// project_id and org_id) and returns their IDs.
func seedProject(t *testing.T, db bun.IDB) (orgID, projectID string) {
	t.Helper()
	ctx := context.Background()
	orgID = uuid.NewString()
	projectID = uuid.NewString()
	_, err := db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "test-org-"+orgID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "test-project-"+projectID)
	require.NoError(t, err)
	return orgID, projectID
}

// toolResultText extracts the JSON text from a ToolResult.
func toolResultText(t *testing.T, res *ToolResult, err error) string {
	t.Helper()
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Content, 1)
	return res.Content[0].Text
}

// seedMember creates a user profile plus an org membership for orgID and
// returns the user id, so a caller can be authorized against a project's
// owning org (mirrors the skills domain's seedOrgMember).
func seedMember(t *testing.T, db bun.IDB, orgID string) string {
	t.Helper()
	ctx := context.Background()
	userID := uuid.NewString()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at) VALUES (?, ?, 'org_admin', NOW())`,
		orgID, userID)
	require.NoError(t, err)
	return userID
}

// memberCtx returns a context carrying the given caller as the authenticated
// principal (the source AuthorizeSkillAccess/AuthorizeSkillWrite read via
// auth.RequireUser).
func memberCtx(userID string) context.Context {
	return auth.ContextWithUser(context.Background(), &auth.AuthUser{ID: userID})
}

func TestExecuteCreateSkill_ProjectScopedWithEmbedding(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()

	t.Run("populates description embedding", func(t *testing.T) {
		repo := skills.NewRepository(db, slog.Default(), skills.WithEmbedder(&fakeEmbedder{vec: testVector()}))
		svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}

		_, projectID := seedProject(t, db)
		res, err := svc.executeCreateSkill(ctx, projectID, map[string]any{
			"name":        uniqueName("agent-skill"),
			"description": "A skill created via MCP",
			"content":     "# Steps\n1. Do the thing",
		})
		text := toolResultText(t, res, err)

		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.Equal(t, "project", dto["scope"])
		assert.NotEmpty(t, dto["id"])

		// The stored skill must have a non-null description_embedding.
		id, perr := uuid.Parse(dto["id"].(string))
		require.NoError(t, perr)
		stored, ferr := repo.FindByID(ctx, id)
		require.NoError(t, ferr)
		assert.NotEmpty(t, stored.DescriptionEmbedding, "MCP create must produce a non-null description_embedding")
	})

	t.Run("embedding failure non-fatal with null embedding", func(t *testing.T) {
		repo := skills.NewRepository(db, slog.Default(), skills.WithEmbedder(&fakeEmbedder{err: errors.New("embeddings down")}))
		svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}

		_, projectID := seedProject(t, db)
		res, err := svc.executeCreateSkill(ctx, projectID, map[string]any{
			"name":        uniqueName("agent-skill"),
			"description": "skill created despite embedding failure",
			"content":     "# Still here",
		})
		text := toolResultText(t, res, err)

		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.NotEmpty(t, dto["id"], "skill must still be created when embedding fails")

		id, perr := uuid.Parse(dto["id"].(string))
		require.NoError(t, perr)
		stored, ferr := repo.FindByID(ctx, id)
		require.NoError(t, ferr)
		assert.Empty(t, stored.DescriptionEmbedding, "embedding must be null when generation fails")
	})

	t.Run("invalid name rejected via shared validation", func(t *testing.T) {
		repo := skills.NewRepository(db, slog.Default())
		svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}

		_, err := svc.executeCreateSkill(ctx, uuid.NewString(), map[string]any{
			"name":        "Invalid Name!",
			"description": "desc",
			"content":     "content",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create_skill")
	})

	t.Run("empty content rejected", func(t *testing.T) {
		repo := skills.NewRepository(db, slog.Default())
		svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}

		_, err := svc.executeCreateSkill(ctx, uuid.NewString(), map[string]any{
			"name":        uniqueName("agent-skill"),
			"description": "desc",
			"content":     "",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "content")
	})
}

func TestExecuteGetSkill_NameResolution(t *testing.T) {
	db := connectTestDB(t)
	repo := skills.NewRepository(db, slog.Default())
	svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}
	ctx := context.Background()

	orgID, projectID := seedProject(t, db)
	name := uniqueName("shared-skill")

	// Global skill with the name.
	global := &skills.Skill{Name: name, Description: "global version", Content: "global content"}
	require.NoError(t, repo.Create(ctx, global))

	// Project-scoped skill with the same name (should win).
	project := &skills.Skill{Name: name, Description: "project version", Content: "project content", ProjectID: &projectID}
	require.NoError(t, repo.Create(ctx, project))

	memberID := seedMember(t, db, orgID)

	t.Run("resolves project-scoped skill by name", func(t *testing.T) {
		res, err := svc.executeGetSkill(ctx, projectID, map[string]any{"skill_id": name})
		text := toolResultText(t, res, err)
		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.Equal(t, "project", dto["scope"])
		assert.Equal(t, "project content", dto["content"])
	})

	t.Run("falls back to global when no project-scoped match", func(t *testing.T) {
		otherProject := uuid.NewString()
		res, err := svc.executeGetSkill(ctx, otherProject, map[string]any{"skill_id": name})
		text := toolResultText(t, res, err)
		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.Equal(t, "global", dto["scope"])
		assert.Equal(t, "global content", dto["content"])
	})

	t.Run("resolves by UUID", func(t *testing.T) {
		// UUID path is authorization-gated; a member of the owning org succeeds.
		res, err := svc.executeGetSkill(memberCtx(memberID), projectID, map[string]any{"skill_id": project.ID.String()})
		text := toolResultText(t, res, err)
		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.Equal(t, project.ID.String(), dto["id"])
	})

	t.Run("unknown name errors", func(t *testing.T) {
		_, err := svc.executeGetSkill(ctx, projectID, map[string]any{"skill_id": "does-not-exist"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestSkillsToolScopeFiltering(t *testing.T) {
	defs := skillsToolDefinitions()
	require.Len(t, defs, 5)

	t.Run("skills:write implies skills:read", func(t *testing.T) {
		expanded := expandScopesSet([]string{"skills:write"})
		assert.True(t, expanded["skills:read"], "skills:write must imply skills:read")
		assert.True(t, expanded["skills:write"])
	})

	t.Run("read scope exposes only read tools", func(t *testing.T) {
		filtered := FilterToolsForScopes(defs, []string{"skills:read"})
		names := toolNames(filtered)
		assert.ElementsMatch(t, []string{"skill-list", "skill-get"}, names)
	})

	t.Run("write scope exposes all skill tools", func(t *testing.T) {
		filtered := FilterToolsForScopes(defs, []string{"skills:write"})
		names := toolNames(filtered)
		assert.ElementsMatch(t, []string{"skill-list", "skill-get", "skill-create", "skill-update", "skill-delete"}, names)
	})

	t.Run("no scope exposes nothing", func(t *testing.T) {
		filtered := FilterToolsForScopes(defs, nil)
		assert.Empty(t, filtered)
	})
}

func toolNames(defs []ToolDefinition) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}

func TestExecuteUpdateSkill_SharedValidation(t *testing.T) {
	db := connectTestDB(t)
	repo := skills.NewRepository(db, slog.Default())
	svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}
	ctx := context.Background()

	orgID, pid := seedProject(t, db)
	sk := &skills.Skill{Name: uniqueName("update-me"), Description: "desc", Content: "content", ProjectID: &pid}
	require.NoError(t, repo.Create(ctx, sk))

	// The update UUID path is authorization-gated; a member of the owning org
	// reaches the shared validation / update logic.
	authCtx := memberCtx(seedMember(t, db, orgID))

	t.Run("empty description rejected", func(t *testing.T) {
		_, err := svc.executeUpdateSkill(authCtx, map[string]any{
			"skill_id":    sk.ID.String(),
			"description": "",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "description")
	})

	t.Run("valid update succeeds", func(t *testing.T) {
		res, err := svc.executeUpdateSkill(authCtx, map[string]any{
			"skill_id":    sk.ID.String(),
			"description": "updated via MCP",
			"content":     "updated content via MCP",
		})
		text := toolResultText(t, res, err)
		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.Equal(t, "updated via MCP", dto["description"])
	})
}

// TestExecuteSkillTools_CrossProjectScoped is the fail-first for issue #1041's
// MCP leg: the UUID-keyed get/update/delete tools must match the REST tier
// resolution (shared helper), so a caller in project A cannot read or mutate a
// project B skill by UUID, and global writes are refused (no superadmin on the
// project-scoped MCP surface). The legitimate paths (own project, global read)
// still work.
func TestExecuteSkillTools_CrossProjectScoped(t *testing.T) {
	db := connectTestDB(t)
	repo := skills.NewRepository(db, slog.Default())
	svc := &Service{skillsRepo: repo, db: db, log: slog.Default()}
	ctx := context.Background()

	orgA, projectA := seedProject(t, db)
	_, projectB := seedProject(t, db)

	skillA := &skills.Skill{Name: uniqueName("proj-a"), Description: "A", Content: "content A", ProjectID: &projectA}
	require.NoError(t, repo.Create(ctx, skillA))

	skillB := &skills.Skill{Name: uniqueName("proj-b"), Description: "B", Content: "TOP-SECRET-B", ProjectID: &projectB}
	require.NoError(t, repo.Create(ctx, skillB))

	global := &skills.Skill{Name: uniqueName("global"), Description: "G", Content: "global content"}
	require.NoError(t, repo.Create(ctx, global))

	attacker := memberCtx(seedMember(t, db, orgA)) // member of org A only

	t.Run("cross-project get by UUID refused (404)", func(t *testing.T) {
		_, err := svc.executeGetSkill(attacker, projectA, map[string]any{"skill_id": skillB.ID.String()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("cross-project update by UUID refused (404)", func(t *testing.T) {
		_, err := svc.executeUpdateSkill(attacker, map[string]any{
			"skill_id": skillB.ID.String(),
			"content":  "pwned",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("cross-project delete by UUID refused (404)", func(t *testing.T) {
		_, err := svc.executeDeleteSkill(attacker, map[string]any{"skill_id": skillB.ID.String()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("global update refused (no superadmin on MCP)", func(t *testing.T) {
		_, err := svc.executeUpdateSkill(attacker, map[string]any{
			"skill_id": global.ID.String(),
			"content":  "pwned global",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forbidden")
	})

	t.Run("global delete refused (no superadmin on MCP)", func(t *testing.T) {
		_, err := svc.executeDeleteSkill(attacker, map[string]any{"skill_id": global.ID.String()})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forbidden")
	})

	t.Run("own-project delete succeeds", func(t *testing.T) {
		_, err := svc.executeDeleteSkill(attacker, map[string]any{"skill_id": skillA.ID.String()})
		require.NoError(t, err)
		_, ferr := repo.FindByID(ctx, skillA.ID)
		require.Error(t, ferr, "own-project skill must actually be deleted")
	})

	t.Run("global read by any authenticated caller succeeds", func(t *testing.T) {
		stranger := memberCtx(uuid.NewString()) // belongs to no org
		res, err := svc.executeGetSkill(stranger, projectA, map[string]any{"skill_id": global.ID.String()})
		text := toolResultText(t, res, err)
		var dto map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &dto))
		assert.Equal(t, "global", dto["scope"])
	})
}
