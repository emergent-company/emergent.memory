package mcpregistry_test

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// RequiredConfigSuite exercises the required-config install gate, enable gate,
// and save-less test probe against a real database.
type RequiredConfigSuite struct {
	testutil.BaseSuite
	enc *crypto.Encryptor
	svc *mcpregistry.Service
}

func TestRequiredConfigSuite(t *testing.T) { suite.Run(t, new(RequiredConfigSuite)) }

func (s *RequiredConfigSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_required_config")
	s.BaseSuite.SetupSuite()
}

func (s *RequiredConfigSuite) SetupTest() {
	s.BaseSuite.SetupTest()
	key, err := crypto.GenerateKey()
	s.Require().NoError(err)
	s.enc, err = crypto.NewEncryptor(key)
	s.Require().NoError(err)
	s.svc, err = mcpregistry.NewService(mcpregistry.NewRepository(s.DB()), nil, s.enc, slog.Default())
	s.Require().NoError(err)
}

func (s *RequiredConfigSuite) serverCount() int {
	n, err := s.DB().NewSelect().Model((*mcpregistry.MCPServer)(nil)).Count(s.Ctx)
	s.Require().NoError(err)
	return n
}

// TestInstallFromRegistryRequiredConfig installs the GitHub entry (whose
// selected streamable-http remote declares an Authorization secret header) and
// asserts the server is created disabled with the key surfaced as required.
func (s *RequiredConfigSuite) TestInstallFromRegistryRequiredConfig() {
	result, err := s.svc.InstallFromRegistry(s.Ctx, s.ProjectID, &mcpregistry.InstallFromRegistryDTO{
		RegistryName: "io.github.github/github-mcp-server",
	})
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().NotNil(result.Server)

	s.Require().False(result.Server.Enabled, "install with required config must be created disabled")

	var names []string
	for _, k := range result.RequiredConfig {
		names = append(names, k.Name)
	}
	s.Require().ElementsMatch([]string{"Authorization"}, names,
		"install result must name the required header")

	s.Require().True(result.Server.NeedsConfiguration,
		"a disabled install with a blank secret must report needsConfiguration")
	s.Require().ElementsMatch([]string{"Authorization"}, result.Server.MissingConfigKeys)
}

// TestUpdateServerEnableGateBlankRequired verifies enabling a server with a
// still-blank required key is rejected with ErrRequiredConfigMissing.
func (s *RequiredConfigSuite) TestUpdateServerEnableGateBlankRequired() {
	server := s.seedServerWithRequiredHeader()

	enable := true
	_, err := s.svc.UpdateServer(s.Ctx, server.ID, s.ProjectID, &mcpregistry.UpdateMCPServerDTO{
		Enabled: &enable,
	})
	s.Require().Error(err)
	var missing *mcpregistry.ErrRequiredConfigMissing
	s.Require().True(errors.As(err, &missing), "expected *ErrRequiredConfigMissing, got %T: %v", err, err)
	s.Require().ElementsMatch([]string{"Authorization"}, missing.Keys)
}

// TestUpdateServerEnableGateFillThenEnableSucceeds verifies that filling the
// required header in the same request that enables the server passes the gate.
func (s *RequiredConfigSuite) TestUpdateServerEnableGateFillThenEnableSucceeds() {
	server := s.seedServerWithRequiredHeader()

	enable := true
	updated, err := s.svc.UpdateServer(s.Ctx, server.ID, s.ProjectID, &mcpregistry.UpdateMCPServerDTO{
		Enabled:           &enable,
		Headers:           map[string]any{"Authorization": "Bearer tok"},
		SecretHeadersKeys: []string{"Authorization"},
	})
	s.Require().NoError(err)
	s.Require().NotNil(updated)
	s.Require().True(updated.Enabled)

	// The stored secret must be present (encrypted) so the server can connect.
	s.Require().Len(updated.SecretHeaders, 1)
	s.Require().Contains(updated.SecretHeaders, "Authorization")
}

// TestTestServerConfigNoRepoWrite proves the save-less probe never persists a row.
func (s *RequiredConfigSuite) TestTestServerConfigNoRepoWrite() {
	before := s.serverCount()

	cmd := "echo"
	result, err := s.svc.TestServerConfig(s.Ctx, s.ProjectID, &mcpregistry.CreateMCPServerDTO{
		Name:    "probe",
		Type:    mcpregistry.ServerTypeStdio,
		Command: &cmd,
	}, nil)
	s.Require().NoError(err)
	s.Require().NotNil(result, "the probe must return an inspect DTO")

	after := s.serverCount()
	s.Require().Equal(before, after, "test probe must never write to the repository")
}

// seedServerWithRequiredHeader creates an enabled HTTP server whose required
// config declares a blank Authorization secret header, mirroring a fresh
// registry install before the user configures the credential.
func (s *RequiredConfigSuite) seedServerWithRequiredHeader() *mcpregistry.MCPServer {
	url := "https://8.8.8.8/mcp"
	server, err := s.svc.CreateServer(s.Ctx, s.ProjectID, &mcpregistry.CreateMCPServerDTO{
		Name: "gh-required",
		Type: mcpregistry.ServerTypeHTTP,
		URL:  &url,
		RequiredConfig: []mcpregistry.RequiredConfigKey{
			{Name: "Authorization", Kind: "header", IsSecret: true, IsRequired: true},
		},
		Headers:           map[string]any{"Authorization": ""},
		SecretHeadersKeys: []string{"Authorization"},
	})
	s.Require().NoError(err)
	s.Require().NotNil(server)
	return server
}
