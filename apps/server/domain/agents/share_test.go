package agents

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// =============================================================================
// Fakes
// =============================================================================

type fakeShareRepo struct {
	tokenLink map[string]*AgentShareLink
	linkByID  map[string]*AgentShareLink
	defByID   map[string]*AgentDefinition
	orgByProj map[string]string
	sessions  map[string]*AgentShareSession
	agents    map[string]*Agent

	sessionByRunAndUser *AgentShareSession

	activeSessions int
	activeRuns     int
	approvals      int

	usage *ShareUsageAggregate

	// reserveBudgetErr, when non-nil, is returned by ReserveShareBudget (used to
	// simulate a tripped budget).
	reserveBudgetErr error

	// sessionCapErr / runSlotErr / approvalCapErr are returned by the respective
	// atomic-reservation fakes to simulate a tripped cap.
	sessionCapErr  error
	runSlotErr     error
	approvalCapErr error

	// createdRun records the pre-created run from CreateShareRunIfUnderLimit.
	createdRun *AgentRun

	// recording
	createdACPSession   *ACPSession
	createdShareSession *AgentShareSession
	createdShareLink    *AgentShareLink
	createdAgent        *Agent
	replacedTokenID     string
	incrementedUsage    bool
	incrementMessages   int
	incrementTokens     int64
	incrementCost       float64
	runUsage            *RunTokenUsage
	question            *AgentQuestion
	accessLogs          []accessLogEntry

	// owner-facing project session listing (ListShareSessionsByProject /
	// GetShareSessionByProject).
	shareSessionRows []shareSessionProjectRow
	shareSessionRow  *shareSessionProjectRow
}

type accessLogEntry struct {
	linkID, ref, ip, action string
}

func newFakeShareRepo() *fakeShareRepo {
	return &fakeShareRepo{
		tokenLink: map[string]*AgentShareLink{},
		linkByID:  map[string]*AgentShareLink{},
		defByID:   map[string]*AgentDefinition{},
		orgByProj: map[string]string{},
		sessions:  map[string]*AgentShareSession{},
		agents:    map[string]*Agent{},
	}
}

// --- link CRUD ---
func (f *fakeShareRepo) CreateShareLink(_ context.Context, l *AgentShareLink) error {
	f.createdShareLink = l
	f.linkByID[l.ID] = l
	return nil
}
func (f *fakeShareRepo) ListShareLinksByProject(_ context.Context, _ string) ([]*AgentShareLink, error) {
	out := []*AgentShareLink{}
	for _, l := range f.linkByID {
		out = append(out, l)
	}
	return out, nil
}
func (f *fakeShareRepo) GetShareLinkByID(_ context.Context, id string, _ *string) (*AgentShareLink, error) {
	return f.linkByID[id], nil
}
func (f *fakeShareRepo) GetShareLinkByTokenID(_ context.Context, tokenID string) (*AgentShareLink, error) {
	return f.tokenLink[tokenID], nil
}
func (f *fakeShareRepo) UpdateShareLink(_ context.Context, linkID, projectID, label string, config *ShareLinkConfig, expiresAt *time.Time) (bool, error) {
	l := f.linkByID[linkID]
	if l == nil || l.RevokedAt != nil {
		return false, nil
	}
	l.Label = label
	l.Config = config
	l.ExpiresAt = expiresAt
	return true, nil
}
func (f *fakeShareRepo) RevokeShareLink(_ context.Context, linkID, projectID string) (bool, error) {
	l := f.linkByID[linkID]
	if l == nil || l.RevokedAt != nil {
		return false, nil
	}
	now := time.Now()
	l.RevokedAt = &now
	return true, nil
}
func (f *fakeShareRepo) ReplaceShareLinkToken(_ context.Context, _ bun.Tx, linkID, newTokenID string) error {
	f.replacedTokenID = newTokenID
	if l := f.linkByID[linkID]; l != nil {
		l.APITokenID = newTokenID
	}
	return nil
}
func (f *fakeShareRepo) TouchShareLink(_ context.Context, _ string) error { return nil }

