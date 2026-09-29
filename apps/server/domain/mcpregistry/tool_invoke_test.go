package mcpregistry_test

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// MCPRegistryToolInvokeSuite proves the CallToolOnServer service contract:
// builtin servers delegate to mcp.Service.ExecuteTool with the transport's
// per-tool authority check, disabled servers/tools are rejected, forbidden
// tools are refused, foreign/missing servers and tools are not found, and
// upstream failures are surfaced as a sanitized 502.
type MCPRegistryToolInvokeSuite struct {
	testutil.BaseSuite
}

func TestMCPRegistryToolInvokeSuite(t *testing.T) {
	suite.Run(t, new(MCPRegistryToolInvokeSuite))
}

func (s *MCPRegistryToolInvokeSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_tool_invoke")
	s.BaseSuite.SetupSuite()
}

// seedServer inserts an MCP server row for the suite's project and returns its ID.
func (s *MCPRegistryToolInvokeSuite) seedServer(serverType string, enabled bool) string {
	serverID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_servers (id, project_id, name, enabled, type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, NOW(), NOW())
	`, serverID, s.ProjectID, "test-server", enabled, serverType).Exec(s.Ctx)
	s.Require().NoError(err)
	return serverID
}

// seedExternalServer inserts an external HTTP server with a URL (for exercising
// the proxied-invoke path) and returns its ID.
func (s *MCPRegistryToolInvokeSuite) seedExternalServer(url string) string {
	serverID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_servers (id, project_id, name, enabled, type, url, created_at, updated_at)
		VALUES (?, ?, ?, true, 'http', ?, NOW(), NOW())
	`, serverID, s.ProjectID, "ext-server", url).Exec(s.Ctx)
	s.Require().NoError(err)
	return serverID
}

// seedTool inserts an MCP server tool row for the given server.
func (s *MCPRegistryToolInvokeSuite) seedTool(serverID, toolName string, enabled bool) {
	_, err := s.DB().NewRaw(`
		INSERT INTO kb.mcp_server_tools (id, server_id, tool_name, enabled, created_at)
		VALUES (?, ?, ?, ?, NOW())
	`, uuid.New().String(), serverID, toolName, enabled).Exec(s.Ctx)
	s.Require().NoError(err)
}

// newService builds a registry service against the suite DB, backed by a real
// mcp.Service with its tool index populated so per-tool authority (AgentOnly /
// RequiredScope / SuperadminOnly) resolves exactly as it does in production.
func (s *MCPRegistryToolInvokeSuite) newService() *mcpregistry.Service {
	mcpSvc := mcp.NewService(mcp.ServiceParams{DB: s.DB(), Cfg: s.TestDB.Config, Log: slog.Default()})
	_ = mcpSvc.GetToolDefinitions() // populate the tool index GetToolByName reads
	return mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), mcpSvc, nil, nil, slog.Default())
}

func (s *MCPRegistryToolInvokeSuite) TestBuiltinDelegates() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "schema-icon-list", true)

	result, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "schema-icon-list", nil, []string{"schema:read"}, "")

	s.Require().NoError(err, "builtin delegation must succeed: %v", err)
	s.Require().NotNil(result)
	s.Require().NotNil(result.StructuredContent, "schema-icon-list must produce structuredContent")
	s.Require().Contains(result.StructuredContent, "count")
}

func (s *MCPRegistryToolInvokeSuite) TestBuiltinAgentOnlyRejected() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "web-fetch", true)

	// web-fetch is AgentOnly: refused regardless of the caller's scopes.
	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "web-fetch", map[string]any{"url": "http://169.254.169.254/"}, auth.GetAllScopes(), "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrToolForbidden),
		"agent-only tool must return ErrToolForbidden, got: %v", err)
}

func (s *MCPRegistryToolInvokeSuite) TestBuiltinScopeDenied() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "schema-icon-list", true)

	// schema-icon-list requires schema:read; "search" does not imply it.
	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "schema-icon-list", nil, []string{"search"}, "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrToolForbidden),
		"missing required scope must return ErrToolForbidden, got: %v", err)
}

func (s *MCPRegistryToolInvokeSuite) TestBuiltinSuperadminDenied() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "provider-list-org", true)

	// provider-list-org is SuperadminOnly: a regular project member is denied.
	memberCtx := auth.ContextWithUser(s.Ctx, &auth.AuthUser{ID: testutil.RegularUser.ID})
	_, err := s.newService().CallToolOnServer(memberCtx, s.ProjectID, serverID, "provider-list-org", nil, auth.GetAllScopes(), "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrToolForbidden),
		"superadmin-only tool must return ErrToolForbidden for a non-superadmin, got: %v", err)
}

func (s *MCPRegistryToolInvokeSuite) TestDisabledServerRejected() {
	serverID := s.seedServer("builtin", false)
	s.seedTool(serverID, "schema-icon-list", true)

	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "schema-icon-list", nil, nil, "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrServerDisabled),
		"disabled server must return ErrServerDisabled, got: %v", err)
}

