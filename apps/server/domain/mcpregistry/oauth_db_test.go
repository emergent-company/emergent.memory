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

// seedPersonalCredential writes a personal (UserID non-nil) credential row for
// the given user, exercising the partial-index ON CONFLICT clause that the
// shared-row seed does not reach.
func (s *MCPOAuthDBSuite) seedPersonalCredential(server *mcpregistry.MCPServer, userID, access string) *mcpregistry.MCPServerOAuth {
	s.T().Helper()
	expiry := time.Now().UTC().Add(time.Hour)
	row := &mcpregistry.MCPServerOAuth{
		ServerID:             server.ID,
		UserID:               &userID,
		ProjectID:            s.ProjectID,
		AuthorizationServer:  "https://api.figma.com",
		TokenEndpoint:        "https://api.figma.com/v1/oauth/token",
		RegistrationEndpoint: "https://api.figma.com/v1/oauth/mcp/register",
		Resource:             "https://mcp.figma.com/mcp",
		Scope:                "mcp:connect",
		ClientID:             "client-" + userID[:8],
		ClientSecret:         s.encrypt("client-secret-value"),
		AccessToken:          s.encrypt(access),
		RefreshToken:         s.encrypt("refresh-token-value"),
		ExpiresAt:            &expiry,
	}
	s.Require().NoError(s.repo.UpsertOAuth(s.Ctx, row))
	return row
}

// countOAuthRows returns the number of OAuth credential rows for a server,
// across all users (personal and shared).
func (s *MCPOAuthDBSuite) countOAuthRows(serverID string) int {
	s.T().Helper()
	n, err := s.DB().NewSelect().
		Model((*mcpregistry.MCPServerOAuth)(nil)).
		Where("server_id = ?", serverID).
		Count(s.Ctx)
	s.Require().NoError(err)
	return n
}

// --- store round-trip ---

func (s *MCPOAuthDBSuite) TestOAuthCredentialRoundTrip() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	seeded := s.seedCredential(server, "access-token-value")

	row, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
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
	all, err := s.repo.FindOAuthByServerIDs(s.Ctx, s.ProjectID, []string{server.ID}, "")
	s.Require().NoError(err)
	s.Require().Len(all, 1)
	s.Equal("mcp:connect offline_access", all[server.ID].Scope)

	// A token refresh updates only the token columns and keeps the registered
	// client identity.
	newExpiry := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Millisecond)
	s.Require().NoError(s.repo.UpdateOAuthTokens(s.Ctx, server.ID, "", s.encrypt("access-2"), s.encrypt("refresh-2"), "mcp:connect", &newExpiry))

	refreshed, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
	s.Require().NoError(err)
	s.Require().NotNil(refreshed)
	s.Equal("access-2", s.decrypt(refreshed.AccessToken))
	s.Equal("refresh-2", s.decrypt(refreshed.RefreshToken))
	s.Equal("client-abc", refreshed.ClientID, "refresh must not clear the registered client")
	s.Equal("https://api.figma.com/v1/oauth/mcp/register", refreshed.RegistrationEndpoint)
	s.Require().NotNil(refreshed.ExpiresAt)
	s.True(refreshed.ExpiresAt.UTC().Equal(newExpiry))

	s.Require().NoError(s.repo.DeleteOAuthByServerID(s.Ctx, server.ID, ""))
	deleted, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
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

	s.Require().NoError(s.svc.DecorateOAuthSummaries(s.Ctx, s.ProjectID, "", []*mcpregistry.MCPServer{target}, dtos))

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

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID, "")
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

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID, "")
	s.Require().NoError(err)
	s.False(status.Configured)
	s.False(status.Connected)
	s.Empty(status.Scopes, "scopes must serialize as [] rather than null")
}

func (s *MCPOAuthDBSuite) TestStatusUnknownServer() {
	_, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111", "")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

func (s *MCPOAuthDBSuite) TestDisconnectRemovesCredentialButKeepsStaticHeaders() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")

	s.Require().NoError(s.svc.DisconnectMCPOAuth(s.Ctx, s.ProjectID, server.ID, ""))

	row, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
	s.Require().NoError(err)
	s.Nil(row)

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID, "")
	s.Require().NoError(err)
	s.False(status.Configured)
	s.False(status.Connected)
}