// --- sessions ---
func (f *fakeShareRepo) CreateShareSession(_ context.Context, s *AgentShareSession) error {
	f.createdShareSession = s
	f.sessions[s.ID] = s
	return nil
}
func (f *fakeShareRepo) CreateShareSessionIfUnderCap(_ context.Context, s *AgentShareSession, maxActive int) (bool, error) {
	if f.sessionCapErr != nil {
		return false, f.sessionCapErr
	}
	if maxActive > 0 && f.activeSessions >= maxActive {
		return false, nil
	}
	f.createdShareSession = s
	f.sessions[s.ID] = s
	return true, nil
}
func (f *fakeShareRepo) GetShareSessionByID(_ context.Context, id, linkID, endUserRef string) (*AgentShareSession, error) {
	s := f.sessions[id]
	if s == nil {
		return nil, nil
	}
	if s.ShareLinkID != linkID || s.EndUserRef != endUserRef {
		return nil, nil
	}
	return s, nil
}
func (f *fakeShareRepo) ListShareSessionsByEndUser(_ context.Context, linkID, endUserRef string, _ bool) ([]*AgentShareSession, error) {
	out := []*AgentShareSession{}
	for _, s := range f.sessions {
		if s.ShareLinkID == linkID && s.EndUserRef == endUserRef {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeShareRepo) ListShareSessionsByProject(_ context.Context, _ string) ([]shareSessionProjectRow, error) {
	return f.shareSessionRows, nil
}
func (f *fakeShareRepo) GetShareSessionByProject(_ context.Context, _, _ string) (*shareSessionProjectRow, error) {
	return f.shareSessionRow, nil
}
func (f *fakeShareRepo) CountActiveShareSessions(_ context.Context, _, _ string) (int, error) {
	return f.activeSessions, nil
}
func (f *fakeShareRepo) ArchiveShareSession(_ context.Context, id, _, _ string) (bool, error) {
	s := f.sessions[id]
	if s == nil || s.IsArchived {
		return false, nil
	}
	s.IsArchived = true
	return true, nil
}
func (f *fakeShareRepo) TouchShareSession(_ context.Context, _ string) error { return nil }
func (f *fakeShareRepo) FindShareSessionByRunAndEndUser(_ context.Context, _, _, _ string) (*AgentShareSession, error) {
	return f.sessionByRunAndUser, nil
}
func (f *fakeShareRepo) CountActiveShareRuns(_ context.Context, _ string) (int, error) {
	return f.activeRuns, nil
}
func (f *fakeShareRepo) CreateShareRunIfUnderLimit(_ context.Context, _ string, maxConcurrent int, opts CreateRunOptions) (*AgentRun, error) {
	if f.runSlotErr != nil {
		return nil, f.runSlotErr
	}
	if maxConcurrent > 0 && f.activeRuns >= maxConcurrent {
		return nil, apperror.New(429, "share_busy", "share link is at its concurrent run limit")
	}
	run := newAgentRun(opts)
	run.ID = "pre-run"
	f.createdRun = run
	return run, nil
}
func (f *fakeShareRepo) CountSessionToolApprovals(_ context.Context, _, _ string) (int, error) {
	return f.approvals, nil
}
func (f *fakeShareRepo) ReserveAndDecideShareApproval(_ context.Context, _, _, _, _, _, _ string, maxApprovals int) (bool, error) {
	if f.approvalCapErr != nil {
		return false, f.approvalCapErr
	}
	if maxApprovals > 0 && f.approvals >= maxApprovals {
		return false, nil
	}
	f.approvals++
	return true, nil
}
func (f *fakeShareRepo) ListPendingQuestionsForACPSession(_ context.Context, _ string) ([]*AgentQuestion, error) {
	return nil, nil
}

// --- end users ---
func (f *fakeShareRepo) UpsertShareEndUser(_ context.Context, _ *AgentShareEndUser) error { return nil }
func (f *fakeShareRepo) FindShareEndUser(_ context.Context, _, _ string) (*AgentShareEndUser, error) {
	return nil, nil
}

// --- usage ---
func (f *fakeShareRepo) IncrementShareUsage(_ context.Context, _ string, _ time.Time, messages int, tokens int64, costUSD float64) error {
	f.incrementedUsage = true
	f.incrementMessages = messages
	f.incrementTokens = tokens
	f.incrementCost = costUSD
	return nil
}
func (f *fakeShareRepo) ReserveShareBudget(_ context.Context, _ string, _ time.Time, _ int, _ int64, _ float64, _ int64, _ float64) error {
	return f.reserveBudgetErr
}
func (f *fakeShareRepo) SumShareUsageSince(_ context.Context, _ string, _ time.Time) (*ShareUsageAggregate, error) {
	return f.usage, nil
}
func (f *fakeShareRepo) ListShareUsage(_ context.Context, _ string, _ int) ([]*AgentShareUsage, error) {
	return nil, nil
}

// --- access log + reaper ---
func (f *fakeShareRepo) CreateShareAccessLog(_ context.Context, linkID, endUserRef, ipHash, action string) error {
	f.accessLogs = append(f.accessLogs, accessLogEntry{linkID, endUserRef, ipHash, action})
	return nil
}
func (f *fakeShareRepo) ListPausedShareRuns(_ context.Context) ([]shareReapCandidate, error) {
	return nil, nil
}

// --- shared agent lookups ---
func (f *fakeShareRepo) FindDefinitionByID(_ context.Context, id string, _ *string) (*AgentDefinition, error) {
	return f.defByID[id], nil
}
func (f *fakeShareRepo) GetOrgIDByProjectID(_ context.Context, projectID string) (string, error) {
	return f.orgByProj[projectID], nil
}
func (f *fakeShareRepo) FindByName(_ context.Context, _, name string) (*Agent, error) {
	return f.agents[name], nil
}
func (f *fakeShareRepo) Create(_ context.Context, a *Agent) error {
	f.createdAgent = a
	f.agents[a.Name] = a
	return nil
}
func (f *fakeShareRepo) CreateACPSession(_ context.Context, s *ACPSession) error {
	f.createdACPSession = s
	return nil
}
func (f *fakeShareRepo) FindQuestionByID(_ context.Context, _ string) (*AgentQuestion, error) {
	return f.question, nil
}
func (f *fakeShareRepo) GetRunTokenUsage(_ context.Context, _ string) (*RunTokenUsage, error) {
	return f.runUsage, nil
}
func (f *fakeShareRepo) GetConversationFullHistory(_ context.Context, _ string) ([]*ConversationHistoryItem, error) {
	return nil, nil
}

type shareFakeRunner struct {
	captured ExecuteRequest
	result   *ExecuteResult
	err      error
}

func (f *shareFakeRunner) Execute(_ context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	f.captured = req
	return f.result, f.err
}

type shareFakeResponder struct {
	captured RespondParams
	dto      *AgentQuestionDTO
	err      error
}

func (f *shareFakeResponder) RespondToQuestion(_ context.Context, p RespondParams) (*AgentQuestionDTO, error) {
	f.captured = p
	return f.dto, f.err
}

type shareFakeTokenService struct {
	revokedTokenIDs []string
	revealedKey     string
	role            string
}

func (f *shareFakeTokenService) CreateAgentChatShareToken(_ context.Context, _, _ string, _ *time.Time) (*apitoken.CreateApiTokenResponseDTO, error) {
	return &apitoken.CreateApiTokenResponseDTO{ApiTokenDTO: apitoken.ApiTokenDTO{ID: "tok-1", TokenPrefix: "emt_"}, Token: "emt_test"}, nil
}
func (f *shareFakeTokenService) RevokeEphemeral(_ context.Context, tokenID string) {
	f.revokedTokenIDs = append(f.revokedTokenIDs, tokenID)
}
func (f *shareFakeTokenService) RegenerateWith(_ context.Context, _, _, _ string, _ func(context.Context, bun.Tx, string) error) (*apitoken.CreateApiTokenResponseDTO, error) {
	return nil, nil
}
func (f *shareFakeTokenService) GetByID(_ context.Context, _, _ string) (*apitoken.GetApiTokenResponseDTO, error) {
	return &apitoken.GetApiTokenResponseDTO{ApiTokenDTO: apitoken.ApiTokenDTO{ID: "tok-1"}, Token: f.revealedKey}, nil
}
func (f *shareFakeTokenService) GetUserProjectRole(_ context.Context, _, _ string) (string, error) {
	if f.role == "" {
		return "project_admin", nil
	}
	return f.role, nil
}

// =============================================================================
// Helpers
// =============================================================================

func testBinding(link *AgentShareLink, def *AgentDefinition) *ShareLinkBinding {
	return &ShareLinkBinding{
		Link:       link,
		Definition: def,
		ProjectID:  link.ProjectID,
		OrgID:      "org-1",
		Config:     link.EffectiveConfig(),
	}
}

func testLink(id, projectID, defID, tokenID string) *AgentShareLink {
	cfg := DefaultShareLinkConfig()
	return &AgentShareLink{
		ID:                id,
		ProjectID:         projectID,
		AgentDefinitionID: defID,
		APITokenID:        tokenID,
		Label:             "test link",
		Config:            cfg,
	}
}

func testDefinition(id, projectID, name string) *AgentDefinition {
	return &AgentDefinition{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
	}
}

// =============================================================================
// Tests
// =============================================================================

// Resolver must derive the link strictly from the token — never from scope alone
// or any other input — and reject missing/revoked/expired.
func TestResolveLinkByTokenID_TokenBoundToDifferentLink(t *testing.T) {
	repo := newFakeShareRepo()
	def := testDefinition("def-a", "proj-a", "Agent A")
	linkA := testLink("link-a", "proj-a", "def-a", "token-a")
	linkB := testLink("link-b", "proj-b", "def-a", "token-b")
	repo.tokenLink["token-a"] = linkA
	repo.tokenLink["token-b"] = linkB
	repo.defByID["def-a"] = def
	repo.orgByProj["proj-a"] = "org-a"

	svc := NewShareService(repo, nil, nil, nil, "", nil)

	// Token A resolves to link A, not link B.
	binding, err := svc.ResolveLinkByTokenID(context.Background(), "token-a")
	require.NoError(t, err)
	require.NotNil(t, binding)
	assert.Equal(t, "link-a", binding.Link.ID)
	assert.Equal(t, "proj-a", binding.ProjectID)

	// A token bound to no link is rejected (scope alone never authorizes).
	_, err = svc.ResolveLinkByTokenID(context.Background(), "token-nonexistent")
	require.Error(t, err)

	// A revoked link is rejected (410).
	now := time.Now()
	linkB.RevokedAt = &now
	_, err = svc.ResolveLinkByTokenID(context.Background(), "token-b")
	require.Error(t, err)
}

// The share resolver must ignore client-supplied project/org headers.
func TestRequireShareLink_IgnoresForgedProjectID(t *testing.T) {
	repo := newFakeShareRepo()
	def := testDefinition("def-a", "proj-good", "Agent A")
	link := testLink("link-a", "proj-good", "def-a", "token-a")
	repo.tokenLink["token-a"] = link
	repo.defByID["def-a"] = def
	repo.orgByProj["proj-good"] = "org-good"

	svc := NewShareService(repo, nil, nil, nil, "", nil)
	h := NewShareHandler(svc)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/share/agent", nil)
	req.Header.Set("X-Project-ID", "evil-project")
	req.Header.Set("X-Org-ID", "evil-org")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	user := &auth.AuthUser{ID: "u", APITokenID: "token-a", Scopes: []string{shareAgentChatScope}}
	c.Set(string(auth.UserContextKey), user)

	var gotBinding *ShareLinkBinding
	next := func(c echo.Context) error {
		gotBinding = shareBindingFromContext(c.Request().Context())
		return nil
	}

	require.NoError(t, h.RequireShareLink()(next)(c))
	require.NotNil(t, gotBinding)
	assert.Equal(t, "proj-good", gotBinding.ProjectID, "resolved project must come from the link, not X-Project-ID")
	assert.Equal(t, "org-good", gotBinding.OrgID, "resolved org must come from the link, not X-Org-ID")
	assert.Equal(t, "proj-good", user.ProjectID, "echo user project must be overwritten with link project")
}

// Share runs must never receive the owner's project credentials, and sandbox is
// forced off when the link config disables it.
func TestBuildShareExecuteRequest_SandboxOffNoOwnerCreds(t *testing.T) {
	repo := newFakeShareRepo()
	def := testDefinition("def-a", "proj-a", "Agent A")
	def.SandboxConfig = map[string]any{"image": "owner-image"}
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	binding := testBinding(link, def)

	svc := NewShareService(repo, nil, nil, nil, "", nil)
	session := &AgentShareSession{ID: "sess-1", ACPSessionID: "acp-1", EndUserRef: "ref-1"}

	req, err := svc.buildShareExecuteRequest(context.Background(), binding, session, "hello", nil)
	require.NoError(t, err)

	assert.True(t, req.DisableAuthMint, "share runs must disable the ephemeral auth mint")
	assert.Equal(t, "", req.AuthToken, "share runs must never inject an owner auth token")
	assert.Equal(t, "", req.EphemeralTokenID, "share runs must never inject an ephemeral token")
	assert.Nil(t, req.AgentDefinition.SandboxConfig, "sandbox config must be forced empty when sandbox is disabled")
	assert.Equal(t, "link-a", req.ShareLinkID)
	assert.Equal(t, "", req.UserID, "anonymous share runs must not target notifications")
}

// A deny-listed (dangerous, non-allowlisted) tool is hard-blocked by the share
// allowlist; allowlisting removes it.
func TestComputeShareToolDeny_DefaultDenyAndAllowlist(t *testing.T) {
	cfg := DefaultShareLinkConfig()
	deny := cfg.ComputeShareToolDeny()

	assert.Contains(t, deny, "entity-delete", "dangerous tools are denied by default")
	assert.Contains(t, deny, "schema-migrate-execute", "dangerous tools are denied by default")

	// Allowlist re-enables a dangerous tool.
	cfg.ToolAllowlist = []string{"entity-delete"}
	deny2 := cfg.ComputeShareToolDeny()
	assert.NotContains(t, deny2, "entity-delete", "allowlisted tools are removed from the deny list")
	assert.Contains(t, deny2, "schema-migrate-execute", "non-allowlisted dangerous tools remain denied")
}

// A foreign end_user_ref can never answer another user's question.
func TestRespondToQuestion_ForeignEndUserRef(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	binding := testBinding(link, testDefinition("def-a", "proj-a", "Agent A"))

	alice := "11111111-1111-1111-1111-111111111111"
	bob := "22222222-2222-2222-2222-222222222222"

	// The caller's session is "sess-1" but the run maps to a DIFFERENT session
	// (owned by another end user).
	repo.sessions["sess-1"] = &AgentShareSession{ID: "sess-1", ShareLinkID: "link-a", EndUserRef: alice, ACPSessionID: "acp-1"}
	repo.sessionByRunAndUser = &AgentShareSession{ID: "sess-2", ShareLinkID: "link-a", EndUserRef: bob, ACPSessionID: "acp-1"}

	responder := &shareFakeResponder{}
	svc := NewShareService(repo, nil, nil, responder, "", nil)

	_, err := svc.RespondToQuestion(context.Background(), binding, "sess-1", alice, "q-1", "approve", "", "iphash")
	require.Error(t, err)

	// The responder must have been NOT called.
	assert.Equal(t, "", responder.captured.QuestionID)
}

// A resumed share run leg must record its usage, otherwise a visitor can cycle
// approve/resume to spend above the per-link budget (the initial StreamMessage
// only reconciles usage up to the first pause).
func TestResumeSettled_RecordsResumeUsage(t *testing.T) {
	repo := newFakeShareRepo()
	repo.runUsage = &RunTokenUsage{
		TotalInputTokens:  100,
		TotalOutputTokens: 50,
		EstimatedCostUSD:  0.02,
	}
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	binding := testBinding(link, testDefinition("def-a", "proj-a", "Agent A"))

	svc := NewShareService(repo, nil, nil, nil, "", nil)
	svc.resumeSettled(binding)(&ExecuteResult{RunID: "run-resumed"})

	assert.True(t, repo.incrementedUsage, "resumed leg usage must be recorded")
	assert.Equal(t, 0, repo.incrementMessages, "no message slot reserved for a resume leg")
	assert.Equal(t, int64(150), repo.incrementTokens, "resume tokens must be recorded in full")
	assert.InDelta(t, 0.02, repo.incrementCost, 1e-9, "resume cost must be recorded in full")
}

// RespondToQuestion must wire the OnRunSettled callback so the resumed leg's
// usage is recorded after it settles.
func TestRespondToQuestion_SetsOnRunSettled(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	binding := testBinding(link, testDefinition("def-a", "proj-a", "Agent A"))

	alice := "11111111-1111-1111-1111-111111111111"
	repo.sessions["sess-1"] = &AgentShareSession{ID: "sess-1", ShareLinkID: "link-a", EndUserRef: alice, ACPSessionID: "acp-1"}
	repo.sessionByRunAndUser = repo.sessions["sess-1"]
	repo.question = &AgentQuestion{ID: "q-1", RunID: "run-1", ProjectID: "proj-a", Status: QuestionStatusPending}
	repo.runUsage = &RunTokenUsage{TotalInputTokens: 10, TotalOutputTokens: 5, EstimatedCostUSD: 0.01}

	responder := &shareFakeResponder{}
	svc := NewShareService(repo, nil, nil, responder, "", nil)

	_, err := svc.RespondToQuestion(context.Background(), binding, "sess-1", alice, "q-1", "approve", "", "iphash")
	require.NoError(t, err)

	require.NotNil(t, responder.captured.OnRunSettled, "RespondToQuestion must wire OnRunSettled")
	responder.captured.OnRunSettled(&ExecuteResult{RunID: "run-resumed"})
	assert.True(t, repo.incrementedUsage, "resumed leg usage must be recorded via the wired callback")
}

// Session cap is enforced.
func TestCreateSession_SessionCap(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	cfg := DefaultShareLinkConfig()
	cfg.MaxActiveSessionsPerUser = 5
	link.Config = cfg
	binding := testBinding(link, testDefinition("def-a", "proj-a", "Agent A"))

	repo.activeSessions = 5

	svc := NewShareService(repo, nil, nil, nil, "", nil)
	_, err := svc.CreateSession(context.Background(), binding, "alice", "alice@example.com", "title")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
}

// Budget trip: when usage is at/over the cap, the stream is rejected before the
// runner executes.
func TestStreamMessage_BudgetTrip(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	cfg := DefaultShareLinkConfig()
	cfg.BudgetMaxMessages = 500
	link.Config = cfg
	binding := testBinding(link, testDefinition("def-a", "proj-a", "Agent A"))

	repo.reserveBudgetErr = apperror.New(429, "share_budget_exceeded", "share link message budget exceeded")

	runner := &shareFakeRunner{result: &ExecuteResult{RunID: "run-1"}}
	svc := NewShareService(repo, nil, runner, nil, "", nil)

	session := &AgentShareSession{ID: "sess-1", ACPSessionID: "acp-1", EndUserRef: "alice"}
	_, err := svc.StreamMessage(context.Background(), binding, session, "hello", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "budget")
	assert.Equal(t, "", runner.captured.ProjectID, "runner must not execute when budget is tripped")
}

// =============================================================================
// End-user ref signature verification
// =============================================================================

// signShareRef computes the gateway contract signature:
// base64url-nopad(HMAC-SHA256(secret, ref)).
func signShareRef(secret, ref string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ref))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyEndUserRefSig_MissingSig(t *testing.T) {
	svc := NewShareService(newFakeShareRepo(), nil, nil, nil, "secret", nil)
	err := svc.VerifyEndUserRefSig("11111111-1111-1111-1111-111111111111", "")
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, apperrStatus(err))
}

