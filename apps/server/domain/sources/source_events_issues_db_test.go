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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	cryptoutil "github.com/emergent-company/emergent.memory/pkg/crypto"
)

type fakeIssueWorkStore struct {
	req *graph.CreateGraphObjectRequest
}

func (f *fakeIssueWorkStore) CreateOrUpdate(_ context.Context, _ uuid.UUID, req *graph.CreateGraphObjectRequest, _ *uuid.UUID) (*graph.GraphObjectResponse, bool, error) {
	f.req = req
	return &graph.GraphObjectResponse{CanonicalID: uuid.New()}, true, nil
}

func (f *fakeIssueWorkStore) FindHeadObjectByTypeAndKey(context.Context, string, string, string) (*graph.GraphObject, error) {
	return nil, nil
}

func TestSourceEventDispatcherIssueAutoStart(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutilDB(t, ctx, "source_issues")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	agentsRepo := agents.NewRepository(db.DB)

	orgID, projectID, agentID, sourceID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "issues-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "issues-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "coding-agent", projectID)
	require.NoError(t, err)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	keyBytes, err := hex.DecodeString(keyHex)
	require.NoError(t, err)
	const webhookSecret = "whsec-issues"
	sealedKey, err := SealSourceBlob(keyBytes, testRSAKeyPEM(t))
	require.NoError(t, err)
	sealedSecret, err := SealSourceBlob(keyBytes, []byte(webhookSecret))
	require.NoError(t, err)
	app := &SourceApp{OrgID: orgID, Provider: "github", AppID: 99, Slug: "memory-coder", PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret}
	require.NoError(t, repo.CreateSourceApp(ctx, app))

	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
		VALUES (?, ?, 'github_app', 'issues', ?, '42', '{"repos":["owner/repo"],"assignee_login":"mkucharz"}'::jsonb, 'active')`, sourceID, projectID, app.ID)
	require.NoError(t, err)
	trigger := &SourceTrigger{
		ProjectID: projectID, SourceID: sourceID, EventType: "issues", AgentID: agentID,
		Queue: "coding", EventFilter: map[string]any{}, Enabled: true,
		Invocation: &agents.AgentInvocation{
			Model:      "provider/coding",
			Credential: &agents.AgentCredentialRef{Kind: "github_app", Permissions: map[string]string{"contents": "write"}},
		},
	}
	require.NoError(t, repo.CreateSourceTrigger(ctx, trigger))

	liveLabels := "[]"
	var assignedPath, labelsPath, commentPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test","expires_at":"2099-01-01T00:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/737":
			_, _ = w.Write([]byte(`{"number":737,"title":"fix it","body":"details","state":"open","labels":` + liveLabels + `,"assignees":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/737/assignees":
			assignedPath = r.URL.Path
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/737/labels":
			labelsPath = r.URL.Path
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/737/comments":
			commentPath = r.URL.Path
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
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
	store := &fakeIssueWorkStore{}
	dispatcher.workObjects = store

	body := []byte(`{"action":"opened","installation":{"id":42},"repository":{"full_name":"owner/repo"},"issue":{"number":737,"title":"fix it","body":"details","state":"open","html_url":"https://github.com/owner/repo/issues/737","labels":[{"name":"bug"}],"user":{"login":"mkucharz","type":"User"}}}`)
	require.NoError(t, dispatcher.HandleWebhook(ctx, app.WebhookKey, "issues", "d-issue", signWebhook(webhookSecret, body), body))

	require.NotNil(t, store.req)
	require.Equal(t, "GitHubIssue", store.req.Type)
	require.Equal(t, "coding-agent", *store.req.Assignee)
	require.Equal(t, "ready", *store.req.Status)
	require.Equal(t, trigger.ID.String(), store.req.Properties["source_trigger_id"])
	_, hasInvocation := store.req.Properties["run_invocation"]
	require.False(t, hasInvocation, "invocation must not be trusted from graph properties")

	resolver := NewSourceTriggerInvocationResolver(repo)
	resolved, err := resolver.ResolveSourceTriggerInvocation(ctx, projectID.String(), sourceID.String(), trigger.ID.String())
	require.NoError(t, err)
	require.Equal(t, "provider/coding", resolved.Model)

	ev, err := repo.FindSourceEventByDelivery(ctx, projectID, sourceID, "d-issue")
	require.NoError(t, err)
	require.Equal(t, SourceEventDispatched, ev.State)
	require.Equal(t, "/repos/owner/repo/issues/737/assignees", assignedPath)
	require.Equal(t, "/repos/owner/repo/issues/737/labels", labelsPath)
	require.Equal(t, "/repos/owner/repo/issues/737/comments", commentPath)

	// A peer claim must not create a board item or start work.
	liveLabels = `[{"name":"status: in-progress"},{"name":"manager: peer-lane"}]`
	store.req = nil
	peerBody := []byte(`{"action":"opened","installation":{"id":42},"repository":{"full_name":"owner/repo"},"issue":{"number":737,"title":"fix it","body":"details","state":"open","html_url":"https://github.com/owner/repo/issues/737","labels":[{"name":"status: in-progress"},{"name":"manager: peer-lane"}],"user":{"login":"mkucharz","type":"User"}}}`)
	require.NoError(t, dispatcher.HandleWebhook(ctx, app.WebhookKey, "issues", "d-issue-peer", signWebhook(webhookSecret, peerBody), peerBody))
	require.Nil(t, store.req, "a peer claim must not create or start a Memory work item")
	ev, err = repo.FindSourceEventByDelivery(ctx, projectID, sourceID, "d-issue-peer")
	require.NoError(t, err)
	require.Equal(t, SourceEventSkipped, ev.State)
}

func testRSAKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

type fakePRLinkStore struct {
	issue    *graph.GraphObject
	issueReq *graph.CreateGraphObjectRequest
}

func (f *fakePRLinkStore) CreateOrUpdate(_ context.Context, _ uuid.UUID, req *graph.CreateGraphObjectRequest, _ *uuid.UUID) (*graph.GraphObjectResponse, bool, error) {
	if req.Type == "GitHubIssue" {
		f.issueReq = req
	}
	return &graph.GraphObjectResponse{CanonicalID: uuid.New()}, true, nil
}

func (f *fakePRLinkStore) FindHeadObjectByTypeAndKey(context.Context, string, string, string) (*graph.GraphObject, error) {
	return f.issue, nil
}

func TestEnsureIssuePRLink(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testutilDB(t, ctx, "issue_pr_link")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	orgID, projectID, sourceID, appID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "link-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "link-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.source_apps (id, org_id, provider, app_id, slug, private_key_encrypted, webhook_secret_encrypted)
		VALUES (?, ?, 'github', 99, 'link-app', '\x00'::bytea, '\x00'::bytea)`, appID, orgID)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
		VALUES (?, ?, 'github_app', 'coding', ?, '42', '{"repos":["owner/repo"]}'::jsonb, 'active')`, sourceID, projectID, appID)
	require.NoError(t, err)
	src, err := repo.GetByID(ctx, sourceID, projectID)
	require.NoError(t, err)

	issueCanonical := uuid.New()
	store := &fakePRLinkStore{issue: &graph.GraphObject{CanonicalID: issueCanonical, Type: "GitHubIssue", Status: ptr("in_progress"), Properties: map[string]any{
		"repo": "emergent-company/emergent.memory", "issue_number": 7, "source_id": sourceID.String(), "title": "fix",
	}}}
	d := NewSourceEventDispatcher(repo, nil, &config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.workObjects = store
	p := &githubapp.WebhookPayload{
		Action:     "opened",
		Repository: githubapp.WebhookRepo{FullName: "owner/repo"},
		PullRequest: githubapp.WebhookPullRequest{
			Number: 42, Title: "fix", Body: "Closes #7", State: "open",
			Head: githubapp.WebhookRef{Ref: "fix/7-fix", SHA: strings.Repeat("a", 40)},
		},
	}
	_, err = d.ensureIssuePRLink(ctx, src, p, &githubapp.PullRequestState{State: "open", HeadSHA: strings.Repeat("a", 40)}, "owner/repo#42")
	require.NoError(t, err)
	link, err := repo.FindSourceIssuePRLink(ctx, projectID, "owner/repo", 42)
	require.NoError(t, err)
	require.Equal(t, issueCanonical, link.IssueCanonicalID)
	require.True(t, link.ClosesIssue)
	require.NotNil(t, store.issueReq)
	require.Equal(t, "review", *store.issueReq.Status)
	require.Equal(t, "review", store.issueReq.Properties["status"])
	require.Equal(t, "emergent-company/emergent.memory", store.issueReq.Properties["repo"])

	// A draft conversion must not cancel the issue.
	p.Action = "converted_to_draft"
	p.PullRequest.State = "open"
	_, err = d.ensureIssuePRLink(ctx, src, p, &githubapp.PullRequestState{State: "draft", HeadSHA: strings.Repeat("a", 40)}, "owner/repo#42")
	require.NoError(t, err)
	require.Equal(t, "review", *store.issueReq.Status)
	link, err = repo.FindSourceIssuePRLink(ctx, projectID, "owner/repo", 42)
	require.NoError(t, err)
	require.Equal(t, "draft", link.State)

	// Refs-only is a link but not a closing keyword.
	p.Action = "synchronize"
	p.PullRequest.State = "open"
	p.PullRequest.Body = "Refs #7"
	_, err = d.ensureIssuePRLink(ctx, src, p, &githubapp.PullRequestState{State: "open", HeadSHA: strings.Repeat("a", 40)}, "owner/repo#42")
	require.NoError(t, err)
	link, err = repo.FindSourceIssuePRLink(ctx, projectID, "owner/repo", 42)
	require.NoError(t, err)
	require.False(t, link.ClosesIssue)
}

func ptr(s string) *string { return &s }
