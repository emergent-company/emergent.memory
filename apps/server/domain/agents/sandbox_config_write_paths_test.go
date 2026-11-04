package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// These tests pin the write-path secret handling for every endpoint that accepts
// a raw sandbox-config map (issue #1844 review): CreateDefinition, UpdateDefinition,
// and SetAgentOverride. Each proves (a) a client-supplied plaintext git token is
// rejected and never persisted, and (b) the redaction-placeholder round-trip
// preserves a previously stored secret.

const dummyGitToken = "ghp_dummy_token_not_real"

func newAuthedContext(method, path, body string, projectID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})
	return c, rec
}

func requireBadRequest(t *testing.T, err error, substr string) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr, "expected *apperror.Error, got %T: %v", err, err)
	require.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	require.Contains(t, appErr.Message, substr)
}

// --- CreateDefinition (POST /api/projects/:projectId/agent-definitions) ---

func TestCreateDefinition_RejectsPlaintextGitToken(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_create_def_reject_git_token")
	t.Cleanup(tdb.Close)
	ctx := context.Background()
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	for _, body := range []string{
		`{"name":"secret-agent","workspaceConfig":{"enabled":true,"git_token":"` + dummyGitToken + `"}}`,
		`{"name":"secret-agent","workspaceConfig":{"enabled":true,"env_vars":{"GH_TOKEN":"` + dummyGitToken + `"}}}`,
		`{"name":"secret-agent","workspaceConfig":{"enabled":true,"env_vars":{"GITHUB_TOKEN":"` + dummyGitToken + `"}}}`,
	} {
		c, _ := newAuthedContext(http.MethodPost, "/api/projects/"+projectID+"/agent-definitions", body, projectID)
		c.SetParamNames("projectId")
		c.SetParamValues(projectID)

		err := h.CreateDefinition(c)
		requireBadRequest(t, err, "git_token_secret_ref")
	}

	// Nothing was persisted.
	def, err := repo.FindDefinitionByName(ctx, projectID, "secret-agent")
	require.NoError(t, err)
	require.Nil(t, def, "a rejected create must not persist the definition")
}

// --- UpdateDefinition (PATCH /api/projects/:projectId/agent-definitions/:id) ---

func TestUpdateDefinition_RejectsPlaintextGitToken(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_update_def_reject_git_token")
	t.Cleanup(tdb.Close)
	ctx := context.Background()
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "secret-agent", string(VisibilityProject))

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	body := `{"workspaceConfig":{"enabled":true,"env_vars":{"GH_TOKEN":"` + dummyGitToken + `"}}}`
	c, _ := newAuthedContext(http.MethodPatch, "/api/projects/"+projectID+"/agent-definitions/"+defID, body, projectID)
	c.SetParamNames("id")
	c.SetParamValues(defID)

	err := h.UpdateDefinition(c)
	requireBadRequest(t, err, "git_token_secret_ref")

	// The stored definition is untouched.
	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)
	require.Nil(t, def.SandboxConfig, "a rejected update must not persist any sandbox config")
}