func TestVerifyEndUserRefSig_WrongSig(t *testing.T) {
	svc := NewShareService(newFakeShareRepo(), nil, nil, nil, "secret", nil)
	ref := "11111111-1111-1111-1111-111111111111"
	err := svc.VerifyEndUserRefSig(ref, signShareRef("other-secret", ref))
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, apperrStatus(err))
}

func TestVerifyEndUserRefSig_ValidSig(t *testing.T) {
	svc := NewShareService(newFakeShareRepo(), nil, nil, nil, "secret", nil)
	ref := "11111111-1111-1111-1111-111111111111"
	require.NoError(t, svc.VerifyEndUserRefSig(ref, signShareRef("secret", ref)))
}

func TestVerifyEndUserRefSig_SecretUnset(t *testing.T) {
	svc := NewShareService(newFakeShareRepo(), nil, nil, nil, "", nil)
	ref := "11111111-1111-1111-1111-111111111111"
	err := svc.VerifyEndUserRefSig(ref, "anything")
	require.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, apperrStatus(err))
}

// A gateway-minted ref (valid signature) presented against a different link
// must yield no data — the end-user DB row check scopes it to its link.
func TestEnsureEndUserExists_ForeignRef(t *testing.T) {
	repo := newFakeShareRepo()
	// No end-user row exists for (link-b, ref), so a validly-signed ref for
	// link-a must not resolve on link-b.
	svc := NewShareService(repo, nil, nil, nil, "secret", nil)
	ref := "11111111-1111-1111-1111-111111111111"
	err := svc.EnsureEndUserExists(context.Background(), "link-b", ref)
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, apperrStatus(err))
}

