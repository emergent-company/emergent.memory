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
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestUpdateSandboxConfigRedactsRestoredSecret drives h.UpdateSandboxConfig against
// a real repository. The stored definition carries a real GH_TOKEN in
// sandbox_config.env_vars. The PUT body sends back the redaction placeholder for
// that key, which triggers the preserve-block (restoring the stored secret into
// the in-memory config so storage keeps the credential). The 200 response must
// redact the restored secret — i.e. the body must never contain the real token
// value — otherwise a caller with agents:write could GET the placeholder, PUT it
// straight back, and read the real token out of the response (issue #1844).
func TestUpdateSandboxConfigRedactsRestoredSecret(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_sandbox_put_redact")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	const dummyToken = "ghp_dummy_token_not_real"
	defID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agent_definitions (id, project_id, name, visibility, sandbox_config)
		 VALUES (?, ?, ?, ?, ?::jsonb)`,
		defID, projectID, "secret-agent", string(VisibilityProject),
		`{"enabled":true,"env_vars":{"GH_TOKEN":"`+dummyToken+`"}}`)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	body := `{"enabled":true,"env_vars":{"GH_TOKEN":"` + sandbox.RedactedSecretValue + `"}}`
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/agent-definitions/"+defID+"/sandbox-config", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(defID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})

	require.NoError(t, h.UpdateSandboxConfig(c))

	bodyOut := rec.Body.String()
	require.NotContains(t, bodyOut, dummyToken,
		"PUT response must never echo the real stored secret back")
	require.Contains(t, bodyOut, sandbox.RedactedSecretValue,
		"PUT response must carry the redaction placeholder for the preserved secret")

	// Storage must still hold the real credential: the preserve-block kept it and
	// redaction only affects the response, never the persisted config.
	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)
	envVars, ok := def.SandboxConfig["env_vars"].(map[string]any)
	require.True(t, ok, "env_vars must be preserved in stored sandbox_config")
	require.Equal(t, dummyToken, envVars["GH_TOKEN"],
		"storage must preserve the real secret, not the placeholder")
}

// TestGetProjectSettingRedactsAgentOverrideSandboxSecrets proves the generic
// settings read path cannot bypass the override redaction. SetAgentOverride
// persists the override (including sandboxConfig.env_vars) through the generic
// UpsertProjectSetting store, so GET /settings/agent_override/<name> — reachable
// under the same agents:read privilege — would otherwise return the raw value
// map. The response must never contain the real token (issue #1844).
func TestGetProjectSettingRedactsAgentOverrideSandboxSecrets(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_settings_get_redact")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	const dummyToken = "ghp_dummy_token_not_real"
	repo := NewRepository(tdb.DB)

	_, err := repo.SetAgentOverride(ctx, projectID, "secret-agent", &AgentOverride{
		SandboxConfig: map[string]any{
			"enabled":  true,
			"env_vars": map[string]any{"GH_TOKEN": dummyToken},
		},
	})
	require.NoError(t, err)

	h := &Handler{repo: repo}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/settings/agent_override/secret-agent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("category", "key")
	c.SetParamValues(SettingsCategoryAgentOverride, "secret-agent")
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})

	require.NoError(t, h.GetProjectSetting(c))

	bodyOut := rec.Body.String()
	require.NotContains(t, bodyOut, dummyToken,
		"generic settings read must never expose the real stored secret")
	require.Contains(t, bodyOut, sandbox.RedactedSecretValue,
		"generic settings read must return the redaction placeholder")
}
