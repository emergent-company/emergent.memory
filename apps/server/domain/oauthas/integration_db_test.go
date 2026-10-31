package oauthas

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// connectTestDB opens a throwaway database built from the embedded migrations
// and drops it on cleanup. Skips when unavailable (fails when REQUIRE_DB is
// set, matching the repo convention).
func connectTestDB(t *testing.T) *bun.DB {
	t.Helper()
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "oauthas")
	t.Cleanup(tdb.Close)
	return tdb.DB
}

// seedUserProject inserts a user, org, project and membership row.
func seedUserProject(t *testing.T, db bun.IDB, userID, projectID, orgID, role string) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx,
		`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name) VALUES (?, ?, ?)`,
		userID, "zitadel-"+userID, "Test User")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "Org")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, "Test Project")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO kb.project_memberships (project_id, user_id, role) VALUES (?, ?, ?)`,
		projectID, userID, role)
	require.NoError(t, err)
}

func TestRepositoryOAuthStore(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	repo := NewRepository(db, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	userID, projectID, orgID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	viewerID, viewerProjectID, viewerOrgID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedUserProject(t, db, userID, projectID, orgID, "project_admin")
	seedUserProject(t, db, viewerID, viewerProjectID, viewerOrgID, "project_viewer")

	client := &OAuthClient{
		ClientID:                "client-" + uuid.NewString(),
		ClientName:              "Integration Client",
		RedirectURIs:            []string{"https://app.example/cb"},
		GrantTypes:              []string{"authorization_code"},
		TokenEndpointAuthMethod: "none",
		Scope:                   "graph:read",
	}
	require.NoError(t, repo.CreateClient(ctx, client))
	require.NotEmpty(t, client.ID)

	got, err := repo.GetClient(ctx, client.ClientID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, client.ClientName, got.ClientName)
	require.Equal(t, []string{"https://app.example/cb"}, got.RedirectURIs)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, repo.TouchClient(ctx, client.ClientID, now))
	got, err = repo.GetClient(ctx, client.ClientID)
	require.NoError(t, err)
	require.NotNil(t, got.LastUsedAt)

	// Writable project listing excludes the viewer-only project.
	projects, err := repo.ListWritableProjects(ctx, userID)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, projectID, projects[0].ID)
	require.Equal(t, "Test Project", projects[0].Name)

	// Viewer-only and non-member users get nothing.
	projects, err = repo.ListWritableProjects(ctx, viewerID)
	require.NoError(t, err)
	require.Empty(t, projects)
	projects, err = repo.ListWritableProjects(ctx, uuid.NewString())
	require.NoError(t, err)
	require.Empty(t, projects)

	allowed, err := repo.CanWriteProject(ctx, projectID, userID)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = repo.CanWriteProject(ctx, viewerProjectID, viewerID)
	require.NoError(t, err)
	require.False(t, allowed, "project_viewer is read-only")

	// Codes are stored by hash, single-use, and expiry-aware.
	code := &OAuthAuthorizationCode{
		CodeHash:            hashCode("code-one"),
		ClientID:            client.ClientID,
		UserID:              userID,
		ProjectID:           projectID,
		Scopes:              []string{"graph:read"},
		RedirectURI:         "https://app.example/cb",
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           now.Add(authorizationCodeTTL),
	}
	require.NoError(t, repo.CreateCode(ctx, code))

	fetched, err := repo.GetCodeByHash(ctx, code.CodeHash)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	require.Equal(t, []string{"graph:read"}, fetched.Scopes)
	require.Nil(t, fetched.ConsumedAt)

	consumed, err := repo.ConsumeCode(ctx, code.CodeHash, client.ClientID, code.RedirectURI, now)
	require.NoError(t, err)
	require.True(t, consumed)

	consumed, err = repo.ConsumeCode(ctx, code.CodeHash, client.ClientID, code.RedirectURI, now)
	require.NoError(t, err)
	require.False(t, consumed, "replay must be rejected")

	// Redirect mismatch must not consume an unspent code.
	expired := &OAuthAuthorizationCode{
		CodeHash:            hashCode("code-two"),
		ClientID:            client.ClientID,
		UserID:              userID,
		ProjectID:           projectID,
		Scopes:              []string{"graph:read"},
		RedirectURI:         "https://app.example/cb",
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           now.Add(-time.Minute),
	}
	require.NoError(t, repo.CreateCode(ctx, expired))
	consumed, err = repo.ConsumeCode(ctx, expired.CodeHash, client.ClientID, "https://app.example/other", now)
	require.NoError(t, err)
	require.False(t, consumed)
	consumed, err = repo.ConsumeCode(ctx, expired.CodeHash, client.ClientID, expired.RedirectURI, now)
	require.NoError(t, err)
	require.False(t, consumed, "expired codes are rejected")

	// Grants join client and project names.
	grant := &OAuthGrant{
		ClientID:  client.ClientID,
		UserID:    userID,
		ProjectID: projectID,
		Scopes:    []string{"graph:read", "projects:read"},
		TokenID:   uuid.NewString(),
		CreatedAt: now,
	}
	require.NoError(t, repo.CreateGrant(ctx, grant))
	require.NotEmpty(t, grant.ID)

	rows, err := repo.ListGrantsByUser(ctx, userID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Integration Client", rows[0].ClientName)
	require.Equal(t, "Test Project", rows[0].ProjectName)
	require.Equal(t, []string{"graph:read", "projects:read"}, rows[0].Scopes)

	byToken, err := repo.GetGrantByTokenIDAndClient(ctx, grant.TokenID, client.ClientID)
	require.NoError(t, err)
	require.NotNil(t, byToken)
	require.Equal(t, grant.ID, byToken.ID)

	owned, err := repo.GetGrantByIDForUser(ctx, grant.ID, userID)
	require.NoError(t, err)
	require.NotNil(t, owned)
	other, err := repo.GetGrantByIDForUser(ctx, grant.ID, uuid.NewString())
	require.NoError(t, err)
	require.Nil(t, other)

	revoked, err := repo.RevokeGrant(ctx, grant.ID, userID, now)
	require.NoError(t, err)
	require.True(t, revoked)
	revoked, err = repo.RevokeGrant(ctx, grant.ID, userID, now)
	require.NoError(t, err)
	require.False(t, revoked, "already-revoked grants are not revoked twice")

	name, err := repo.GetProjectName(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, "Test Project", name)
}

// TestServiceExchangeWithRealToken proves the full flow against a real
// database: the minted token is a project-scoped emt_ token, the grant is
// persisted, and revoking the grant revokes the token immediately.
func TestServiceExchangeWithRealToken(t *testing.T) {
	db := connectTestDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	userID, projectID, orgID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedUserProject(t, db, userID, projectID, orgID, "project_admin")

	apitokenRepo := apitoken.NewRepository(db, log)
	apitokenSvc := apitoken.NewService(db, apitokenRepo, nil, log)
	repo := NewRepository(db, log)
	cfg := &config.Config{OAuthAS: config.OAuthASConfig{Enabled: true, RegistrationEnabled: true}}
	svc := NewService(repo, apitokenSvc, apitokenRepo, cfg, log)

	client, err := svc.RegisterClient(ctx, ClientRegistrationRequest{
		ClientName:   "emergent.feedback",
		RedirectURIs: []string{"https://app.feedback.example/callback"},
	})
	require.NoError(t, err)

	authz, err := svc.Authorize(ctx, userID, AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "https://app.feedback.example/callback",
		ProjectID:           projectID,
		Scopes:              []string{"graph:read", "projects:read"},
		CodeChallenge:       rfcChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	code := codeFromRedirect(t, authz.RedirectURL)

	token, err := svc.ExchangeCode(ctx, TokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "https://app.feedback.example/callback",
		ClientID:     client.ClientID,
		CodeVerifier: rfcVerifier,
	})
	require.NoError(t, err)
	require.True(t, len(token.AccessToken) > len("emt_"))
	require.Equal(t, "emt_", token.AccessToken[:4])
	require.Equal(t, projectID, token.ProjectID)
	require.Equal(t, "Test Project", token.ProjectName)

	stored, err := apitokenRepo.FindByHash(ctx, hashCode(token.AccessToken))
	require.NoError(t, err)
	require.NotNil(t, stored, "the minted token must resolve through the API-token store")
	require.NotNil(t, stored.ProjectID)
	require.Equal(t, projectID, *stored.ProjectID)
	require.Nil(t, stored.RevokedAt)

	grants, err := svc.ListGrants(ctx, userID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, client.ClientID, grants[0].ClientID)
	require.Equal(t, []string{"graph:read", "projects:read"}, grants[0].Scopes)

	// Replay is rejected against the real atomic consume.
	_, err = svc.ExchangeCode(ctx, TokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "https://app.feedback.example/callback",
		ClientID:     client.ClientID,
		CodeVerifier: rfcVerifier,
	})
	requireOAuthError(t, err, "invalid_grant", 400)

	require.NoError(t, svc.RevokeGrant(ctx, userID, grants[0].ID))
	stored, err = apitokenRepo.FindByHash(ctx, hashCode(token.AccessToken))
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.RevokedAt, "revoking the grant must revoke the token immediately")
}