// apperrStatus extracts the HTTP status from an *apperror.Error.
func apperrStatus(err error) int {
	if e, ok := err.(*apperror.Error); ok {
		return e.HTTPStatus
	}
	return 0
}

// Revoking a link must also revoke the bound credential (core.api_tokens row)
// so the share key is immediately unusable.
func TestRevokeLink_RevokesCredential(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	repo.linkByID["link-a"] = link

	tokens := &shareFakeTokenService{}
	svc := NewShareService(repo, tokens, nil, nil, "", nil)

	require.NoError(t, svc.RevokeLink(context.Background(), "link-a", "proj-a", "owner-user"))

	assert.NotNil(t, link.RevokedAt, "link must be revoked")
	require.Len(t, tokens.revokedTokenIDs, 1, "bound credential must be revoked")
	assert.Equal(t, "token-a", tokens.revokedTokenIDs[0])
}

// A non-admin caller must be rejected from owner mutations.
func TestRevokeLink_NonAdminRejected(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	repo.linkByID["link-a"] = link

	tokens := &shareFakeTokenService{role: "project_viewer"}
	svc := NewShareService(repo, tokens, nil, nil, "", nil)

	err := svc.RevokeLink(context.Background(), "link-a", "proj-a", "viewer-user")
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, apperrStatus(err))
	assert.Nil(t, link.RevokedAt, "link must NOT be revoked for a non-admin")
}