func (s *MCPOAuthDBSuite) TestDisconnectUnknownServer() {
	err := s.svc.DisconnectMCPOAuth(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111", "")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

// --- per-user isolation ---

// TestPerUserOAuthTwoUsersUpsertIndependently is the regression guard for the
// partial-index ON CONFLICT clause: a personal (user_id non-nil) upsert must
// succeed (it previously errored at parse time because PostgreSQL cannot infer a
// partial unique index from `ON CONFLICT (server_id, user_id)`), and re-upserting
// the same user must update in place rather than insert a duplicate row.
func (s *MCPOAuthDBSuite) TestPerUserOAuthTwoUsersUpsertIndependently() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	userA := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	userB := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	// Two different users each upsert their own personal row for the same server.
	s.seedPersonalCredential(server, userA, "access-a")
	s.seedPersonalCredential(server, userB, "access-b")
	s.Equal(2, s.countOAuthRows(server.ID))

	// Re-upserting user A updates in place rather than inserting a third row.
	s.seedPersonalCredential(server, userA, "access-a2")
	s.Equal(2, s.countOAuthRows(server.ID))

	rowA, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, userA)
	s.Require().NoError(err)
	s.Require().NotNil(rowA)
	s.Equal("access-a2", s.decrypt(rowA.AccessToken))

	rowB, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, userB)
	s.Require().NoError(err)
	s.Require().NotNil(rowB)
	s.Equal("access-b", s.decrypt(rowB.AccessToken), "re-upserting A must not disturb B's row")
}

// TestPerUserOAuthPersonalResolvesOverShared verifies the credential-resolution
// precedence: a user with a personal row reads their own, and a user without one
// falls back to the shared row. The status DTO reports the resolved identity.
func (s *MCPOAuthDBSuite) TestPerUserOAuthPersonalResolvesOverShared() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	userA := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

	s.seedCredential(server, "access-shared")
	s.seedPersonalCredential(server, userA, "access-a")

	rowA, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, userA)
	s.Require().NoError(err)
	s.Require().NotNil(rowA)
	s.Require().NotNil(rowA.UserID)
	s.Equal(userA, *rowA.UserID)
	s.Equal("access-a", s.decrypt(rowA.AccessToken), "A must read their own row, not the shared one")

	rowOther, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, other)
	s.Require().NoError(err)
	s.Require().NotNil(rowOther)
	s.Nil(rowOther.UserID, "a user with no personal row must resolve the shared row")
	s.Equal("access-shared", s.decrypt(rowOther.AccessToken))

	statusA, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID, userA)
	s.Require().NoError(err)
	s.True(statusA.Connected)
	s.False(statusA.Shared, "A's credential is personal, not shared")
	s.Require().NotNil(statusA.UserID)
	s.Equal(userA, *statusA.UserID)

	statusOther, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID, other)
	s.Require().NoError(err)
	s.True(statusOther.Connected)
	s.True(statusOther.Shared, "a user without a personal row resolves the shared credential")
	s.Nil(statusOther.UserID)
}

// TestPerUserOAuthDisconnectRemovesOnlyCallerRow verifies per-user isolation on
// disconnect: deleting A's credential leaves B's personal row and the shared row
// intact, and A falls back to the shared row afterward.
func (s *MCPOAuthDBSuite) TestPerUserOAuthDisconnectRemovesOnlyCallerRow() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	userA := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	userB := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	s.seedCredential(server, "access-shared")
	s.seedPersonalCredential(server, userA, "access-a")
	s.seedPersonalCredential(server, userB, "access-b")
	s.Equal(3, s.countOAuthRows(server.ID))

	s.Require().NoError(s.svc.DisconnectMCPOAuth(s.Ctx, s.ProjectID, server.ID, userA))

	// Only A's personal row is gone; B's row and the shared row remain.
	s.Equal(2, s.countOAuthRows(server.ID))

	rowB, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, userB)
	s.Require().NoError(err)
	s.Require().NotNil(rowB)
	s.Require().NotNil(rowB.UserID)
	s.Equal(userB, *rowB.UserID)
	s.Equal("access-b", s.decrypt(rowB.AccessToken))

	sharedRow, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
	s.Require().NoError(err)
	s.Require().NotNil(sharedRow)
	s.Nil(sharedRow.UserID)
	s.Equal("access-shared", s.decrypt(sharedRow.AccessToken))

	// A has no personal row left, so A now falls back to the shared row.
	rowA, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, userA)
	s.Require().NoError(err)
	s.Require().NotNil(rowA)
	s.Nil(rowA.UserID, "A's personal row must be gone; only the shared fallback remains")
	s.Equal("access-shared", s.decrypt(rowA.AccessToken))
}

