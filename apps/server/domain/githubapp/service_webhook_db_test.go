package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

func TestHandleWebhookEnqueuesOneReviewPerHead(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "githubapp_webhook")
	defer db.Close()

	orgID, projectID, agentID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "gh-webhook-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "gh-webhook-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "gh-review", projectID)
	require.NoError(t, err)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	cipher, err := NewCredentialCipher(key)
	require.NoError(t, err)
	sealedKey, err := cipher.Seal(string(pkcs1PEM(testRSAKey(t))))
	require.NoError(t, err)
	const webhookSecret = "whsec"
	sealedSecret, err := cipher.Seal(webhookSecret)
	require.NoError(t, err)

	repo := NewRepository(db.DB)
	app := &GitHubApp{
		OrgID:                  orgID,
		AppID:                  99,
		Slug:                   "memory-reviewer",
		PrivateKeyEncrypted:    sealedKey,
		WebhookSecretEncrypted: sealedSecret,
	}
	require.NoError(t, repo.CreateApp(ctx, app))
	inst := &Installation{
		AppPK:          app.ID,
		OrgID:          orgID,
		ProjectID:      projectID,
		InstallationID: 42,
		Repos:          []string{"owner/repo"},
		AgentID:        agentID,
		Queue:          "review",
		Enabled:        true,
	}
	require.NoError(t, repo.CreateInstallation(ctx, inst))

	// Fake GitHub: token mint + live PR head per number.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_test", "expires_at": time.Now().Add(time.Hour)})
		case "/repos/owner/repo/pulls/7", "/repos/owner/repo/pulls/8":
			number, head := 7, "h1"
			if r.URL.Path == "/repos/owner/repo/pulls/8" {
				number, head = 8, "h8"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": number, "title": "fix", "state": "open",
				"user": map[string]any{"login": "mkucharz", "type": "User"},
				"head": map[string]any{"sha": head, "ref": "feat/x"},
				"base": map[string]any{"ref": "dev"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	svc := NewService(repo, agents.NewRepository(db.DB), cipher, NewTokenMinter(fake.URL, fake.Client()), &config.Config{}, nil)
	svc.newClient = func(_, token string) *Client { return NewClient(fake.URL, token, fake.Client()) }

	body := func(action, state string, pr int, head, repoName string, draft bool) []byte {
		return []byte(fmt.Sprintf(`{"action":%q,"installation":{"id":42},"repository":{"full_name":%q},"pull_request":{"number":%d,"title":"fix","state":%q,"draft":%t,"user":{"login":"mkucharz","type":"User"},"base":{"ref":"dev","sha":"b0"},"head":{"ref":"feat/x","sha":%q}}}`,
			action, repoName, pr, state, draft, head))
	}
	deliver := func(delivery string, event string, payload []byte) error {
		return svc.HandleWebhook(ctx, event, delivery, signed(webhookSecret, payload), payload)
	}

	require.NoError(t, deliver("d1", "pull_request", body("opened", "open", 7, "h1", "owner/repo", false)))
	claim := findClaim(t, db, 42, "owner/repo", 7, "h1")
	require.Equal(t, ClaimRunning, claim.State)
	require.NotNil(t, claim.RunID)

	var run agents.AgentRun
	require.NoError(t, db.DB.NewSelect().Model(&run).Where("id = ?", claim.RunID.String()).Scan(ctx))
	require.Equal(t, "pull_request", run.TriggerMetadata["subjectType"])
	require.Equal(t, "owner/repo#7", run.TriggerMetadata["subjectRef"])
	require.Equal(t, "h1", run.TriggerMetadata["headSha"])
	require.Equal(t, "d1", run.TriggerMetadata["deliveryId"])
	require.True(t, run.TrustedInternal, "the HMAC-verified ingress enqueues a trusted in-process review run")

	// Duplicate delivery is a no-op.
	require.NoError(t, deliver("d1", "pull_request", body("opened", "open", 7, "h1", "owner/repo", false)))
	require.Equal(t, 1, countRuns(t, db, agentID))

	// An out-of-order delivery for an older head is ignored.
	require.NoError(t, deliver("d2", "pull_request", body("synchronize", "open", 7, "old", "owner/repo", false)))
	require.Equal(t, ClaimRunning, findClaim(t, db, 42, "owner/repo", 7, "h1").State)
	require.Equal(t, 1, countRuns(t, db, agentID))

	// A repo outside the allowlist is ignored.
	require.NoError(t, deliver("d3", "pull_request", body("opened", "open", 7, "h1", "other/repo", false)))
	require.Equal(t, 1, countRuns(t, db, agentID))

	// Invalid signature and unknown installation fail closed.
	require.ErrorIs(t, svc.HandleWebhook(ctx, "pull_request", "d-bad", "sha256=deadbeef",
		body("opened", "open", 7, "h1", "owner/repo", false)), ErrInvalidSignature)
	unknown := []byte(`{"action":"opened","installation":{"id":999},"repository":{"full_name":"owner/repo"}}`)
	require.ErrorIs(t, svc.HandleWebhook(ctx, "pull_request", "d-unknown", signed(webhookSecret, unknown), unknown), ErrUnknownInstallation)

	// Converted-to-draft cancels the active claim for a second PR.
	require.NoError(t, deliver("d5", "pull_request", body("opened", "open", 8, "h8", "owner/repo", false)))
	require.NoError(t, deliver("d6", "pull_request", body("converted_to_draft", "open", 8, "h8", "owner/repo", true)))
	require.Equal(t, ClaimCancelled, findClaim(t, db, 42, "owner/repo", 8, "h8").State)

	// Closed cancels the first claim.
	require.NoError(t, deliver("d7", "pull_request", body("closed", "closed", 7, "h1", "owner/repo", false)))
	require.Equal(t, ClaimCancelled, findClaim(t, db, 42, "owner/repo", 7, "h1").State)

	// installation_repositories deltas are top-level, not nested.
	delta := []byte(`{"action":"added","installation":{"id":42},"repository":{"full_name":"owner/repo"},"repositories_added":[{"full_name":"owner/other"}],"repositories_removed":[]}`)
	require.NoError(t, deliver("d8", "installation_repositories", delta))
	updated, err := repo.FindEnabledInstallationByInstallationID(ctx, 42)
	require.NoError(t, err)
	require.Contains(t, updated.Repos, "owner/other")

	// installation.deleted disables the installation.
	deleted := []byte(`{"action":"deleted","installation":{"id":42}}`)
	require.NoError(t, deliver("d9", "installation", deleted))
	_, err = repo.FindEnabledInstallationByInstallationID(ctx, 42)
	require.Error(t, err)
}

func findClaim(t *testing.T, db *testdb.TestDB, installationID int64, repo string, pr int, head string) *ReviewClaim {
	t.Helper()
	claim := new(ReviewClaim)
	require.NoError(t, db.DB.NewSelect().Model(claim).
		Where("installation_id = ?", installationID).
		Where("repo = ?", repo).
		Where("pull_request = ?", pr).
		Where("head_sha = ?", head).
		Scan(context.Background()))
	return claim
}

func countRuns(t *testing.T, db *testdb.TestDB, agentID uuid.UUID) int {
	t.Helper()
	n, err := db.DB.NewSelect().Table("kb.agent_runs").Where("agent_id = ?", agentID).Count(context.Background())
	require.NoError(t, err)
	return n
}
