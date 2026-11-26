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

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	cryptoutil "github.com/emergent-company/emergent.memory/pkg/crypto"
)

// budgetParkFixture is a project plus the GitHub App credentials a
// budget-parked review lane needs: a sealed App private key, an
// installation-bound source, and a repository allowlist.
type budgetParkFixture struct {
	ctx     context.Context
	db      *testdb.TestDB
	repo    *Repository
	orgID   uuid.UUID
	project uuid.UUID
	key     []byte
}

func newBudgetParkFixture(t *testing.T, suffix string) *budgetParkFixture {
	t.Helper()
	ctx := context.Background()
	db := testutilDB(t, ctx, suffix)
	t.Cleanup(db.Close)

	keyHex, err := cryptoutil.GenerateKey()
	require.NoError(t, err)
	key, err := hex.DecodeString(keyHex)
	require.NoError(t, err)

	orgID, projectID := uuid.New(), uuid.New()
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, suffix+"-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		projectID, orgID, suffix+"-project")
	require.NoError(t, err)

	return &budgetParkFixture{
		ctx:     ctx,
		db:      db,
		repo:    NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil))),
		orgID:   orgID,
		project: projectID,
		key:     key,
	}
}

// addApp seals a fresh RSA key so mintToken can sign an installation token.
func (f *budgetParkFixture) addApp(t *testing.T, appID int64) *SourceApp {
	t.Helper()
	sealedKey, err := SealSourceBlob(f.key, rsaPEM(t))
	require.NoError(t, err)
	sealedSecret, err := SealSourceBlob(f.key, []byte("whsec"))
	require.NoError(t, err)
	app := &SourceApp{
		OrgID: f.orgID, Provider: SourceAppProviderGitHub, AppID: appID,
		Slug:                fmt.Sprintf("budget-bot-%d", appID),
		PrivateKeyEncrypted: sealedKey, WebhookSecretEncrypted: sealedSecret,
	}
	require.NoError(t, f.repo.CreateSourceApp(f.ctx, app))
	return app
}

// addSource binds an installation to a project under an App.
func (f *budgetParkFixture) addSource(t *testing.T, projectID uuid.UUID, app *SourceApp, installation string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := f.db.DB.ExecContext(f.ctx,
		`INSERT INTO kb.sources (id, project_id, provider, display_name, app_id, external_account_id, config, status)
			VALUES (?, ?, 'github_app', ?, ?, ?, '{}'::jsonb, ?)`,
		id, projectID, fmt.Sprintf("budget-source-%s", id), app.ID, installation, SourceStatusActive)
	require.NoError(t, err)
	return id
}

// budgetParkCheck is one recorded POST /repos/{repo}/check-runs request.
type budgetParkCheck struct {
	Path string
	Body map[string]any
}

// budgetParkGitHub is a fake GitHub API that records the installation tokens it
// mints and the check-run bodies it receives, so a test can assert exactly what
// the publish call site sent over the wire.
type budgetParkGitHub struct {
	server *httptest.Server
	url    string

	mu     sync.Mutex
	mints  []int64
	checks []budgetParkCheck
}

func newBudgetParkGitHub(t *testing.T) *budgetParkGitHub {
	t.Helper()
	api := &budgetParkGitHub{}
	api.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/app/installations/") &&
			strings.HasSuffix(r.URL.Path, "/access_tokens"):
			var installation int64
			_, _ = fmt.Sscanf(r.URL.Path, "/app/installations/%d/access_tokens", &installation)
			api.mints = append(api.mints, installation)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "ghs_budget_park", "expires_at": time.Now().Add(time.Hour),
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/check-runs"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.checks = append(api.checks, budgetParkCheck{Path: r.URL.Path, Body: body})
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(api.server.Close)
	api.url = api.server.URL
	return api
}

func (a *budgetParkGitHub) snapshotChecks() []budgetParkCheck {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]budgetParkCheck(nil), a.checks...)
}

func (a *budgetParkGitHub) snapshotMints() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]int64(nil), a.mints...)
}