func TestUpdateDefinition_PlaceholderRoundTripPreservesStoredSecret(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_update_def_placeholder_roundtrip")
	t.Cleanup(tdb.Close)
	ctx := context.Background()
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	defID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility, sandbox_config)
		 VALUES (?, ?, ?, ?, ?::jsonb)`,
		defID, projectID, "secret-agent", string(VisibilityProject),
		`{"enabled":true,"env_vars":{"GH_TOKEN":"`+dummyGitToken+`"}}`)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	body := `{"workspaceConfig":{"enabled":true,"env_vars":{"GH_TOKEN":"` + sandbox.RedactedSecretValue + `"}}}`
	c, _ := newAuthedContext(http.MethodPatch, "/api/projects/"+projectID+"/agent-definitions/"+defID, body, projectID)
	c.SetParamNames("id")
	c.SetParamValues(defID)

	require.NoError(t, h.UpdateDefinition(c))

	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)
	envVars, ok := def.SandboxConfig["env_vars"].(map[string]any)
	require.True(t, ok, "env_vars must be preserved in stored sandbox_config")
	require.Equal(t, dummyGitToken, envVars["GH_TOKEN"],
		"placeholder round-trip must restore (not erase) the stored secret")
}

// --- SetAgentOverride (PUT /api/projects/:projectId/agent-definitions/overrides/:agentName) ---

func TestSetAgentOverride_RejectsPlaintextGitToken(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_override_reject_git_token")
	t.Cleanup(tdb.Close)
	ctx := context.Background()
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	for _, body := range []string{
		`{"sandboxConfig":{"enabled":true,"git_token":"` + dummyGitToken + `"}}`,
		`{"sandboxConfig":{"enabled":true,"env_vars":{"GH_TOKEN":"` + dummyGitToken + `"}}}`,
	} {
		c, _ := newAuthedContext(http.MethodPut, "/api/projects/"+projectID+"/agent-definitions/overrides/secret-agent", body, projectID)
		c.SetParamNames("projectId", "agentName")
		c.SetParamValues(projectID, "secret-agent")

		err := h.SetAgentOverride(c)
		requireBadRequest(t, err, "git_token_secret_ref")
	}

	// Nothing was persisted.
	override, err := repo.GetAgentOverride(ctx, projectID, "secret-agent")
	require.NoError(t, err)
	require.Nil(t, override, "a rejected override must not be persisted")
}

func TestSetAgentOverride_PlaceholderRoundTripPreservesStoredSecret(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_override_placeholder_roundtrip")
	t.Cleanup(tdb.Close)
	ctx := context.Background()
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)

	// Seed an override that already stores a real git token.
	_, err := repo.SetAgentOverride(ctx, projectID, "secret-agent", &AgentOverride{
		SandboxConfig: map[string]any{
			"enabled":  true,
			"env_vars": map[string]any{"GH_TOKEN": dummyGitToken},
		},
	})
	require.NoError(t, err)

	h := &Handler{repo: repo}

	body := `{"sandboxConfig":{"enabled":true,"env_vars":{"GH_TOKEN":"` + sandbox.RedactedSecretValue + `"}}}`
	c, _ := newAuthedContext(http.MethodPut, "/api/projects/"+projectID+"/agent-definitions/overrides/secret-agent", body, projectID)
	c.SetParamNames("projectId", "agentName")
	c.SetParamValues(projectID, "secret-agent")

	require.NoError(t, h.SetAgentOverride(c))

	override, err := repo.GetAgentOverride(ctx, projectID, "secret-agent")
	require.NoError(t, err)
	require.NotNil(t, override)
	envVars, ok := override.SandboxConfig["env_vars"].(map[string]any)
	require.True(t, ok, "env_vars must be preserved in stored override sandboxConfig")
	require.Equal(t, dummyGitToken, envVars["GH_TOKEN"],
		"placeholder round-trip must restore (not erase) the stored override secret")
}

// --- SetProjectSetting (PUT /api/projects/:projectId/settings/:category/:key) ---

// TestSetProjectSetting_RejectsPlaintextGitToken proves the generic settings
// endpoint refuses to write the agent_override category at all — the one category
// whose stored JSONB value GetAgentOverride/ApplyAgentOverride later turns into a
// sandbox config. Without this guard, a caller with agents:write could POST a
// sandboxConfig with a plaintext git token straight into kb.project_settings.value
// and bypass the normalization on SetAgentOverride (issue #1844 review).
func TestSetProjectSetting_RejectsPlaintextGitToken(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_generic_setting_reject_git_token")
	t.Cleanup(tdb.Close)
	ctx := context.Background()
	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	for _, body := range []string{
		`{"sandboxConfig":{"enabled":true,"env_vars":{"GH_TOKEN":"` + dummyGitToken + `"}}}`,
		`{"sandboxConfig":{"enabled":true,"git_token":"` + dummyGitToken + `"}}`,
		`{"any":"payload"}`,
	} {
		c, _ := newAuthedContext(http.MethodPut, "/api/projects/"+projectID+"/settings/agent_override/secret-agent", body, projectID)
		c.SetParamNames("projectId", "category", "key")
		c.SetParamValues(projectID, SettingsCategoryAgentOverride, "secret-agent")

		// The whole agent_override category is rejected (not just the token), so the
		// assertion is a 400 and no persisted row, regardless of the payload shape.
		requireBadRequest(t, h.SetProjectSetting(c), "dedicated endpoint")
	}

	// Nothing was persisted through the generic endpoint.
	override, err := repo.GetAgentOverride(ctx, projectID, "secret-agent")
	require.NoError(t, err)
	require.Nil(t, override, "a rejected generic agent_override write must not be persisted")
}