// A non-admin caller must be rejected from link creation too.
func TestCreateLink_NonAdminRejected(t *testing.T) {
	repo := newFakeShareRepo()
	repo.defByID["def-a"] = testDefinition("def-a", "proj-a", "Agent A")

	tokens := &shareFakeTokenService{role: "project_viewer"}
	svc := NewShareService(repo, tokens, nil, nil, "", nil)

	_, err := svc.CreateLink(context.Background(), "proj-a", "def-a", "label", "viewer-user", nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, apperrStatus(err))
}

// Updating LinkExpiryDays must recompute expires_at so extend/expire/never-expire
// controls actually affect authorization.
func TestUpdateLink_RecomputesExpiry(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	repo.linkByID["link-a"] = link

	tokens := &shareFakeTokenService{}
	svc := NewShareService(repo, tokens, nil, nil, "", nil)

	one := 1
	_, err := svc.UpdateLink(context.Background(), "link-a", "proj-a", "admin-user", "", &ShareLinkConfigInput{LinkExpiryDays: &one})
	require.NoError(t, err)
	require.NotNil(t, link.ExpiresAt, "expires_at must be set for a finite expiry")

	zero := 0
	_, err = svc.UpdateLink(context.Background(), "link-a", "proj-a", "admin-user", "", &ShareLinkConfigInput{LinkExpiryDays: &zero})
	require.NoError(t, err)
	assert.Nil(t, link.ExpiresAt, "expires_at must be cleared for never-expire")
}

// The concurrent-run limit must be enforced atomically (busy before Execute).
func TestStreamMessage_ConcurrentRunLimit(t *testing.T) {
	repo := newFakeShareRepo()
	link := testLink("link-a", "proj-a", "def-a", "token-a")
	cfg := DefaultShareLinkConfig()
	cfg.MaxConcurrentRuns = 3
	link.Config = cfg
	binding := testBinding(link, testDefinition("def-a", "proj-a", "Agent A"))

	repo.runSlotErr = apperror.New(429, "share_busy", "share link is at its concurrent run limit")

	runner := &shareFakeRunner{result: &ExecuteResult{RunID: "run-1"}}
	svc := NewShareService(repo, nil, runner, nil, "", nil)

	session := &AgentShareSession{ID: "sess-1", ACPSessionID: "acp-1", EndUserRef: "alice"}
	_, err := svc.StreamMessage(context.Background(), binding, session, "hello", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "concurrent")
	assert.Equal(t, "", runner.captured.ProjectID, "runner must not execute when the concurrent limit is reached")
}
