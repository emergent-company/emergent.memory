package sources

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	cryptoutil "github.com/emergent-company/emergent.memory/pkg/crypto"
)

func TestSourceAppRegistrationAndGitHubAppSource(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "github_app_provider")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID, agentID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	otherOrg, otherProject := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?), (?, ?)`, orgID, "prov-org", otherOrg, "other-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?), (?, ?, ?)`, projectID, orgID, "prov-project", otherProject, otherOrg, "other-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "review-agent", projectID)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`, userID, "zitadel-admin")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, orgID, userID)
	require.NoError(t, err)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	key, err := hex.DecodeString(keyHex)
	require.NoError(t, err)
	_ = key
	svc := NewService(repo, &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: keyHex}},
		slog.New(slog.NewTextHandler(io.Discard, nil)), NewRegistry(newGitHubAppProvider()),
		WithAgentRepository(agents.NewRepository(db.DB)))

	app, err := svc.RegisterSourceApp(ctx, orgID, userID.String(), RegisterSourceAppRequest{
		Provider: "github", AppID: 99, Slug: "memory-reviewer", PrivateKey: string(rsaPEMForTest(t)), WebhookSecret: "whsec",
	})
	require.NoError(t, err)
	require.NotEmpty(t, app.PrivateKeyEncrypted)
	require.NotContains(t, string(app.PrivateKeyEncrypted), "PRIVATE KEY")
	apps, err := svc.ListSourceApps(ctx, orgID, userID.String())
	require.NoError(t, err)
	require.Len(t, apps, 1)

	// Another org cannot bind this App.
	mode := SyncPolicyManual
	_, err = svc.Create(ctx, otherProject, &CreateIntegrationRequest{
		Name: "bad", ProviderType: ProviderTypeGitHubApp, SyncMode: &mode,
		Config: map[string]any{"app_id": app.ID.String(), "installation_id": "42", "repos": []any{"owner/repo"}},
	})
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)

	// A github_app source binds the installation and allowlist.
	integ, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name: "bot", ProviderType: ProviderTypeGitHubApp, SyncMode: &mode,
		Config: map[string]any{"app_id": app.ID.String(), "installation_id": "42", "repos": []any{"owner/repo"}},
	})
	require.NoError(t, err)
	src, err := repo.FindSourceByID(ctx, uuid.MustParse(integ.ID))
	require.NoError(t, err)
	require.NotNil(t, src.AppID)
	require.Equal(t, app.ID, *src.AppID)
	require.NotNil(t, src.ExternalAccountID)
	require.Equal(t, "42", *src.ExternalAccountID)

	// The App/installation cannot be swapped through a config update.
	_, err = svc.Update(ctx, projectID, src.ID, &UpdateIntegrationRequest{Config: map[string]any{
		"app_id": uuid.New().String(), "installation_id": "42", "repos": []any{"owner/repo"},
	}})
	require.Error(t, err)

	// Trigger CRUD validates the runtime agent and event type.
	trigger, err := svc.CreateSourceTrigger(ctx, projectID, src.ID, SourceTriggerInput{
		EventType: "pull_request", AgentID: agentID.String(), Queue: "review", Enabled: boolPtr(true),
	})
	require.NoError(t, err)
	require.True(t, trigger.Enabled)
	listed, err := svc.ListSourceTriggers(ctx, projectID, src.ID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	_, err = svc.CreateSourceTrigger(ctx, projectID, src.ID, SourceTriggerInput{EventType: "issue", AgentID: agentID.String()})
	require.Error(t, err)
	updated, err := svc.UpdateSourceTrigger(ctx, projectID, src.ID, trigger.ID, SourceTriggerInput{Enabled: boolPtr(false)})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	updated, err = svc.UpdateSourceTrigger(ctx, projectID, src.ID, trigger.ID, SourceTriggerInput{EventType: "pull_request", AgentID: agentID.String(), Enabled: boolPtr(true)})
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.NoError(t, svc.DeleteSourceTrigger(ctx, projectID, src.ID, trigger.ID))

	// A non-github_app source can change its event type; the column must persist.
	webSource := uuid.New()
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, config, status)
		VALUES (?, ?, 'web', 'web', '{}'::jsonb, 'active')`, webSource, projectID)
	require.NoError(t, err)
	webTrigger, err := svc.CreateSourceTrigger(ctx, projectID, webSource, SourceTriggerInput{EventType: "pull_request", AgentID: agentID.String()})
	require.NoError(t, err)
	webTrigger, err = svc.UpdateSourceTrigger(ctx, projectID, webSource, webTrigger.ID, SourceTriggerInput{EventType: "issue"})
	require.NoError(t, err)
	require.Equal(t, "issue", webTrigger.EventType)
	reloaded, err := repo.FindSourceTriggerByID(ctx, webTrigger.ID)
	require.NoError(t, err)
	require.Equal(t, "issue", reloaded.EventType)

}

func boolPtr(v bool) *bool { return &v }

func rsaPEMForTest(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}
