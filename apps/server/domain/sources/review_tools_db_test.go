package sources

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	cryptoutil "github.com/emergent-company/emergent.memory/pkg/crypto"
)

// reviewToolsFixture is one project with an org-scoped App credential and the
// installation-bound github_app sources under test.
type reviewToolsFixture struct {
	ctx      context.Context
	db       *testdb.TestDB
	repo     *Repository
	orgID    uuid.UUID
	project  uuid.UUID
	keyHex   string
	probe    *Repository
	handler  *ReviewToolHandler
	apiCalls *reviewToolsAPI
}

func newReviewToolsFixture(t *testing.T, suffix string) *reviewToolsFixture {
	t.Helper()
	baseCtx := context.Background()
	db := testdb.SetupTestDBOrFail(t, baseCtx, suffix)
	t.Cleanup(db.Close)
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID := uuid.New(), uuid.New()
	_, err := db.DB.ExecContext(baseCtx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, suffix+"-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(baseCtx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, suffix+"-project")
	require.NoError(t, err)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)

	// The handler's repo read runs as a non-superuser, non-BYPASSRLS role so the
	// kb.sources read is exercised under row security, not as the schema owner.
	role := createRLSProbeRole(t, baseCtx, db.DB, "tools")
	probe := NewRepository(probeBunDB(t, baseCtx, db, role), slog.New(slog.NewTextHandler(io.Discard, nil)))

	return &reviewToolsFixture{
		ctx:     mcp.ContextWithNewToolResultCache(baseCtx),
		db:      db,
		repo:    repo,
		orgID:   orgID,
		project: projectID,
		keyHex:  keyHex,
		probe:   probe,
	}
}

// addApp seals a fresh RSA key into a kb.source_apps row.
func (f *reviewToolsFixture) addApp(t *testing.T, appID int64, slug string) *SourceApp {
	t.Helper()
	keyBytes, err := hex.DecodeString(f.keyHex)
	require.NoError(t, err)
	sealedKey, err := SealSourceBlob(keyBytes, rsaPEM(t))
	require.NoError(t, err)
	sealedSecret, err := SealSourceBlob(keyBytes, []byte("whsec"))
	require.NoError(t, err)
	app := &SourceApp{
		OrgID: f.orgID, Provider: SourceAppProviderGitHub, AppID: appID, Slug: slug,
		PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret,
	}
	require.NoError(t, f.repo.CreateSourceApp(f.ctx, app))
	return app
}

// addSource inserts a github_app source with an explicit repo allowlist/status.
func (f *reviewToolsFixture) addSource(t *testing.T, projectID uuid.UUID, app *SourceApp, name, installation string, repos []string, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	cfg, err := json.Marshal(map[string]any{"repos": repos})
	require.NoError(t, err)
	_, err = f.db.DB.ExecContext(f.ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
		VALUES (?, ?, 'github_app', ?, ?, ?, ?::jsonb, ?)`,
		id, projectID, name, app.ID, installation, string(cfg), status)
	require.NoError(t, err)
	return id
}

// buildHandler wires the real handler to a fake GitHub API. Both the minter and
// the client are pointed at the fake; the source read still runs on the probe
// role.
func (f *reviewToolsFixture) buildHandler(t *testing.T, api *reviewToolsAPI) {
	t.Helper()
	h := NewReviewToolHandler(f.probe, &config.Config{
		LLMProvider:  config.LLMProviderConfig{EncryptionKey: f.keyHex},
		GitHubSource: config.GitHubSourceConfig{APIBaseURL: api.url},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.minter = githubapp.NewTokenMinter(api.url, api.server.Client())
	h.newClient = func(_, token string) *githubapp.Client {
		return githubapp.NewClient(api.url, token, api.server.Client())
	}
	f.handler = h
	f.apiCalls = api
}

// reviewToolsAPI is a fake GitHub API recording token mints per installation and
// the review/check/comment bodies the merge guard depends on.
type reviewToolsAPI struct {
	server *httptest.Server
	url    string

	mu          sync.Mutex
	currentHead string
	tokenMints  map[int64]int
	reviews     int
	checks      int
	comments    int
	reviewBody  map[string]any
	checkBody   map[string]any
	commentBody map[string]any
}

func newReviewToolsAPI(t *testing.T) *reviewToolsAPI {
	t.Helper()
	api := &reviewToolsAPI{
		currentHead: strings.Repeat("a", 40),
		tokenMints:  map[int64]int{},
	}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/app/installations/") && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			var installation int64
			_, _ = fmt.Sscanf(r.URL.Path, "/app/installations/%d/access_tokens", &installation)
			api.tokenMints[installation]++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_test", "expires_at": time.Now().Add(time.Hour)})
		case strings.Contains(r.URL.Path, "/commits/"):
			sha := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sha": sha, "commit": map[string]any{"message": "fix"},
				"files": []map[string]any{{"filename": "a.go", "status": "modified", "additions": 1, "deletions": 0, "patch": "@@ -1 +1 @@"}},
			})
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 7, "title": "fix", "state": "open",
				"user": map[string]any{"login": "mkucharz", "type": "User"},
				"head": map[string]any{"sha": api.currentHead, "ref": "feat/x"},
				"base": map[string]any{"ref": "dev"},
			})
		case strings.HasSuffix(r.URL.Path, "/pulls/7/reviews"):
			api.reviews++
			_ = json.NewDecoder(r.Body).Decode(&api.reviewBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			api.checks++
			_ = json.NewDecoder(r.Body).Decode(&api.checkBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":2}`))
		case strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
			api.comments++
			_ = json.NewDecoder(r.Body).Decode(&api.commentBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":3}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.server.Close)
	api.url = api.server.URL
	return api
}

