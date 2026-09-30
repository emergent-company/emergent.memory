package mcpregistry_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// MCPRegistryTransportUpdateSuite proves that a registered server's transport
// may be changed after registration (spec: openspec/specs/mcp-servers-ui). The
// UpdateMCPServerDTO carries a Type field; UpdateServer applies the switch and
// clears the previous transport's connection fields so stale config never
// leaks into the new transport.
type MCPRegistryTransportUpdateSuite struct {
	testutil.BaseSuite
}

func TestMCPRegistryTransportUpdateSuite(t *testing.T) {
	suite.Run(t, new(MCPRegistryTransportUpdateSuite))
}

func (s *MCPRegistryTransportUpdateSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_transport_update")
	s.BaseSuite.SetupSuite()
}

func (s *MCPRegistryTransportUpdateSuite) SetupTest() {
	s.BaseSuite.SetupTest()
}

// createHTTPServer registers an http-transport server via the real in-process
// API and returns its ID.
func (s *MCPRegistryTransportUpdateSuite) createHTTPServer(name string) string {
	resp := s.Client.POST("/api/admin/mcp-servers",
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"name": name,
			"type": "http",
			"url":  "https://example.com/mcp",
			"headers": map[string]any{
				"Authorization": "Bearer x",
			},
		}))
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "create http server: %s", resp.String())

	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(resp.Body, &created), "parse create response: %s", resp.String())
	s.Require().NotEmpty(created.Data.ID, "create response must carry the server id")
	return created.Data.ID
}

// readServer returns (type, url, headers, command) for a server row by ID.
func (s *MCPRegistryTransportUpdateSuite) readServer(serverID string) (string, *string, []byte, *string) {
	var (
		typ     string
		url     *string
		headers []byte
		command *string
	)
	err := s.DB().NewRaw(
		`SELECT type, url, headers, command FROM kb.mcp_servers WHERE id = ?`, serverID,
	).Scan(s.Ctx, &typ, &url, &headers, &command)
	s.Require().NoError(err)
	return typ, url, headers, command
}

// TestTransportChangePersists switches an http server to stdio via PATCH and
// asserts the persisted row reflects the new transport with the old remote
// fields cleared and the stdio command stored.
func (s *MCPRegistryTransportUpdateSuite) TestTransportChangePersists() {
	serverID := s.createHTTPServer("transport-switch-http")

	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type":    "stdio",
			"command": "npx -y files-mcp",
		}))
	s.Require().Equal(http.StatusOK, resp.StatusCode, "transport switch must be 200: %s", resp.String())

	typ, url, headers, command := s.readServer(serverID)
	s.Require().Equal("stdio", typ, "type must be stdio after the switch")
	s.Require().Nil(url, "url must be NULL after switching away from a remote transport")
	s.Require().Nil(headers, "headers must be NULL after switching away from a remote transport")
	s.Require().NotNil(command, "command must be set for the stdio transport")
	s.Require().Equal("npx -y files-mcp", *command, "command must persist the submitted value")
}

// TestTransportChangeMissingRequiredField switches an http server to stdio
// without a command and asserts the request is rejected (400) and the stored
// row keeps its original http transport (no partial persist).
func (s *MCPRegistryTransportUpdateSuite) TestTransportChangeMissingRequiredField() {
	serverID := s.createHTTPServer("transport-switch-missing")

	resp := s.Client.PATCH("/api/admin/mcp-servers/"+serverID,
		testutil.WithAuth("e2e-test-user"), testutil.WithProjectID(s.ProjectID),
		testutil.WithJSONBody(map[string]any{
			"type": "stdio",
		}))
	s.Require().Equal(http.StatusBadRequest, resp.StatusCode, "missing command must be 400: %s", resp.String())

	typ, _, _, _ := s.readServer(serverID)
	s.Require().Equal("http", typ, "rejected transport switch must not persist a partial update")
}
