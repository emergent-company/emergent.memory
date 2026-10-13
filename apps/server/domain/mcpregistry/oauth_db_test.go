package mcpregistry_test

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

// MCPOAuthDBSuite exercises the MCP OAuth credential store, the status/summary
// DTOs, and the connect flow's pre-flight guards against a real database. The
// stub-authorization-server flow (registration, code exchange, refresh) is
// covered by oauth_test.go, which can substitute the SSRF guard for an httptest
// server.
type MCPOAuthDBSuite struct {
	testutil.BaseSuite
	enc *crypto.Encryptor
	svc *mcpregistry.Service
	// unconfigured has no public redirect origin, mirroring a deployment that
	// has not set MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL.
	unconfigured *mcpregistry.Service
	repo         *mcpregistry.Repository
}

func TestMCPOAuthDBSuite(t *testing.T) { suite.Run(t, new(MCPOAuthDBSuite)) }

func (s *MCPOAuthDBSuite) SetupSuite() {
	s.SetDBSuffix("mcpregistry_oauth")
	s.BaseSuite.SetupSuite()
}

func (s *MCPOAuthDBSuite) SetupTest() {
	s.BaseSuite.SetupTest()

	key, err := crypto.GenerateKey()
	s.Require().NoError(err)
	s.enc, err = crypto.NewEncryptor(key)
	s.Require().NoError(err)

	s.repo = mcpregistry.NewRepository(s.DB())

	cfg := mcpregistry.OAuthConfig{
		RedirectBaseURL: "https://api.test",
		LandingURL:      "https://ui.test/settings/mcp-servers",
		ClientName:      "Test Client",
	}
	s.svc, err = mcpregistry.NewServiceWithOAuth(s.repo, nil, s.enc, cfg, slog.Default())
	s.Require().NoError(err)
	s.unconfigured, err = mcpregistry.NewServiceWithOAuth(s.repo, nil, s.enc, mcpregistry.OAuthConfig{}, slog.Default())
	s.Require().NoError(err)
}

// --- helpers ---

func (s *MCPOAuthDBSuite) createServer(name string, typ mcpregistry.MCPServerType, url string) *mcpregistry.MCPServer {
	s.T().Helper()
	dto := &mcpregistry.CreateMCPServerDTO{Name: name, Type: typ}
	if url != "" {
		dto.URL = &url
	}
	if typ == mcpregistry.ServerTypeStdio {
		command := "npx"
		dto.Command = &command
		dto.Args = []string{"-y", "some-mcp-server"}
	}
	server, err := s.svc.CreateServer(s.Ctx, s.ProjectID, dto)
	s.Require().NoError(err)
	s.Require().NotNil(server)
	return server
}

func (s *MCPOAuthDBSuite) encrypt(plaintext string) *mcpregistry.EncryptedSecret {
	s.T().Helper()
	ct, nonce, err := s.enc.Encrypt([]byte(plaintext))
	s.Require().NoError(err)
	return &mcpregistry.EncryptedSecret{Ciphertext: ct, Nonce: nonce}
}

func (s *MCPOAuthDBSuite) decrypt(secret *mcpregistry.EncryptedSecret) string {
	s.T().Helper()
	s.Require().NotNil(secret)
	plaintext, err := s.enc.Decrypt(secret.Ciphertext, secret.Nonce)
	s.Require().NoError(err)
	return string(plaintext)
}

func (s *MCPOAuthDBSuite) seedCredential(server *mcpregistry.MCPServer, access string) *mcpregistry.MCPServerOAuth {
	s.T().Helper()
	expiry := time.Now().UTC().Add(time.Hour)
	row := &mcpregistry.MCPServerOAuth{
		ServerID:             server.ID,
		ProjectID:            s.ProjectID,
		AuthorizationServer:  "https://api.figma.com",
		TokenEndpoint:        "https://api.figma.com/v1/oauth/token",
		RegistrationEndpoint: "https://api.figma.com/v1/oauth/mcp/register",
		Resource:             "https://mcp.figma.com/mcp",
		Scope:                "mcp:connect",
		ClientID:             "client-abc",
		ClientSecret:         s.encrypt("client-secret-value"),
		AccessToken:          s.encrypt(access),
		RefreshToken:         s.encrypt("refresh-token-value"),
		ExpiresAt:            &expiry,
	}
	s.Require().NoError(s.repo.UpsertOAuth(s.Ctx, row))
	return row
}

// --- store round-trip ---