func (a *reviewToolsAPI) setHead(sha string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentHead = sha
}

func (a *reviewToolsAPI) mintCount(installation int64) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tokenMints[installation]
}

func (a *reviewToolsAPI) snapshot() (reviews, checks, comments int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reviews, a.checks, a.comments
}

func reviewSubmitArgs(head, verdict, summary string) map[string]any {
	return map[string]any{
		"repo": "owner/repo", "number": float64(7), "head_sha": head,
		"verdict": verdict, "summary": summary,
	}
}

func TestReviewToolsResolveSourceAndSubmit(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	f := newReviewToolsFixture(t, "review_tools")
	app := f.addApp(t, 99, "memory-reviewer")
	f.addSource(t, f.project, app, "bot", "42", []string{"Owner/Repo"}, SourceStatusActive)

	api := newReviewToolsAPI(t)
	f.buildHandler(t, api)
	head := strings.Repeat("a", 40)

	require.True(t, f.handler.HandlesGitHubAppTool(toolPRDiff))
	require.True(t, f.handler.HandlesGitHubAppTool(toolReviewSubmit))
	require.False(t, f.handler.HandlesGitHubAppTool("graph-write"))

	// Resolves the allowlisted repo case-insensitively through the source and
	// reads a PR diff with the installation token minted for that source.
	diff, err := f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolPRDiff, map[string]any{
		"repo": "owner/repo", "head_sha": head,
	})
	require.NoError(t, err)
	require.Contains(t, diff.Content[0].Text, "a.go")
	require.Contains(t, diff.Content[0].Text, "@@ -1 +1 @@")
	require.Equal(t, 1, api.mintCount(42), "the source's installation must mint the token")
	require.Equal(t, head, diff.StructuredContent["sha"])

	// The merge-gate marker and check title are byte-exact: post-verdict.sh and
	// the merge guard parse them.
	res, err := f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolReviewSubmit,
		reviewSubmitArgs(head, "MERGE", "looks good"))
	require.NoError(t, err)
	require.Equal(t, head, api.reviewBody["commit_id"])
	require.Equal(t, "APPROVE", api.reviewBody["event"])
	require.Equal(t, "success", api.checkBody["conclusion"])
	require.Equal(t, head, api.checkBody["head_sha"])
	require.Equal(t, "reviewed: approved", api.checkBody["output"].(map[string]any)["title"])
	require.Equal(t, "Reviewed-SHA: "+head+"\nVerdict: MERGE\n\nlooks good", api.commentBody["body"])
	require.Equal(t, "MERGE", res.StructuredContent["verdict"])

	// The changes-requested title is pinned equally tightly.
	_, err = f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolReviewSubmit,
		reviewSubmitArgs(head, "CHANGES_REQUESTED", "needs work"))
	require.NoError(t, err)
	require.Equal(t, "failure", api.checkBody["conclusion"])
	require.Equal(t, "reviewed: changes requested", api.checkBody["output"].(map[string]any)["title"])
	require.Equal(t, "Reviewed-SHA: "+head+"\nVerdict: CHANGES_REQUESTED\n\nneeds work", api.commentBody["body"])

	// A fresh run has no evidence: the verdict is refused before any review.
	freshCtx := mcp.ContextWithNewToolResultCache(context.Background())
	beforeReviews, _, beforeComments := api.snapshot()
	_, err = f.handler.ExecuteGitHubAppTool(freshCtx, f.project.String(), toolReviewSubmit,
		reviewSubmitArgs(head, "MERGE", "approved without reading"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "github-pr-diff")
	afterReviews, _, afterComments := api.snapshot()
	require.Equal(t, beforeReviews, afterReviews, "no review may be posted without same-run evidence")
	require.Equal(t, beforeComments, afterComments)

	// A moved head fails closed and posts nothing new.
	api.setHead(strings.Repeat("b", 40))
	beforeReviews, beforeChecks, beforeComments := api.snapshot()
	_, err = f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolReviewSubmit,
		reviewSubmitArgs(head, "MERGE", "stale"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "PR head is "+strings.Repeat("b", 40))
	afterReviews, afterChecks, afterComments := api.snapshot()
	require.Equal(t, beforeReviews, afterReviews)
	require.Equal(t, beforeChecks, afterChecks)
	require.Equal(t, beforeComments, afterComments)

	// The reviewed SHA must be a full commit id, not a ref or abbreviation.
	_, err = f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolReviewSubmit,
		reviewSubmitArgs("abc1234", "MERGE", "short sha"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "40 lowercase hex")
}

