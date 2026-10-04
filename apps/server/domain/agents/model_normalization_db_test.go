package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestCreateDefinition_NormalizesModelName drives h.CreateDefinition against a
// real repository and asserts the persisted model.name is normalized to the
// canonical "provider/model" form, proving the normalization assignment actually
// runs (not merely that validation rejects). A padded reference with internal
// whitespace collapses to "openai/deepseek-v4-flash".
func TestCreateDefinition_NormalizesModelName(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_model_norm_create")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	c := newAgentDefWriteContext(http.MethodPost, "/api/projects/"+projectID+"/agent-definitions", projectID,
		[]string{"projectId"}, []string{projectID},
		`{"name":"norm-agent","model":{"name":"  openai / deepseek-v4-flash  "}}`)
	require.NoError(t, h.CreateDefinition(c))

	def, err := repo.FindDefinitionByName(ctx, projectID, "norm-agent")
	require.NoError(t, err)
	require.NotNil(t, def)
	require.NotNil(t, def.Model)
	require.Equal(t, "openai/deepseek-v4-flash", def.Model.Name,
		"persisted model.name must be normalized to canonical 'provider/model' form")
}

// TestUpdateDefinition_NormalizesModelName drives h.UpdateDefinition against a
// real repository and asserts the stored model.name is normalized, exercising the
// same normalization assignment on the update path.
func TestUpdateDefinition_NormalizesModelName(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_model_norm_update")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "upd-agent", string(VisibilityProject))

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	c := newAgentDefWriteContext(http.MethodPatch, "/api/projects/"+projectID+"/agent-definitions/"+defID, projectID,
		[]string{"id"}, []string{defID},
		`{"model":{"name":"  google / gemini-2.5-flash  "}}`)
	require.NoError(t, h.UpdateDefinition(c))

	def, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, def)
	require.NotNil(t, def.Model)
	require.Equal(t, "google/gemini-2.5-flash", def.Model.Name,
		"persisted model.name must be normalized to canonical 'provider/model' form")
}

// newAgentDefWriteContext builds an authenticated Echo context with the given
// route params and an auth user scoped to projectID, mirroring the webhook test
// helper. It exists because newEchoContextWithUser hardcodes project "proj-test-id",
// which does not exist in the DB-backed fixture.
func newAgentDefWriteContext(method, path, projectID string, paramNames, paramValues []string, body string) echo.Context {
	e := echo.New()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if len(paramNames) > 0 {
		c.SetParamNames(paramNames...)
		c.SetParamValues(paramValues...)
	}
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})
	return c
}