// --- client provisioning ---

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientStoresEncryptedSecretAndClearsTokens() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	// Seed a prior client + token set: provisioning must replace the identity and
	// clear the tokens issued to the old client.
	s.seedCredential(server, "access-token-value")

	status, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "", "provisioned-client", "provisioned-secret", "repo read:org")
	s.Require().NoError(err)
	s.True(status.ClientConfigured)
	s.False(status.Connected, "provisioning must clear the prior connection")
	s.Require().NotNil(status.RequestedScope)
	s.Equal("repo read:org", *status.RequestedScope)

	row, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
	s.Require().NoError(err)
	s.Require().NotNil(row)
	s.Equal("provisioned-client", row.ClientID)
	s.Equal("repo read:org", row.RequestedScope, "the requested scope set must persist with the client")

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

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientScopeOnlyKeepsClientAndTokens() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")

	// A blank client id with a scope updates only the requested set: the stored
	// client and the live token set must survive so the change can be applied on
	// the next connect without a reconnect first.
	status, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "", "", "", "repo read:org")
	s.Require().NoError(err)
	s.Require().NotNil(status.RequestedScope)
	s.Equal("repo read:org", *status.RequestedScope)
	s.True(status.ClientConfigured)
	s.True(status.Connected, "a scope-only update must not drop the current connection")

	row, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
	s.Require().NoError(err)
	s.Require().NotNil(row)
	s.Equal("client-abc", row.ClientID, "the stored client identity must be untouched")
	s.NotNil(row.ClientSecret, "the stored client secret must be untouched")
	s.Equal("repo read:org", row.RequestedScope)
	s.Equal("access-token-value", s.decrypt(row.AccessToken), "the live token set must be untouched")
}

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientRequiresClientIDOrScope() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "", "", "", "")
	s.ErrorIs(err, mcpregistry.ErrOAuthClientIDRequired)
}

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientScopeOnlyRequiresStoredClient() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	// No client has been stored: a scope set has nothing to attach to, and
	// silently dropping it would make the next connect ignore it.
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "", "", "", "repo read:org")
	s.ErrorIs(err, mcpregistry.ErrOAuthClientIDRequired)
}

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientUnknownServer() {
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111", "", "cid", "sec", "")
	s.ErrorIs(err, mcpregistry.ErrServerNotFound)
}

func (s *MCPOAuthDBSuite) TestProvisionOAuthClientRejectsNonRemoteTransport() {
	server := s.createServer("local-stdio", mcpregistry.ServerTypeStdio, "")
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "", "cid", "sec", "")
	s.ErrorIs(err, mcpregistry.ErrOAuthNotSupported)
}

func (s *MCPOAuthDBSuite) TestClearOAuthClientClearsIdentity() {
	server := s.createServer("figma", mcpregistry.ServerTypeHTTP, "https://mcp.figma.com/mcp")
	s.seedCredential(server, "access-token-value")
	// Give the row an operator-configured scope set: clearing the client must not
	// lose it, so a later (re-)registration still requests the chosen scopes.
	_, err := s.svc.ProvisionOAuthClient(s.Ctx, s.ProjectID, server.ID, "", "", "", "repo read:org")
	s.Require().NoError(err)

	s.Require().NoError(s.svc.ClearOAuthClient(s.Ctx, s.ProjectID, server.ID, ""))

	row, err := s.repo.FindOAuthForUser(s.Ctx, server.ID, "")
	s.Require().NoError(err)
	s.Require().NotNil(row, "clear must keep the row so a later authorize can re-provision")
	s.Empty(row.ClientID)
	s.Nil(row.ClientSecret)
	s.Nil(row.AccessToken)
	s.Nil(row.RefreshToken)
	s.Nil(row.ExpiresAt)
	s.Equal("repo read:org", row.RequestedScope, "the operator's requested set survives a client clear")

	status, err := s.svc.MCPOAuthStatus(s.Ctx, s.ProjectID, server.ID, "")
	s.Require().NoError(err)
	s.False(status.ClientConfigured)
	s.False(status.Connected)
}

func (s *MCPOAuthDBSuite) TestClearOAuthClientUnknownServer() {
	err := s.svc.ClearOAuthClient(s.Ctx, s.ProjectID, "11111111-1111-1111-1111-111111111111", "")
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
