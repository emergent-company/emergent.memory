package sources

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	cryptoutil "github.com/emergent-company/emergent.memory/pkg/crypto"
)

func TestSourceEventDispatcher(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutilDB(t, ctx, "source_events")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	agentsRepo := agents.NewRepository(db.DB)

	orgID, projectID, agentID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "events-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "events-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "review-agent", projectID)
	require.NoError(t, err)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	keyBytes, err := hex.DecodeString(keyHex)
	require.NoError(t, err)
	const webhookSecret = "whsec"
	sealedKey, err := SealSourceBlob(keyBytes, rsaPEM(t))
	require.NoError(t, err)
	sealedSecret, err := SealSourceBlob(keyBytes, []byte(webhookSecret))
	require.NoError(t, err)

	app := &SourceApp{OrgID: orgID, Provider: "github", AppID: 99, Slug: "memory-reviewer", PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret}
	require.NoError(t, repo.CreateSourceApp(ctx, app))

	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
		VALUES (?, ?, 'github_app', 'bot', ?, '42', '{"repos":["owner/repo"]}'::jsonb, 'active')`, sourceID, projectID, app.ID)
	require.NoError(t, err)
	trigger := &SourceTrigger{ProjectID: projectID, SourceID: sourceID, EventType: "pull_request", AgentID: agentID, Queue: "security-review", EventFilter: map[string]any{}, Enabled: true}
	require.NoError(t, repo.CreateSourceTrigger(ctx, trigger))

	// Fake GitHub: token mint + live head.
	currentHead := strings.Repeat("a", 40)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
		case "/repos/owner/repo/pulls/7":
			_, _ = fmt.Fprintf(w, `{"number":7,"title":"fix","state":"open","user":{"login":"mkucharz","type":"User"},"head":{"sha":%q,"ref":"feat/x"},"base":{"ref":"dev"}}`, currentHead)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	dispatcher := NewSourceEventDispatcher(repo, agentsRepo, &config.Config{
		LLMProvider:  config.LLMProviderConfig{EncryptionKey: keyHex},
		GitHubSource: config.GitHubSourceConfig{APIBaseURL: fake.URL},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dispatcher.minter = githubapp.NewTokenMinter(fake.URL, fake.Client())
	dispatcher.newClient = func(_, token string) *githubapp.Client {
		return githubapp.NewClient(fake.URL, token, fake.Client())
	}

	payload := func(action, head string) []byte {
		return []byte(fmt.Sprintf(`{"action":%q,"installation":{"id":42},"repository":{"full_name":"owner/repo"},"pull_request":{"number":7,"title":"fix","state":"open","draft":false,"user":{"login":"mkucharz","type":"User"},"base":{"ref":"dev","sha":"b0"},"head":{"ref":"feat/x","sha":%q}}}`,
			action, head))
	}
	deliver := func(delivery string, body []byte) error {
		return dispatcher.HandleWebhook(ctx, app.WebhookKey, "pull_request", delivery, signWebhook(webhookSecret, body), body)
	}

	require.NoError(t, deliver("d1", payload("opened", currentHead)))
	ev, err := repo.FindSourceEventByDelivery(ctx, projectID, sourceID, "d1")
	require.NoError(t, err)
	require.Equal(t, SourceEventDispatched, ev.State)
	claim, err := repo.FindSourceEventClaim(ctx, projectID, sourceID, "owner/repo#7", currentHead)
	require.NoError(t, err)
	require.Equal(t, SourceClaimRunning, claim.State)
	require.NotNil(t, claim.RunID)
	var run agents.AgentRun
	require.NoError(t, db.DB.NewSelect().Model(&run).Where("id = ?", claim.RunID.String()).Scan(ctx))
	require.Equal(t, sourceID.String(), run.TriggerMetadata["sourceId"])
	require.Equal(t, "owner/repo#7", run.TriggerMetadata["subjectRef"])

	// Duplicate delivery is a no-op.
	require.NoError(t, deliver("d1", payload("opened", currentHead)))
	require.Equal(t, 1, countSourceRuns(t, db, agentID))

	// Out-of-order head is skipped and does not disturb the active claim.
	require.NoError(t, deliver("d2", payload("synchronize", strings.Repeat("b", 40))))
	ev2, err := repo.FindSourceEventByDelivery(ctx, projectID, sourceID, "d2")
	require.NoError(t, err)
	require.Equal(t, SourceEventSkipped, ev2.State)
	require.Equal(t, 1, countSourceRuns(t, db, agentID))

	// Invalid signature and unknown app fail closed.
	require.ErrorIs(t, dispatcher.HandleWebhook(ctx, app.WebhookKey, "pull_request", "d-bad", "sha256=deadbeef", payload("opened", currentHead)), ErrSourceEventInvalidSignature)
	require.ErrorIs(t, dispatcher.HandleWebhook(ctx, uuid.New(), "pull_request", "d-unknown", "sha256=00", payload("opened", currentHead)), ErrSourceAppNotFound)

	// Disabled trigger: a new head claims but does not enqueue.
	trigger.Enabled = false
	require.NoError(t, repo.UpdateSourceTrigger(ctx, trigger))
	currentHead = strings.Repeat("c", 40)
	require.NoError(t, deliver("d3", payload("opened", currentHead)))
	ev3, err := repo.FindSourceEventByDelivery(ctx, projectID, sourceID, "d3")
	require.NoError(t, err)
	require.Equal(t, SourceEventSkipped, ev3.State)
	require.Equal(t, 1, countSourceRuns(t, db, agentID))

	// Uninstall disables the source.
	require.NoError(t, dispatcher.HandleWebhook(ctx, app.WebhookKey, "installation", "d4",
		signWebhook(webhookSecret, []byte(`{"action":"deleted","installation":{"id":42}}`)),
		[]byte(`{"action":"deleted","installation":{"id":42}}`)))
	_, err = repo.FindSourceByAppAndInstallation(ctx, app.ID, "42")
	require.Error(t, err)
}

// TestSourceEventDispatcherDedupesUnderFailClosedRLS drives the ingress as a
// non-superuser, non-BYPASSRLS role so the 00291 policies are actually
// enforced. Two identical deliveries must still produce exactly one run and
// one event row: the dedupe INSERT must carry the project GUC or WITH CHECK
// refuses it (a 500 on every retry), and the claim/trigger reads must carry it
// or they silently see zero rows.
func TestSourceEventDispatcherDedupesUnderFailClosedRLS(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_events_rls_dedupe")
	defer db.Close()
	setupRepo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID, agentID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "rls-dedupe-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "rls-dedupe-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`,
		agentID, "review-agent", projectID)
	require.NoError(t, err)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	keyBytes, _ := hex.DecodeString(keyHex)
	sealedKey, err := SealSourceBlob(keyBytes, rsaPEM(t))
	require.NoError(t, err)
	sealedSecret, err := SealSourceBlob(keyBytes, []byte("whsec"))
	require.NoError(t, err)
	app := &SourceApp{OrgID: orgID, Provider: "github", AppID: 88, Slug: "memory-reviewer", PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret}
	require.NoError(t, setupRepo.CreateSourceApp(ctx, app))
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
		VALUES (?, ?, 'github_app', 'bot', ?, '42', '{"repos":["owner/repo"]}'::jsonb, 'active')`, sourceID, projectID, app.ID)
	require.NoError(t, err)
	require.NoError(t, setupRepo.CreateSourceTrigger(ctx, &SourceTrigger{
		ProjectID: projectID, SourceID: sourceID, EventType: "pull_request",
		AgentID: agentID, Queue: "review", EventFilter: map[string]any{}, Enabled: true,
	}))

	currentHead := strings.Repeat("a", 40)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
		case "/repos/owner/repo/pulls/7":
			_, _ = fmt.Fprintf(w, `{"number":7,"title":"fix","state":"open","user":{"login":"mkucharz","type":"User"},"head":{"sha":%q,"ref":"feat/x"},"base":{"ref":"dev"}}`, currentHead)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	// The ingress itself runs as a role that RLS applies to.
	probe := probeBunDB(t, ctx, db, createRLSProbeRole(t, ctx, db.DB, "dispatch"))
	dispatcher := NewSourceEventDispatcher(
		NewRepository(probe, slog.New(slog.NewTextHandler(io.Discard, nil))),
		agents.NewRepository(probe),
		&config.Config{
			LLMProvider:  config.LLMProviderConfig{EncryptionKey: keyHex},
			GitHubSource: config.GitHubSourceConfig{APIBaseURL: fake.URL},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	dispatcher.minter = githubapp.NewTokenMinter(fake.URL, fake.Client())
	dispatcher.newClient = func(_, token string) *githubapp.Client {
		return githubapp.NewClient(fake.URL, token, fake.Client())
	}

	body := []byte(fmt.Sprintf(`{"action":"opened","installation":{"id":42},"repository":{"full_name":"owner/repo"},"pull_request":{"number":7,"title":"fix","state":"open","draft":false,"user":{"login":"mkucharz","type":"User"},"base":{"ref":"dev","sha":"b0"},"head":{"ref":"feat/x","sha":%q}}}`, currentHead))
	deliver := func() error {
		return dispatcher.HandleWebhook(ctx, app.WebhookKey, "pull_request", "d1", signWebhook("whsec", body), body)
	}

	require.NoError(t, deliver(), "the dedupe INSERT must be allowed with the project GUC")
	require.NoError(t, deliver(), "the retry must be a dedupe no-op, not a 500")
	require.Equal(t, 1, countSourceRuns(t, db, agentID), "two identical deliveries must enqueue exactly one run")

	var events int
	require.NoError(t, db.DB.NewRaw(`SELECT count(*) FROM kb.source_events WHERE source_id = ?`, sourceID).Scan(ctx, &events))
	require.Equal(t, 1, events, "two identical deliveries must leave exactly one event row")
}

func testutilDB(t *testing.T, ctx context.Context, suffix string) *testdb.TestDB {
	t.Helper()
	return testdb.SetupTestDBOrFail(t, ctx, suffix)
}

func rsaPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func countSourceRuns(t *testing.T, db *testdb.TestDB, agentID uuid.UUID) int {
	t.Helper()
	n, err := db.DB.NewSelect().Table("kb.agent_runs").Where("agent_id = ?", agentID).Count(context.Background())
	require.NoError(t, err)
	return n
}

func TestSourceEventDispatcherFansOutAndLifecycle(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_events_fanout")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID := uuid.New(), uuid.New()
	agent1, agent2 := uuid.New(), uuid.New()
	source1, source2 := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "fanout-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "fanout-project")
	require.NoError(t, err)
	for _, agentID := range []uuid.UUID{agent1, agent2} {
		_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "agent-"+agentID.String()[:6], projectID)
		require.NoError(t, err)
	}

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	keyBytes, _ := hex.DecodeString(keyHex)
	sealedKey, err := SealSourceBlob(keyBytes, rsaPEM(t))
	require.NoError(t, err)
	sealedSecret, err := SealSourceBlob(keyBytes, []byte("whsec"))
	require.NoError(t, err)
	app := &SourceApp{OrgID: orgID, Provider: "github", AppID: 77, Slug: "memory-reviewer", PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret}
	require.NoError(t, repo.CreateSourceApp(ctx, app))

	for _, spec := range []struct {
		id, agent uuid.UUID
	}{{source1, agent1}, {source2, agent2}} {
		_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
			VALUES (?, ?, 'github_app', 'bot', ?, '42', '{"repos":["owner/repo"]}'::jsonb, 'active')`, spec.id, projectID, app.ID)
		require.NoError(t, err)
		require.NoError(t, repo.CreateSourceTrigger(ctx, &SourceTrigger{ProjectID: projectID, SourceID: spec.id, EventType: "pull_request", AgentID: spec.agent, Queue: "review", EventFilter: map[string]any{}, Enabled: true}))
	}

	currentHead := strings.Repeat("a", 40)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`))
		case "/repos/owner/repo/pulls/7":
			_, _ = fmt.Fprintf(w, `{"number":7,"title":"fix","state":"open","user":{"login":"mkucharz","type":"User"},"head":{"sha":%q,"ref":"feat/x"},"base":{"ref":"dev"}}`, currentHead)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	d := NewSourceEventDispatcher(repo, agents.NewRepository(db.DB), &config.Config{
		LLMProvider:  config.LLMProviderConfig{EncryptionKey: keyHex},
		GitHubSource: config.GitHubSourceConfig{APIBaseURL: fake.URL},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.minter = githubapp.NewTokenMinter(fake.URL, fake.Client())
	d.newClient = func(_, token string) *githubapp.Client { return githubapp.NewClient(fake.URL, token, fake.Client()) }

	payload := func(action, login, repoName, head string) []byte {
		return []byte(fmt.Sprintf(`{"action":%q,"installation":{"id":42},"repository":{"full_name":%q},"pull_request":{"number":7,"title":"fix","state":"open","draft":false,"user":{"login":%q,"type":"User"},"base":{"ref":"dev","sha":"b0"},"head":{"ref":"feat/x","sha":%q}}}`,
			action, repoName, login, head))
	}
	deliver := func(event, delivery string, body []byte) error {
		return d.HandleWebhook(ctx, app.WebhookKey, event, delivery, signWebhook("whsec", body), body)
	}

	// One delivery fans out to both sources.
	require.NoError(t, deliver("pull_request", "d1", payload("opened", "mkucharz", "owner/repo", currentHead)))
	require.Equal(t, 1, countSourceRuns(t, db, agent1))
	require.Equal(t, 1, countSourceRuns(t, db, agent2))

	// Closing cancels both claims.
	require.NoError(t, deliver("pull_request", "d2", payload("closed", "mkucharz", "owner/repo", currentHead)))
	for _, sourceID := range []uuid.UUID{source1, source2} {
		claim, err := repo.FindSourceEventClaim(ctx, projectID, sourceID, "owner/repo#7", currentHead)
		require.NoError(t, err)
		require.Equal(t, SourceClaimCancelled, claim.State)
	}

	// Repo allowlist and App-authored PRs are ignored.
	require.NoError(t, deliver("pull_request", "d3", payload("opened", "mkucharz", "other/repo", currentHead)))
	require.NoError(t, deliver("pull_request", "d4", payload("opened", "memory-reviewer[bot]", "owner/repo", currentHead)))
	require.Equal(t, 1, countSourceRuns(t, db, agent1))

	// Repository deltas update every source.
	delta := []byte(`{"action":"added","installation":{"id":42},"repository":{"full_name":"owner/repo"},"repositories_added":[{"full_name":"owner/other"}],"repositories_removed":[]}`)
	require.NoError(t, deliver("installation_repositories", "d5", delta))
	for _, sourceID := range []uuid.UUID{source1, source2} {
		src, err := repo.FindSourceByID(ctx, sourceID)
		require.NoError(t, err)
		require.Contains(t, sourceRepoList(src.Config), "owner/other")
	}

	// Uninstall disables both sources.
	require.NoError(t, deliver("installation", "d6", []byte(`{"action":"deleted","installation":{"id":42}}`)))
	remaining, err := repo.ListSourcesByAppAndInstallation(ctx, app.ID, "42")
	require.NoError(t, err)
	require.Empty(t, remaining)
}