func (s *MCPOAuthDBSuite) TestOAuthCredentialRoundTrip() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	seeded := s.seedCredential(server, "access-token-value")

	row, err := s.repo.FindOAuthByServerID(s.Ctx, server.ID)
	s.Require().NoError(err)
	s.Require().NotNil(row)
	s.Equal(seeded.AuthorizationServer, row.AuthorizationServer)
	s.Equal(seeded.TokenEndpoint, row.TokenEndpoint)
	s.Equal(seeded.RegistrationEndpoint, row.RegistrationEndpoint)
	s.Equal(seeded.Resource, row.Resource)
	s.Equal(seeded.ClientID, row.ClientID)
	s.Equal("access-token-value", s.decrypt(row.AccessToken))
	s.Equal("refresh-token-value", s.decrypt(row.RefreshToken))
	s.Equal("client-secret-value", s.decrypt(row.ClientSecret))
	s.Require().NotNil(row.ExpiresAt)

	// Upsert is idempotent per server: a second write must not create a
	// duplicate row.
	seeded.Scope = "mcp:connect offline_access"
	s.Require().NoError(s.repo.UpsertOAuth(s.Ctx, seeded))
	all, err := s.repo.FindOAuthByServerIDs(s.Ctx, s.ProjectID, []string{server.ID})
	s.Require().NoError(err)
	s.Require().Len(all, 1)
	s.Equal("mcp:connect offline_access", all[server.ID].Scope)

	// A token refresh updates only the token columns and keeps the registered
	// client identity.
	newExpiry := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Millisecond)
	s.Require().NoError(s.repo.UpdateOAuthTokens(s.Ctx, server.ID, s.encrypt("access-2"), s.encrypt("refresh-2"), "mcp:connect", &newExpiry))

	refreshed, err := s.repo.FindOAuthByServerID(s.Ctx, server.ID)
	s.Require().NoError(err)
	s.Require().NotNil(refreshed)
	s.Equal("access-2", s.decrypt(refreshed.AccessToken))
	s.Equal("refresh-2", s.decrypt(refreshed.RefreshToken))
	s.Equal("client-abc", refreshed.ClientID, "refresh must not clear the registered client")
	s.Equal("https://api.figma.com/v1/oauth/mcp/register", refreshed.RegistrationEndpoint)
	s.Require().NotNil(refreshed.ExpiresAt)
	s.True(refreshed.ExpiresAt.UTC().Equal(newExpiry))

	s.Require().NoError(s.repo.DeleteOAuthByServerID(s.Ctx, server.ID))
	deleted, err := s.repo.FindOAuthByServerID(s.Ctx, server.ID)
	s.Require().NoError(err)
	s.Nil(deleted)
}

func (s *MCPOAuthDBSuite) TestOAuthSummaryAttachedToListDTO() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")

	servers, err := s.svc.ListServers(s.Ctx, s.ProjectID)
	s.Require().NoError(err)
	dtos := make([]*mcpregistry.MCPServerDTO, 0, len(servers))
	var target *mcpregistry.MCPServer
	for _, srv := range servers {
		if srv.ID == server.ID {
			target = srv
		}
		dtos = append(dtos, srv.ToDTO())
	}
	s.Require().NotNil(target)

	s.Require().NoError(s.svc.DecorateOAuthSummaries(s.Ctx, s.ProjectID, []*mcpregistry.MCPServer{target}, dtos))

	var found *mcpregistry.MCPServerDTO
	for _, dto := range dtos {
		if dto.ID == server.ID {
			found = dto
		}
	}
	s.Require().NotNil(found)
	s.Require().NotNil(found.OAuth, "a remote server with a credential must carry an oauth summary")
	s.True(found.OAuth.Configured)
	s.True(found.OAuth.Connected)
	s.Require().NotNil(found.OAuth.AuthorizationServer)
	s.Equal("https://api.figma.com", *found.OAuth.AuthorizationServer)
}

// --- status / disconnect ---

func (s *MCPOAuthDBSuite) TestStatusNeverExposesTokenMaterial() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID)
	s.Require().NoError(err)
	s.True(status.Configured)
	s.True(status.Connected)
	s.Require().NotNil(status.AuthorizationServer)
	s.Equal("https://api.figma.com", *status.AuthorizationServer)
	s.Equal([]string{"mcp:connect"}, status.Scopes)
	s.True(status.RegistrationSupported)

	blob, err := json.Marshal(status)
	s.Require().NoError(err)
	for _, secret := range []string{"access-token-value", "refresh-token-value", "client-secret-value", "client-abc", "refresh-token"} {
		s.NotContains(string(blob), secret, "status payload must never carry credential material")
	}
}

func (s *MCPOAuthDBSuite) TestStatusAndSummaryWithoutCredential() {
	server := s.createServer("plain", mcpregistry.ServerTypeHTTP, "https://example.com/mcp")

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID)
	s.Require().NoError(err)
	s.False(status.Configured)
	s.False(status.Connected)
	s.Empty(status.Scopes, "scopes must serialize as [] rather than null")
}

