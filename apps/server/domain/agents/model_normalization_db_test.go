package agents

import (
	"context"
	"encoding/json"
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

// TestCreateDefinition_StructuredProviderFoldsIntoName is the DB-backed guard
// for #1467: a client sending structured {"name":"gpt-4o","provider":"openai"}
// is accepted and persisted as the canonical "openai/gpt-4o" name, with the
// provider field cleared so the two representations cannot diverge.
func TestCreateDefinition_StructuredProviderFoldsIntoName(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_model_structured_create")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo}

	c := newAgentDefWriteContext(http.MethodPost, "/api/projects/"+projectID+"/agent-definitions", projectID,
		[]string{"projectId"}, []string{projectID},
		`{"name":"structured-agent","model":{"name":"gpt-4o","provider":"openai"}}`)
	require.NoError(t, h.CreateDefinition(c))

	def, err := repo.FindDefinitionByName(ctx, projectID, "structured-agent")
	require.NoError(t, err)
	require.NotNil(t, def)
	require.NotNil(t, def.Model)
	require.Equal(t, "openai/gpt-4o", def.Model.Name,
		"structured provider + bare name must fold into the canonical prefixed name")
	require.Empty(t, def.Model.Provider, "structured provider must be cleared after folding")
}

// TestMCPCreateAgentDefinition_StructuredProviderFoldsIntoName is the DB-backed
// guard for the MCP create write path (#1465 + #1467): it must accept the same
// structured reference as REST and persist the same canonical form.
func TestMCPCreateAgentDefinition_StructuredProviderFoldsIntoName(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_model_structured_mcp")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)

	repo := NewRepository(tdb.DB)
	h := &MCPToolHandler{repo: repo}

	res, err := h.ExecuteCreateAgentDefinition(ctx, projectID, map[string]any{
		"name":     "mcp-structured-agent",
		"model":    "gpt-4o",
		"provider": "openai",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	def, err := repo.FindDefinitionByName(ctx, projectID, "mcp-structured-agent")
	require.NoError(t, err)
	require.NotNil(t, def)
	require.NotNil(t, def.Model)
	require.Equal(t, "openai/gpt-4o", def.Model.Name,
		"MCP create must fold structured provider exactly like REST")
	require.Empty(t, def.Model.Provider)
}

// TestListDefinitions_LegacyStructuredModelSurfacesPrefixed drives
// h.ListDefinitions against a real repository holding a legacy structured row
// (the pre-#1465 shape {"name":"<bare>","provider":"<slug>"}, which the
// backfill migration has not yet repaired in this fixture) and asserts the
// read path folds it into the canonical "provider/model" string the list
// reports for normalized rows, instead of echoing a bare name.
func TestListDefinitions_LegacyStructuredModelSurfacesPrefixed(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_model_legacy_read")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "legacy-agent", string(VisibilityProject))
	_, err := tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_definitions SET model = ?::jsonb WHERE id = ?`,
		`{"name":"deepseek-v4-flash","provider":"openai"}`, defID)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	h := &Handler{repo: repo} // no modelResolver: projectDefault is "", so override wins

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/agent-definitions", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId")
	c.SetParamValues(projectID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-test-id", ProjectID: projectID})

	require.NoError(t, h.ListDefinitions(c))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp APIResponse[[]AgentDefinitionSummaryDTO]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Len(t, resp.Data, 1, "fixture project should contain exactly the inserted legacy definition")
	require.Equal(t, "openai/deepseek-v4-flash", resp.Data[0].EffectiveModel,
		"legacy structured provider must fold into the canonical prefixed name on read")
}

// TestExecuteListAgents_LegacyStructuredModelSurfacesPrefixed drives the MCP
// list_agents tool against a real repository holding a legacy structured row and
// asserts the DTO surfaces the folded "provider/model" name (and cleared
// provider), not the raw {"name":"<bare>","provider":"<slug>"} shape.
func TestExecuteListAgents_LegacyStructuredModelSurfacesPrefixed(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_model_legacy_mcp_list")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	insertRuntimeAgent(t, tdb.DB, ctx, projectID, "legacy-agent")
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "legacy-agent", string(VisibilityProject))
	_, err := tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_definitions SET model = ?::jsonb WHERE id = ?`,
		`{"name":"deepseek-v4-flash","provider":"openai"}`, defID)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)
	h := &MCPToolHandler{repo: repo}

	res, err := h.ExecuteListAgents(ctx, projectID, nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.Content)

	var dtos []AgentWithDefinitionDTO
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &dtos))
	require.Len(t, dtos, 1, "fixture project should contain exactly the inserted runtime agent")
	require.NotNil(t, dtos[0].Model, "definition model must be present on the list_agents DTO")
	require.Equal(t, "openai/deepseek-v4-flash", dtos[0].Model.Name,
		"legacy structured provider must fold into the canonical prefixed name on list_agents")
	require.Empty(t, dtos[0].Model.Provider, "folded provider must be cleared")
}

// TestEnsureGraphQueryAgent_ClearsLegacyBareModel proves the query agent
// self-heals a stored model reference that cannot be parsed (the legacy bare
// name that made every search-knowledge call fail with "model name ... must
// include a provider prefix"). Clearing it lets the executor resolve the
// project default, while non-reference model fields (temperature) survive.
func TestEnsureGraphQueryAgent_ClearsLegacyBareModel(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "agents_graph_query_bare_model")
	t.Cleanup(tdb.Close)
	ctx := context.Background()

	_, projectID := insertOrgAndProject(t, tdb.DB, ctx)
	defID := insertAgentDefinition(t, tdb.DB, ctx, projectID, "graph-query-agent", string(VisibilityInternal))
	_, err := tdb.DB.ExecContext(ctx,
		`UPDATE kb.agent_definitions SET model = ?::jsonb WHERE id = ?`,
		`{"name":"gemini-3.1-flash-lite-preview","temperature":0.1}`, defID)
	require.NoError(t, err)

	repo := NewRepository(tdb.DB)

	def, err := repo.EnsureGraphQueryAgent(ctx, projectID)
	require.NoError(t, err)
	require.NotNil(t, def)
	require.NotNil(t, def.Model)
	require.Empty(t, def.Model.Name, "invalid bare model reference must be cleared to use the project default")
	require.Empty(t, def.Model.Provider)
	require.NotNil(t, def.Model.Temperature, "temperature must survive the self-heal")
	require.Equal(t, float32(0.1), *def.Model.Temperature)

	reloaded, err := repo.FindDefinitionByID(ctx, defID, &projectID)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	require.NotNil(t, reloaded.Model)
	require.Empty(t, reloaded.Model.Name, "self-heal must persist the cleared reference")
}
