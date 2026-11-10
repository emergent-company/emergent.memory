package githubapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
)

func TestGitHubAppReviewTools(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	// The review tools are run-scoped: tests that call them directly must carry
	// the per-run cache the executor installs, or submit fails closed.
	ctx = mcp.ContextWithNewToolResultCache(ctx)
	db := testdb.SetupTestDBOrFail(t, ctx, "githubapp_tools")
	defer db.Close()

	orgID, projectID, agentID := uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "gh-tools-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "gh-tools-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "gh-review", projectID)
	require.NoError(t, err)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	cipher, err := NewCredentialCipher(key)
	require.NoError(t, err)
	sealedKey, err := cipher.Seal(string(pkcs1PEM(testRSAKey(t))))
	require.NoError(t, err)
	sealedSecret, err := cipher.Seal("whsec")
	require.NoError(t, err)

	repo := NewRepository(db.DB)
	app := &GitHubApp{OrgID: orgID, AppID: 99, Slug: "memory-reviewer", PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret}
	require.NoError(t, repo.CreateApp(ctx, app))
	inst := &Installation{AppPK: app.ID, OrgID: orgID, ProjectID: projectID, InstallationID: 42, Repos: []string{"owner/repo"}, AgentID: agentID, Queue: "security-review", Enabled: true}
	require.NoError(t, repo.CreateInstallation(ctx, inst))

	currentHead := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reviewCalls := 0
	var reviewBody, checkBody, commentBody map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_test", "expires_at": time.Now().Add(time.Hour)})
		case r.URL.Path == "/repos/owner/repo/commits/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "commit": map[string]any{"message": "fix"},
				"files": []map[string]any{{"filename": "a.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@"}},
			})
		case r.URL.Path == "/repos/owner/repo/pulls/7":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "fix", "state": "open",
				"user": map[string]any{"login": "mkucharz", "type": "User"},
				"head": map[string]any{"sha": currentHead, "ref": "feat/x"},
				"base": map[string]any{"ref": "dev"},
			})
		case r.URL.Path == "/repos/owner/repo/pulls/7/reviews":
			reviewCalls++
			_ = json.NewDecoder(r.Body).Decode(&reviewBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/repos/owner/repo/check-runs":
			_ = json.NewDecoder(r.Body).Decode(&checkBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
		case r.URL.Path == "/repos/owner/repo/issues/7/comments":
			_ = json.NewDecoder(r.Body).Decode(&commentBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":3}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	svc := NewService(repo, agents.NewRepository(db.DB), cipher, NewTokenMinter(fake.URL, fake.Client()), &config.Config{}, nil)
	svc.newClient = func(_, token string) *Client { return NewClient(fake.URL, token, fake.Client()) }

	require.True(t, svc.HandlesGitHubAppTool(toolPRDiff))
	require.True(t, svc.HandlesGitHubAppTool(toolReviewSubmit))
	require.False(t, svc.HandlesGitHubAppTool("graph-write"))

	diff, err := svc.ExecuteGitHubAppTool(ctx, projectID.String(), toolPRDiff, map[string]any{"repo": "owner/repo", "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	require.NoError(t, err)
	require.Contains(t, diff.Content[0].Text, "a.go")
	require.Contains(t, diff.Content[0].Text, "@@ -1 +1 @@")

	res, err := svc.ExecuteGitHubAppTool(ctx, projectID.String(), toolReviewSubmit, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"verdict": "MERGE", "summary": "looks good",
	})
	require.NoError(t, err)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", reviewBody["commit_id"])
	require.Equal(t, "APPROVE", reviewBody["event"])
	require.Equal(t, "success", checkBody["conclusion"])
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", checkBody["head_sha"])
	require.Contains(t, commentBody["body"], "Reviewed-SHA: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.Contains(t, commentBody["body"], "Verdict: MERGE")
	require.Equal(t, "MERGE", res.StructuredContent["verdict"])

	// The reviewed SHA must be a full commit id, not a ref or abbreviation.
	_, err = svc.ExecuteGitHubAppTool(ctx, projectID.String(), toolReviewSubmit, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": "abc1234",
		"verdict": "MERGE", "summary": "short sha",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "40 lowercase hex")

	// A moved head fails closed and posts nothing new.
	currentHead = "def5678"
	beforeCalls := reviewCalls
	_, err = svc.ExecuteGitHubAppTool(ctx, projectID.String(), toolReviewSubmit, map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"verdict": "MERGE", "summary": "stale",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "PR head is "+currentHead)
	require.Equal(t, beforeCalls, reviewCalls)

	// No installation for a different repo means no tools/failure.
	_, err = svc.ExecuteGitHubAppTool(ctx, projectID.String(), toolPRDiff, map[string]any{"repo": "other/repo", "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	require.Error(t, err)
}