func (s *MCPOAuthDBSuite) TestStatusUnknownServer() {
	_, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

func (s *MCPOAuthDBSuite) TestDisconnectRemovesCredentialButKeepsStaticHeaders() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")

	s.Require().NoError(s.svc.DisconnectMCPOAuth(s.Ctx, s.ProjectID, server.ID))

	row, err := s.repo.FindOAuthByServerID(s.Ctx, server.ID)
	s.Require().NoError(err)
	s.Nil(row)

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID)
	s.Require().NoError(err)
	s.False(status.Configured)
	s.False(status.Connected)
}

func (s *MCPOAuthDBSuite) TestDisconnectUnknownServer() {
	err := s.svc.DisconnectMCPOAuth(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

// --- client provisioning ---

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientStoresEncryptedSecretAndClearsTokens() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	// Seed a prior client + token set: provisioning must replace the identity and
	// clear the tokens issued to the old client.
	s.seedCredential(server, "access-token-value")

	status, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "provisioned-client", "provisioned-secret")
	s.Require().NoError(err)
	s.True(status.ClientConfigured)
	s.False(status.Connected, "provisioning must clear the prior connection")

	row, err := s.repo.FindOAuthByServerID(s.Ctx, server.ID)
	s.Require().NoError(err)
	s.Require().NotNil(row)
	s.Equal("provisioned-client", row.ClientID)

	// The secret is encrypted at rest: the raw stored value is ciphertext, not
	// the plaintext.
	s.Require().NotNil(row.ClientSecret)
	s.NotEqual("provisioned-secret", string(row.ClientSecret.Ciphertext), "raw stored value must not be the plaintext")
	s.Equal("provisioned-secret", s.decrypt(row.ClientSecret), "secret must decrypt to the provisioned value")

	// Tokens issued to the previous client are cleared.
	s.Nil(row.AccessToken)
	s.Nil(row.RefreshToken)
	s.Nil(row.ExpiresAt)
}

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientUnknownServer() {
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111", "cid", "sec")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientRejectsNonRemoteTransport() {
	server := s.createServer("local-stdio", mcpregistry.ServerTypeStdio, "")
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "cid", "sec")
	s.ErrorIs(err, mcpregistry.ErrOAuthNotSupported)
}

func (s *MCPOAuthDBSuite) TestClearOAuthClientClearsIdentity() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")

	s.Require().NoError(s.svc.ClearOAuthClient(s.Ctx, s.ProjectID, server.ID))

	row, err := s.repo.FindOAuthByServerID(s.Ctx, server.ID)
	s.Require().NoError(err)
	s.Require().NotNil(row, "clear must keep the row so a later authorize can re-provision")
	s.Empty(row.ClientID)
	s.Nil(row.ClientSecret)
	s.Nil(row.AccessToken)
	s.Nil(row.RefreshToken)
	s.Nil(row.ExpiresAt)

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID)
	s.Require().NoError(err)
	s.False(status.ClientConfigured)
	s.False(status.Connected)
}

func (s *MCPOAuthDBSuite) TestClearOAuthClientUnknownServer() {
	err := s.svc.ClearOAuthClient(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

// --- start-flow guards (no network) ---

func (s *MCPOAuthDBSuite) TestStartRequiresAConfiguredDeployment() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")

	_, err := s.unconfigured.StartMCPOAuth(s.Ctx, s.ProjectID, server.ID, "")
	s.ErrorIs(err, mcpregistry.ErrOAuthNotConfigured,
		"an unset redirect base URL must produce an actionable configuration error, not a broken authorize URL")
}

func (s *MCPOAuthDBSuite) TestStartRejectsNonRemoteTransport() {
	server := s.createServer("local-stdio", mcpregistry.ServerTypeStdio, "")

	_, err := s.svc.StartMCPOAuth(s.Ctx, s.ProjectID, server.ID, "")
	s.ErrorIs(err, mcpregistry.ErrOAuthNotSupported)
}

func (s *MCPOAuthDBSuite) TestStartUnknownServer() {
	_, err := s.svc.StartMCPOAuth(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111", "")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

// --- callback guards (no network) ---

func (s *MCPOAuthDBSuite) TestCallbackRejectsUnknownOrMissingState() {
	_, _, err := s.svc.CompleteMCPOAuth(s.Ctx, "never-issued", "code-1")
	s.ErrorIs(err, mcpregistry.ErrOAuthStateInvalid)

	_, _, err = s.svc.CompleteMCPOAuth(s.Ctx, "", "code-1")
	s.ErrorIs(err, mcpregistry.ErrOAuthStateInvalid)

	_, _, err = s.svc.CompleteMCPOAuth(s.Ctx, "state-1", "")
	s.ErrorIs(err, mcpregistry.ErrOAuthStateInvalid)
}