func TestReviewToolsRefuseOutsideAllowlistDisabledAndBadInstallation(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	f := newReviewToolsFixture(t, "review_tools_refuse")
	app := f.addApp(t, 100, "bot")
	f.addSource(t, f.project, app, "allowed", "42", []string{"owner/repo"}, SourceStatusActive)
	f.addSource(t, f.project, app, "disabled-bot", "43", []string{"owner/disabled"}, SourceStatusDisabled)
	f.addSource(t, f.project, app, "bad-id-bot", "not-a-number", []string{"owner/badid"}, SourceStatusActive)

	api := newReviewToolsAPI(t)
	f.buildHandler(t, api)
	head := strings.Repeat("a", 40)
	call := func(repo string) error {
		_, err := f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolPRDiff,
			map[string]any{"repo": repo, "head_sha": head})
		return err
	}

	// Outside the allowlist: refused and the error names the repo.
	err := call("owner/outside")
	require.Error(t, err)
	require.Contains(t, err.Error(), "owner/outside")

	// A disabled source is not a candidate, so its repo is refused too.
	err = call("owner/disabled")
	require.Error(t, err)
	require.Contains(t, err.Error(), "owner/disabled")

	// A non-numeric external_account_id fails closed with a clear error rather
	// than minting for installation 0.
	err = call("owner/badid")
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-numeric installation id")
	require.Contains(t, err.Error(), "not-a-number")

	require.Zero(t, api.mintCount(0))
	require.Zero(t, api.mintCount(42))
	require.Zero(t, api.mintCount(43))
}