// newDispatcher wires a dispatcher to the fake API and the fixture's sealed App
// key, mirroring the production constructor without config plumbing.
func (f *budgetParkFixture) newDispatcher(t *testing.T, api *budgetParkGitHub) *SourceEventDispatcher {
	t.Helper()
	return &SourceEventDispatcher{
		repo:       f.repo,
		key:        f.key,
		apiBaseURL: api.url,
		minter:     githubapp.NewTokenMinter(api.url, api.server.Client()),
		newClient: func(_, token string) *githubapp.Client {
			return githubapp.NewClient(api.url, token, api.server.Client())
		},
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestPublishReviewLaneBudgetUnavailableSendsPinnedCheck is the call-site
// regression for #1036/#1122. reviewLaneBudgetCheck and the head extraction
// have their own unit tests, but nothing asserted what
// publishReviewLaneBudgetUnavailable actually sends: an edit that inlined a
// passing conclusion, dropped the head binding, or lost the `reviewed` name
// would keep the old tests green while opening the merge gate. This drives the
// real call site against a fake GitHub API and pins the wire payload.
func TestPublishReviewLaneBudgetUnavailableSendsPinnedCheck(t *testing.T) {
	f := newBudgetParkFixture(t, "budget_park_publish")
	app := f.addApp(t, 99)
	srcID := f.addSource(t, f.project, app, "42")
	api := newBudgetParkGitHub(t)
	d := f.newDispatcher(t, api)

	const agentName = "memory-reviewer"
	laneHead := strings.Repeat("1", 40)
	// The fake serves a different PR head; if the call site ever re-read the
	// pull request instead of binding the lane's head, this would surface.
	ev := agents.WorkBudgetParkedEvent{
		ProjectID:   f.project.String(),
		CanonicalID: uuid.NewString(),
		ObjectType:  "GitHubReviewLane",
		AgentName:   agentName,
		Reason:      "project " + f.project.String() + " is over budget_usd",
	}
	target := reviewLaneBudgetTarget{Repo: "owner/repo", HeadSHA: laneHead, SourceID: srcID.String()}

	d.publishReviewLaneBudgetUnavailable(f.ctx, ev, target)

	checks := api.snapshotChecks()
	require.Len(t, checks, 1, "the publish call site must post exactly one check run")
	check := checks[0]
	require.Equal(t, "/repos/owner/repo/check-runs", check.Path)
	require.Equal(t, "reviewed", check.Body["name"], "the required merge-gate context is named `reviewed`")
	require.Equal(t, "completed", check.Body["status"])
	require.Equal(t, laneHead, check.Body["head_sha"],
		"the check must bind the lane's own head, not the PR's live head")

	// The conclusion is the whole merge-gate point: a required check with a
	// passing conclusion makes an unreviewed head mergeable. Pin it against the
	// helper so inlining a literal at the call site cannot drift.
	conclusion, title, summary := reviewLaneBudgetCheck(agentName)
	require.Equal(t, "failure", conclusion)
	require.Equal(t, conclusion, check.Body["conclusion"])
	for _, passing := range []string{"neutral", "success", "skipped"} {
		require.NotEqual(t, passing, check.Body["conclusion"], "a required check must not read as a pass")
	}
	output, ok := check.Body["output"].(map[string]any)
	require.True(t, ok, "check output = %#v", check.Body["output"])
	require.Equal(t, title, output["title"])
	require.Equal(t, summary, output["summary"])
	require.Contains(t, output["summary"], agentName)
	require.Contains(t, output["summary"], "budget_usd", "the summary must name the escape hatch")
	require.NotContains(t, output["summary"], f.project.String(),
		"the check summary is repo-visible; it must not carry the project UUID")

	require.Equal(t, []int64{42}, api.snapshotMints(), "the lane's source installation must mint the token")
}

// TestPublishReviewLaneBudgetUnavailableRefusesCrossProjectSource covers the
// project guard: the lane names its source, but a park for project A must never
// use project B's App installation to paint a check on another project's PR.
func TestPublishReviewLaneBudgetUnavailableRefusesCrossProjectSource(t *testing.T) {
	f := newBudgetParkFixture(t, "budget_park_cross_project")
	app := f.addApp(t, 100)
	otherProject := uuid.New()
	_, err := f.db.DB.ExecContext(f.ctx,
		`INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`,
		otherProject, f.orgID, "budget-park-other-project")
	require.NoError(t, err)
	foreignSource := f.addSource(t, otherProject, app, "77")

	api := newBudgetParkGitHub(t)
	d := f.newDispatcher(t, api)

	ev := agents.WorkBudgetParkedEvent{
		ProjectID:   f.project.String(),
		CanonicalID: uuid.NewString(),
		ObjectType:  "GitHubReviewLane",
		AgentName:   "memory-reviewer",
	}
	target := reviewLaneBudgetTarget{
		Repo: "owner/repo", HeadSHA: strings.Repeat("2", 40), SourceID: foreignSource.String(),
	}
	d.publishReviewLaneBudgetUnavailable(f.ctx, ev, target)

	require.Empty(t, api.snapshotChecks(), "a foreign project's source must not publish a check")
	require.Empty(t, api.snapshotMints(), "a foreign project's installation must not mint a token")
}

// TestOnWorkBudgetParkedBindsTheLaneHead drives the listener entry point: the
// target must come from ev.Head (the lane's repo, head and source), and the
// payload the background publish sends must stay head-bound and failing.
func TestOnWorkBudgetParkedBindsTheLaneHead(t *testing.T) {
	f := newBudgetParkFixture(t, "budget_park_listener")
	app := f.addApp(t, 101)
	srcID := f.addSource(t, f.project, app, "55")
	api := newBudgetParkGitHub(t)
	d := f.newDispatcher(t, api)

	laneHead := strings.Repeat("3", 40)
	d.OnWorkBudgetParked(f.ctx, agents.WorkBudgetParkedEvent{
		ProjectID:   f.project.String(),
		CanonicalID: uuid.NewString(),
		ObjectType:  "GitHubReviewLane",
		AgentName:   "memory-reviewer",
		Head: &graph.WorkObjectHead{
			Type: "GitHubReviewLane",
			Properties: map[string]any{
				"repo":      "owner/repo",
				"head_sha":  laneHead,
				"source_id": srcID.String(),
			},
		},
	})

	require.Eventually(t, func() bool { return len(api.snapshotChecks()) == 1 }, 5*time.Second, 20*time.Millisecond,
		"the listener must publish the head-bound check")
	check := api.snapshotChecks()[0]
	require.Equal(t, "reviewed", check.Body["name"])
	require.Equal(t, laneHead, check.Body["head_sha"], "the check must use the lane head from ev.Head")
	require.Equal(t, "failure", check.Body["conclusion"])
}