func (s *MCPRegistryToolInvokeSuite) TestDisabledToolRejected() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "schema-icon-list", false)

	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "schema-icon-list", nil, nil, "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrToolDisabled),
		"disabled tool must return ErrToolDisabled, got: %v", err)
}

func (s *MCPRegistryToolInvokeSuite) TestServerNotFound() {
	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, uuid.New().String(), "schema-icon-list", nil, nil, "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrServerNotFound),
		"missing server must return ErrServerNotFound, got: %v", err)
}

func (s *MCPRegistryToolInvokeSuite) TestToolNotFound() {
	serverID := s.seedServer("builtin", true)

	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "missing-tool", nil, nil, "")

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrToolNotFound),
		"missing tool must return ErrToolNotFound, got: %v", err)
}

// seedShareInstance mints a project-scoped API token and binds it to a
// restricted (non-null) tool allowlist, returning the token ID the invoke path
// resolves the share instance from.
func (s *MCPRegistryToolInvokeSuite) seedShareInstance(allowed []string) string {
	userID := uuid.New().String()
	_, err := s.DB().NewRaw(`
		INSERT INTO core.user_profiles (id, zitadel_user_id, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
	`, userID, "zitadel-"+userID).Exec(s.Ctx)
	s.Require().NoError(err)

	tokenID := uuid.New().String()
	_, err = s.DB().NewRaw(`
		INSERT INTO core.api_tokens (id, user_id, project_id, name, token_hash, token_prefix, scopes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, '{projects:read, schema:read}'::text[], NOW())
	`, tokenID, userID, s.ProjectID, "share-"+tokenID, "hash-"+tokenID, "emt_"+tokenID[:8]).Exec(s.Ctx)
	s.Require().NoError(err)

	_, err = s.DB().NewRaw(`
		INSERT INTO core.mcp_share_instances (id, project_id, name, token_id, allowed_tools, is_legacy, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?::text[], false, NOW(), NOW())
	`, uuid.New().String(), s.ProjectID, "restricted", tokenID, "{"+strings.Join(allowed, ",")+"}").Exec(s.Ctx)
	s.Require().NoError(err)

	return tokenID
}

// TestBuiltinShareInstanceAllowlistDenied proves the per-tool invoke path applies
// the same deny-by-default share-instance tool allowlist as the MCP HTTP
// transports: a token bound to an instance whose allowlist excludes the tool is
// refused even when it holds the tool's RequiredScope.
func (s *MCPRegistryToolInvokeSuite) TestBuiltinShareInstanceAllowlistDenied() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "schema-icon-list", true)

	tokenID := s.seedShareInstance([]string{"entity-query"}) // allowlist excludes schema-icon-list

	_, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "schema-icon-list", nil, []string{"schema:read"}, tokenID)

	s.Require().Error(err)
	s.Require().True(errors.Is(err, mcpregistry.ErrToolForbidden),
		"tool outside the instance allowlist must return ErrToolForbidden, got: %v", err)
}

// TestBuiltinShareInstanceAllowlistPermitted proves the allowlist does not
// over-block: the allowlisted tool runs with a sufficient scope.
func (s *MCPRegistryToolInvokeSuite) TestBuiltinShareInstanceAllowlistPermitted() {
	serverID := s.seedServer("builtin", true)
	s.seedTool(serverID, "schema-icon-list", true)

	tokenID := s.seedShareInstance([]string{"schema-icon-list"})

	result, err := s.newService().CallToolOnServer(s.Ctx, s.ProjectID, serverID, "schema-icon-list", nil, []string{"schema:read"}, tokenID)

	s.Require().NoError(err, "allowlisted tool with sufficient scope must run: %v", err)
	s.Require().NotNil(result)
}

// TestExternalUpstreamFailureSanitized502 proves the handler returns a 502 with
// a sanitized message and never leaks the upstream URL.
func (s *MCPRegistryToolInvokeSuite) TestExternalUpstreamFailureSanitized502() {
	url := "http://127.0.0.1:1" // refused
	serverID := s.seedExternalServer(url)
	s.seedTool(serverID, "search", true)

	resp := s.Client.POST("/api/admin/mcp-servers/"+serverID+"/tools/search/call",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{"arguments": map[string]any{"q": "x"}}))

	s.Require().Equal(http.StatusBadGateway, resp.StatusCode,
		"upstream failure must be 502, got %d: %s", resp.StatusCode, resp.String())
	body := resp.String()
	s.Require().Contains(body, "upstream MCP server call failed",
		"502 body must carry the sanitized message: %s", body)
	s.Require().NotContains(body, "127.0.0.1",
		"502 body must not leak the upstream URL: %s", body)
	s.Require().False(strings.Contains(body, url), "502 body must not leak the upstream URL")
}