func TestReviewToolsTwoSourcesTwoApps(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	f := newReviewToolsFixture(t, "review_tools_two_apps")
	appA := f.addApp(t, 201, "bot-a")
	appB := f.addApp(t, 202, "bot-b")
	f.addSource(t, f.project, appA, "bot-a", "42", []string{"owner/repo"}, SourceStatusActive)
	f.addSource(t, f.project, appB, "bot-b", "43", []string{"other/repo"}, SourceStatusActive)

	// A different project's source must not be reachable by project id.
	otherProject := uuid.New()
	_, err := f.db.DB.ExecContext(f.ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, otherProject, f.orgID, "other-project")
	require.NoError(t, err)
	f.addSource(t, otherProject, appB, "other-project-bot", "44", []string{"other-project/repo"}, SourceStatusActive)

	api := newReviewToolsAPI(t)
	f.buildHandler(t, api)
	head := strings.Repeat("a", 40)
	diff := func(projectID, repo string) (*mcp.ToolResult, error) {
		return f.handler.ExecuteGitHubAppTool(f.ctx, projectID, toolPRDiff, map[string]any{"repo": repo, "head_sha": head})
	}

	// Each repo resolves through its own source and installation.
	_, err = diff(f.project.String(), "owner/repo")
	require.NoError(t, err)
	_, err = diff(f.project.String(), "other/repo")
	require.NoError(t, err)
	require.Equal(t, 1, api.mintCount(42))
	require.Equal(t, 1, api.mintCount(43))
	require.Zero(t, api.mintCount(44))

	// A repo allowlisted only in another project's source is refused rather than
	// borrowing that installation.
	_, err = diff(f.project.String(), "other-project/repo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "other-project/repo")
	require.Zero(t, api.mintCount(44), "another project's installation must never be used")
}

// TestReviewToolsRunSourceIDWins pins the spec's two-Apps-on-one-repo scenario:
// each run's TriggerMetadata sourceId selects its own installation even though
// the repo is ambiguous by allowlist, and a bad sourceId fails closed instead of
// falling back to a source that could serve the repo.
func TestReviewToolsRunSourceIDWins(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	f := newReviewToolsFixture(t, "review_tools_source_id")
	appA := f.addApp(t, 401, "bot-a")
	appB := f.addApp(t, 402, "bot-b")
	srcA := f.addSource(t, f.project, appA, "bot-a", "42", []string{"owner/repo"}, SourceStatusActive)
	srcB := f.addSource(t, f.project, appB, "bot-b", "43", []string{"owner/repo"}, SourceStatusActive)
	srcOtherRepo := f.addSource(t, f.project, appA, "other-bot", "45", []string{"other/repo"}, SourceStatusActive)
	srcDisabled := f.addSource(t, f.project, appB, "disabled-bot", "46", []string{"owner/repo"}, SourceStatusDisabled)

	otherProject := uuid.New()
	_, err := f.db.DB.ExecContext(f.ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		otherProject, f.orgID, "source-id-other-project")
	require.NoError(t, err)
	srcOtherProject := f.addSource(t, otherProject, appA, "other-project-bot", "44", []string{"owner/repo"}, SourceStatusActive)

	api := newReviewToolsAPI(t)
	f.buildHandler(t, api)
	head := strings.Repeat("a", 40)
	call := func(ctx context.Context) error {
		_, err := f.handler.ExecuteGitHubAppTool(ctx, f.project.String(), toolPRDiff,
			map[string]any{"repo": "owner/repo", "head_sha": head})
		return err
	}

	// Run fired by source A mints installation 42; run fired by source B mints
	// installation 43. The repo allowlist alone would be ambiguous here.
	require.NoError(t, call(mcp.ContextWithRunSourceID(f.ctx, srcA.String())))
	require.Equal(t, 1, api.mintCount(42), "source A must mint for installation 42")
	require.Zero(t, api.mintCount(43))
	require.NoError(t, call(mcp.ContextWithRunSourceID(f.ctx, srcB.String())))
	require.Equal(t, 1, api.mintCount(42))
	require.Equal(t, 1, api.mintCount(43), "source B must mint for installation 43")

	// No source id: the same two sources still refuse as ambiguous, naming both.
	before42, before43 := api.mintCount(42), api.mintCount(43)
	err = call(f.ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to guess")
	require.Contains(t, err.Error(), srcA.String())
	require.Contains(t, err.Error(), srcB.String())
	require.Equal(t, before42, api.mintCount(42))
	require.Equal(t, before43, api.mintCount(43))

	// A source id from another project is refused inside this project.
	err = call(mcp.ContextWithRunSourceID(f.ctx, srcOtherProject.String()))
	require.Error(t, err)
	require.Contains(t, err.Error(), srcOtherProject.String())
	require.Contains(t, err.Error(), "not a source in this project")
	require.Zero(t, api.mintCount(44), "another project's installation must not mint")
	require.Equal(t, before42, api.mintCount(42))

	// A source id whose allowlist does not cover the repo is refused; it must not
	// fall back to the sources that do cover it.
	err = call(mcp.ContextWithRunSourceID(f.ctx, srcOtherRepo.String()))
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not allow repo owner/repo")
	require.Zero(t, api.mintCount(45))
	require.Equal(t, before42, api.mintCount(42))
	require.Equal(t, before43, api.mintCount(43))

	// A disabled source id is refused.
	err = call(mcp.ContextWithRunSourceID(f.ctx, srcDisabled.String()))
	require.Error(t, err)
	require.Contains(t, err.Error(), "disabled")
	require.Zero(t, api.mintCount(46), "a disabled source must not mint")
}

func TestReviewToolsAmbiguousSourceFailsClosed(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	f := newReviewToolsFixture(t, "review_tools_ambiguous")
	appA := f.addApp(t, 301, "bot-a")
	appB := f.addApp(t, 302, "bot-b")
	srcA := f.addSource(t, f.project, appA, "bot-a", "42", []string{"owner/repo"}, SourceStatusActive)
	srcB := f.addSource(t, f.project, appB, "bot-b", "43", []string{"owner/repo"}, SourceStatusActive)

	api := newReviewToolsAPI(t)
	f.buildHandler(t, api)
	head := strings.Repeat("a", 40)

	_, err := f.handler.ExecuteGitHubAppTool(f.ctx, f.project.String(), toolPRDiff,
		map[string]any{"repo": "owner/repo", "head_sha": head})
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to guess")
	require.Contains(t, err.Error(), srcA.String())
	require.Contains(t, err.Error(), srcB.String())
	require.Contains(t, err.Error(), "bot-a")
	require.Contains(t, err.Error(), "bot-b")
	require.Zero(t, api.mintCount(42))
	require.Zero(t, api.mintCount(43))
}
