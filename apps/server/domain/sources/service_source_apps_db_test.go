package sources

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"log/slog"
	"testing"
	"time"

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
	// The provider is trigger-only: no cadence, and an explicit scheduled mode
	// is refused instead of leaving a source the sweep can never satisfy.
	require.Equal(t, SyncPolicyManual, src.SyncPolicy)
	require.Nil(t, src.SyncIntervalMinutes)
	scheduled := SyncPolicyScheduled
	_, err = svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name: "sched", ProviderType: ProviderTypeGitHubApp, SyncMode: &scheduled,
		Config: map[string]any{"app_id": app.ID.String(), "installation_id": "42", "repos": []any{"owner/repo"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "event-driven")

	// Without a mode the source defaults to manual with no interval, so nothing
	// can schedule it.
	defaulted, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name: "defaulted", ProviderType: ProviderTypeGitHubApp,
		Config: map[string]any{"app_id": app.ID.String(), "installation_id": "42", "repos": []any{"owner/repo"}},
	})
	require.NoError(t, err)
	defaultedSrc, err := repo.FindSourceByID(ctx, uuid.MustParse(defaulted.ID))
	require.NoError(t, err)
	require.Equal(t, SyncPolicyManual, defaultedSrc.SyncPolicy)
	require.Nil(t, defaultedSrc.SyncIntervalMinutes)
	require.NoError(t, svc.Delete(ctx, projectID, defaultedSrc.ID))

	// An explicit OAuth app cannot be pinned to a config-only provider (#554).
	_, err = svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name: "pinned", ProviderType: ProviderTypeGitHubApp, SyncMode: &mode, OAuthAppID: strPtr(uuid.New().String()),
		Config: map[string]any{"app_id": app.ID.String(), "installation_id": "42", "repos": []any{"owner/repo"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not use OAuth")

	// A numeric installation id (float64 after JSON decode) is stored as the
	// digits the webhook dispatcher compares against, not fmt.Sprint's 1.2e+07.
	numericApp, err := svc.RegisterSourceApp(ctx, orgID, userID.String(), RegisterSourceAppRequest{
		Provider: "github", AppID: 100, Slug: "numeric", PrivateKey: string(rsaPEMForTest(t)), WebhookSecret: "whsec2",
	})
	require.NoError(t, err)
	numeric, err := svc.Create(ctx, projectID, &CreateIntegrationRequest{
		Name: "numeric", ProviderType: ProviderTypeGitHubApp, SyncMode: &mode,
		Config: map[string]any{"app_id": numericApp.ID.String(), "installation_id": float64(12345678), "repos": []any{"owner/repo"}},
	})
	require.NoError(t, err)
	numericSrc, err := repo.FindSourceByID(ctx, uuid.MustParse(numeric.ID))
	require.NoError(t, err)
	require.NotNil(t, numericSrc.ExternalAccountID)
	require.Equal(t, "12345678", *numericSrc.ExternalAccountID)

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
	// A second binding for the same event is a unique violation mapped to 409.
	_, err = svc.CreateSourceTrigger(ctx, projectID, src.ID, SourceTriggerInput{EventType: "pull_request", AgentID: agentID.String()})
	require.Error(t, err)
	var duplicate *apperror.Error
	require.ErrorAs(t, err, &duplicate)
	require.Equal(t, 409, duplicate.HTTPStatus)
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
	reloaded, err := repo.FindSourceTriggerByID(ctx, projectID, webTrigger.ID)
	require.NoError(t, err)
	require.Equal(t, "issue", reloaded.EventType)

	// The org Apps DELETE endpoint refuses while the App is bound to a project
	// source, naming it, and succeeds once the source is detached (#570).
	err = svc.DeleteSourceApp(ctx, orgID, userID.String(), app.ID)
	require.Error(t, err)
	var inUse *apperror.Error
	require.ErrorAs(t, err, &inUse)
	require.Equal(t, 409, inUse.HTTPStatus)
	require.Contains(t, inUse.Message, "bot")
	require.Contains(t, inUse.Message, src.ID.String())

	require.NoError(t, svc.Delete(ctx, projectID, src.ID))
	require.NoError(t, svc.DeleteSourceApp(ctx, orgID, userID.String(), app.ID))
	_, err = svc.ListSourceApps(ctx, orgID, userID.String())
	require.NoError(t, err)

	// The scheduler never admits a trigger-only row, even one edited directly to
	// scheduled: it never writes last_synced_at, so it would be due every pass.
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, config, status, sync_policy, sync_interval_minutes)
		VALUES (?, ?, 'github_app', 'swept', '{}'::jsonb, 'active', 'scheduled', 60)`, uuid.New(), projectID)
	require.NoError(t, err)
	due, err := repo.ListDueSources(ctx, time.Now())
	require.NoError(t, err)
	for _, d := range due {
		require.NotEqual(t, ProviderTypeGitHubApp, d.Provider)
	}
}

func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }

func rsaPEMForTest(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// rsaPKCS8PEMForTest is the "PRIVATE KEY" (PKCS#8) encoding GitHub also issues,
// as opposed to rsaPEMForTest's PKCS#1.
func rsaPKCS8PEMForTest(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// ecPKCS8PEMForTest is a structurally valid PKCS#8 PEM that is not an RSA key.
func ecPKCS8PEMForTest(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// TestRegisterSourceAppValidatesKeyAndProvider pins the registration contract:
// a credential or provider the dispatcher cannot use is rejected with a 400
// naming the field, before anything is sealed, and leaves no row behind — the
// sealed columns are write-only, so a bad key stored cleanly is undiagnosable.
func TestRegisterSourceAppValidatesKeyAndProvider(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx, svc, orgID, userID := newSourceAppValidationService(t)

	request := func(appID int64) RegisterSourceAppRequest {
		return RegisterSourceAppRequest{
			Provider: "github", AppID: appID, Slug: "validation",
			PrivateKey: string(rsaPEMForTest(t)), WebhookSecret: "whsec",
		}
	}
	countApps := func() int {
		t.Helper()
		apps, err := svc.ListSourceApps(ctx, orgID, userID)
		require.NoError(t, err)
		return len(apps)
	}

	rejected := []struct {
		name   string
		key    string
		reason string
	}{
		{"arbitrary text", "definitely not a pem", "not PEM encoded"},
		{"unsupported PEM block type", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("x")})), "unsupported PEM block type"},
		{"EC key in a PKCS#8 PEM", string(ecPKCS8PEMForTest(t)), "not an RSA key"},
		{"corrupt base64 body", "-----BEGIN RSA PRIVATE KEY-----\nnot base64 at all!!\n-----END RSA PRIVATE KEY-----\n", "not PEM encoded"},
		{"valid PEM, malformed DER", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("not der")})), "could not be parsed"},
		// Empty is caught by the required-field check, not the parser.
		{"empty", "", "required"},
	}
	for i, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			before := countApps()
			req := request(int64(600 + i))
			req.PrivateKey = tc.key
			app, err := svc.RegisterSourceApp(ctx, orgID, userID, req)
			require.Nil(t, app)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 400, appErr.HTTPStatus)
			// The error names the field and the reason, never the key bytes.
			require.Contains(t, appErr.Message, "privateKey")
			require.Contains(t, appErr.Message, tc.reason)
			if tc.key != "" {
				require.NotContains(t, appErr.Message, tc.key)
			}
			// A rejected registration must leave no row behind.
			require.Equal(t, before, countApps(), "rejected registration created a row")
		})
	}

	// Providers are matched exactly, not folded: a typo must not become a row
	// that bindGitHubAppSource refuses forever while occupying the unique
	// (org_id, provider, app_id) index.
	for i, provider := range []string{"gitlab", "gihub", "GitHub", " github-app "} {
		t.Run("rejects provider "+provider, func(t *testing.T) {
			before := countApps()
			req := request(int64(650 + i))
			req.Provider = provider
			app, err := svc.RegisterSourceApp(ctx, orgID, userID, req)
			require.Nil(t, app)
			var appErr *apperror.Error
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 400, appErr.HTTPStatus)
			require.Contains(t, appErr.Message, "provider")
			require.Equal(t, before, countApps(), "rejected provider created a row")
		})
	}

	accepted := []struct {
		name     string
		provider string
		key      []byte
		appID    int64
	}{
		{"PKCS#1 RSA", "github", rsaPEMForTest(t), 700},
		{"PKCS#8 RSA", "github", rsaPKCS8PEMForTest(t), 701},
		{"default provider", "", rsaPEMForTest(t), 702},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			before := countApps()
			req := request(tc.appID)
			req.Provider = tc.provider
			req.PrivateKey = string(tc.key)
			app, err := svc.RegisterSourceApp(ctx, orgID, userID, req)
			require.NoError(t, err)
			require.Equal(t, SourceAppProviderGitHub, app.Provider)
			require.Equal(t, before+1, countApps())
			// Sealed, not stored plaintext: no readable key material.
			require.NotEmpty(t, app.PrivateKeyEncrypted)
			require.NotContains(t, string(app.PrivateKeyEncrypted), "PRIVATE KEY")
			require.NotContains(t, string(app.PrivateKeyEncrypted), "BEGIN")
		})
	}

	// Registration accepts exactly what the binder serves: every accepted App
	// has a provider SourceAppProviderSupported reports as bindable.
	apps, err := svc.ListSourceApps(ctx, orgID, userID)
	require.NoError(t, err)
	require.Len(t, apps, len(accepted))
	for _, app := range apps {
		require.True(t, SourceAppProviderSupported(app.Provider), "registered provider %q is not bindable", app.Provider)
	}
}

// newSourceAppValidationService builds a fresh database with one org and one
// org admin, and a service wired for registration. It reuses the same fixtures
// and harness as TestSourceAppRegistrationAndGitHubAppSource.
func newSourceAppValidationService(t *testing.T) (context.Context, *Service, uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_app_validation")
	t.Cleanup(db.Close)
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, userID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "validation-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO core.user_profiles (id, zitadel_user_id) VALUES (?, ?)`, userID, "zitadel-validation-admin")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.organization_memberships (organization_id, user_id, role) VALUES (?, ?, 'org_admin')`, orgID, userID)
	require.NoError(t, err)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	svc := NewService(repo, &config.Config{LLMProvider: config.LLMProviderConfig{EncryptionKey: keyHex}},
		slog.New(slog.NewTextHandler(io.Discard, nil)), NewRegistry(newGitHubAppProvider()))
	return ctx, svc, orgID, userID.String()
}
