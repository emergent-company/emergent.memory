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
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

func TestHandleWebhookEnqueuesOneReviewPerHead(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutil.SetupTestDBOrFail(t, ctx, "githubapp_webhook")
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
	rsaKey := testRSAKey(t)
	sealedKey, err := cipher.Seal(string(pkcs1PEM(rsaKey)))
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

	// Fake GitHub: token mint + live PR head.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_test", "expires_at": time.Now().Add(time.Hour)})
		case "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "fix", "state": "open",
				"user": map[string]any{"login": "mkucharz", "type": "User"},
				"head": map[string]any{"sha": "h1", "ref": "feat/x"},
				"base": map[string]any{"ref": "dev"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	agentsRepo := agents.NewRepository(db.DB)
	svc := NewService(repo, agentsRepo, cipher, NewTokenMinter(fake.URL, fake.Client()), &config.Config{}, nil)
	svc.newClient = func(_, token string) *Client { return NewClient(fake.URL, token, fake.Client()) }

	body := func(action, head, repoName string) []byte {
		return []byte(fmt.Sprintf(`{"action":%q,"installation":{"id":42},"repository":{"full_name":%q},"pull_request":{"number":7,"title":"fix","state":"open","user":{"login":"mkucharz","type":"User"},"base":{"ref":"dev","sha":"b0"},"head":{"ref":"feat/x","sha":%q}}}`,
			action, repoName, head))
	}
	deliver := func(delivery string, payload []byte) error {
		return svc.HandleWebhook(ctx, "pull_request", delivery, signed(webhookSecret, payload), payload)
	}

	require.NoError(t, deliver("d1", body("opened", "h1", "owner/repo")))
	claim := findClaim(t, db, 42, "owner/repo", 7, "h1")
	require.Equal(t, ClaimRunning, claim.State)
	require.NotNil(t, claim.RunID)

	var run agents.AgentRun
	require.NoError(t, db.DB.NewSelect().Model(&run).Where("id = ?", claim.RunID.String()).Scan(ctx))
	require.Equal(t, "pull_request", run.TriggerMetadata["subjectType"])
	require.Equal(t, "owner/repo#7", run.TriggerMetadata["subjectRef"])
	require.Equal(t, "h1", run.TriggerMetadata["headSha"])

	// Duplicate delivery is a no-op.
	require.NoError(t, deliver("d1", body("opened", "h1", "owner/repo")))
	require.Equal(t, 1, countRuns(t, db, agentID))

	// An out-of-order delivery for an older head is ignored.
	require.NoError(t, deliver("d2", body("synchronize", "old", "owner/repo")))
	require.Equal(t, ClaimRunning, findClaim(t, db, 42, "owner/repo", 7, "h1").State)
	require.Equal(t, 1, countRuns(t, db, agentID))

	// A repo outside the allowlist is ignored.
	require.NoError(t, deliver("d3", body("opened", "h1", "other/repo")))
	require.Equal(t, 1, countRuns(t, db, agentID))

	// Closing the PR cancels the active claim and its run.
	require.NoError(t, deliver("d4", body("closed", "h1", "owner/repo")))
	require.Equal(t, ClaimCancelled, findClaim(t, db, 42, "owner/repo", 7, "h1").State)
}

func findClaim(t *testing.T, db *testutil.TestDB, installationID int64, repo string, pr int, head string) *ReviewClaim {
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

func countRuns(t *testing.T, db *testutil.TestDB, agentID uuid.UUID) int {
	t.Helper()
	n, err := db.DB.NewSelect().Table("kb.agent_runs").Where("agent_id = ?", agentID).Count(context.Background())
	require.NoError(t, err)
	return n
}
