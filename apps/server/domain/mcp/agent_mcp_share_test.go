package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Fake store
// ============================================================================

type fakeAgentShareStore struct {
	byID      map[string]*AgentMCPShare
	createErr error
	updateErr error
}

func newFakeAgentShareStore() *fakeAgentShareStore {
	return &fakeAgentShareStore{byID: map[string]*AgentMCPShare{}}
}

func (f *fakeAgentShareStore) ListByProject(_ context.Context, projectID string) ([]*AgentMCPShare, error) {
	out := []*AgentMCPShare{}
	for _, s := range f.byID {
		if s.ProjectID == projectID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeAgentShareStore) ListByAgent(_ context.Context, projectID, agentID string) ([]*AgentMCPShare, error) {
	out := []*AgentMCPShare{}
	for _, s := range f.byID {
		if s.ProjectID == projectID && s.AgentID == agentID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeAgentShareStore) GetByID(_ context.Context, projectID, id string) (*AgentMCPShare, error) {
	s := f.byID[id]
	if s == nil || s.ProjectID != projectID {
		return nil, nil
	}
	return s, nil
}

func (f *fakeAgentShareStore) GetByTokenID(_ context.Context, tokenID string) (*AgentMCPShare, error) {
	for _, s := range f.byID {
		if s.TokenID == tokenID && s.RevokedAt == nil {
			return s, nil
		}
	}
	return nil, nil
}

func (f *fakeAgentShareStore) FindByName(_ context.Context, projectID, name string) (*AgentMCPShare, error) {
	for _, s := range f.byID {
		if s.ProjectID == projectID && s.RevokedAt == nil && equalFold(s.Name, name) {
			return s, nil
		}
	}
	return nil, nil
}

func (f *fakeAgentShareStore) Create(_ context.Context, s *AgentMCPShare) error {
	if f.createErr != nil {
		return f.createErr
	}
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	f.byID[s.ID] = s
	return nil
}

func (f *fakeAgentShareStore) Update(_ context.Context, s *AgentMCPShare) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.byID[s.ID] = s
	return nil
}

func newAgentShareService(store agentMCPShareStore, tok shareTokenService, dir agentDirectory, handler AgentToolHandler) *Service {
	return &Service{agentShares: store, shareTokens: tok, agentDir: dir, agentToolHandler: handler}
}

func agentShareFakes() (*fakeAgentShareStore, *fakeTokenSvc, *fakeAgentDir) {
	return newFakeAgentShareStore(), &fakeTokenSvc{createToken: "emt_agent", createID: "tok-agent"}, &fakeAgentDir{
		agents: []AgentRef{{ID: agentA, Name: "Alpha", Enabled: true}},
	}
}

// ============================================================================
// Pure helper tests
// ============================================================================

func TestNormalizeAgentShareName(t *testing.T) {
	got, err := normalizeAgentShareName("  Team A  ")
	require.NoError(t, err)
	assert.Equal(t, "Team A", got)

	_, err = normalizeAgentShareName("   ")
	assert.Error(t, err)
}

func TestDefaultAgentShareNameIdentifiesAgentAndDate(t *testing.T) {
	name := defaultAgentShareName("Alpha")
	assert.Contains(t, name, "Alpha")
	assert.Contains(t, name, time.Now().UTC().Format("2006-01-02"))
}

func TestAgentMCPEndpointURL(t *testing.T) {
	assert.Equal(t, "/api/mcp/agents/abc", agentMCPEndpointURL("", "abc"))
	assert.Equal(t, "https://h/api/mcp/agents/abc", agentMCPEndpointURL("https://h/", "abc"))
}

// L1: token names must fit core.api_tokens.name varchar(255).
func TestAgentShareTokenNameCapped(t *testing.T) {
	name := agentShareTokenName(strings.Repeat("x", 500))
	assert.True(t, strings.HasPrefix(name, "Agent MCP Share: "))
	assert.LessOrEqual(t, len(name), 255)

	short := agentShareTokenName("Team A")
	assert.Equal(t, "Agent MCP Share: Team A", short)
}

// M1 + C1: the share scope set grants read-only MCP access plus the marker, and
// never broad write scopes.
func TestAgentShareScopes(t *testing.T) {
	assert.Contains(t, agentShareScopes, AgentCallScope)
	assert.Contains(t, agentShareScopes, "data:read")
	assert.Contains(t, agentShareScopes, "schema:read")
	assert.Contains(t, agentShareScopes, "agents:read")
	assert.Contains(t, agentShareScopes, "projects:read")
	assert.Contains(t, agentShareScopes, "chat:use")
	assert.NotContains(t, agentShareScopes, "data:write")
	assert.NotContains(t, agentShareScopes, "agents:write")
}

func TestHasAgentCallScope(t *testing.T) {
	assert.True(t, hasAgentCallScope([]string{"data:read", AgentCallScope}))
	assert.False(t, hasAgentCallScope([]string{"data:read", "admin:all"}))
	assert.False(t, hasAgentCallScope(nil))
}

func TestAgentShareStatus(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	assert.Equal(t, "active", agentShareStatus(nil, nil, nil, now))
	assert.Equal(t, "revoked", agentShareStatus(&past, nil, nil, now))
	assert.Equal(t, "revoked", agentShareStatus(nil, &past, nil, now))
	assert.Equal(t, "expired", agentShareStatus(nil, nil, &past, now))
}

// ============================================================================
// Service lifecycle tests
// ============================================================================

func TestCreateAgentShare(t *testing.T) {
	store, tok, dir := agentShareFakes()
	svc := newAgentShareService(store, tok, dir, nil)

	resp, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "http://h", agentA, CreateAgentMCPShareRequest{Name: "Team A"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "emt_agent", resp.Token)
	assert.Equal(t, agentA, resp.AgentID)
	assert.Equal(t, "http://h/api/mcp/agents/"+agentA, resp.MCPURL)
	assert.Equal(t, "active", resp.Status)
	assert.ElementsMatch(t, agentShareScopes, tok.createdScopes)
	assert.Len(t, store.byID, 1)
}

func TestCreateAgentShareDefaultName(t *testing.T) {
	store, tok, dir := agentShareFakes()
	svc := newAgentShareService(store, tok, dir, nil)

	resp, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "", agentA, CreateAgentMCPShareRequest{})
	require.NoError(t, err)
	assert.Contains(t, resp.Name, "Alpha")
}

func TestCreateAgentShareNonAdmin(t *testing.T) {
	store, tok, dir := agentShareFakes()
	tok.role = "project_user"
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "", agentA, CreateAgentMCPShareRequest{Name: "X"})
	assertAppError(t, err, 403)
	assert.Empty(t, store.byID)
}

func TestCreateAgentShareUnknownAgent(t *testing.T) {
	store, tok, dir := agentShareFakes()
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "", agentB, CreateAgentMCPShareRequest{Name: "X"})
	assertAppError(t, err, 404)
	assert.Empty(t, store.byID)
}

func TestCreateAgentShareInvalidAgentID(t *testing.T) {
	store, tok, dir := agentShareFakes()
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "", "not-a-uuid", CreateAgentMCPShareRequest{Name: "X"})
	assertAppError(t, err, 422)
}

func TestCreateAgentShareDuplicateName(t *testing.T) {
	store, tok, dir := agentShareFakes()
	store.byID["existing"] = &AgentMCPShare{ID: "existing", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "t0"}
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "", agentA, CreateAgentMCPShareRequest{Name: "team a"})
	assertAppError(t, err, 409)
}

func TestListAgentSharesOmitsToken(t *testing.T) {
	store, tok, dir := agentShareFakes()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "tok-1"}
	store.byID["s2"] = &AgentMCPShare{ID: "s2", ProjectID: "proj-1", AgentID: agentB, Name: "Team B", TokenID: "tok-2"}
	svc := newAgentShareService(store, tok, dir, nil)

	perAgent, err := svc.ListAgentShares(context.Background(), "proj-1", "user-1", agentA)
	require.NoError(t, err)
	require.Len(t, perAgent.Shares, 1)
	assert.Equal(t, "s1", perAgent.Shares[0].ID)

	all, err := svc.ListProjectAgentShares(context.Background(), "proj-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, 2, all.Total)

	for _, s := range all.Shares {
		assert.NotEmpty(t, s.Status)
		assert.NotContains(t, s.Name, "emt_")
	}
}

func TestListAgentSharesMarksRevoked(t *testing.T) {
	store, tok, dir := agentShareFakes()
	revoked := time.Now()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "tok-1", RevokedAt: &revoked}
	svc := newAgentShareService(store, tok, dir, nil)

	resp, err := svc.ListAgentShares(context.Background(), "proj-1", "user-1", agentA)
	require.NoError(t, err)
	require.Len(t, resp.Shares, 1)
	assert.Equal(t, "revoked", resp.Shares[0].Status)
}

func TestRevokeAgentShareIdempotent(t *testing.T) {
	store, tok, dir := agentShareFakes()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "tok-1"}
	svc := newAgentShareService(store, tok, dir, nil)

	require.NoError(t, svc.RevokeAgentShare(context.Background(), "proj-1", "user-1", "s1"))
	assert.NotNil(t, store.byID["s1"].RevokedAt)
	assert.Equal(t, []string{"tok-1"}, tok.revoked)

	require.NoError(t, svc.RevokeAgentShare(context.Background(), "proj-1", "user-1", "s1"))
	assert.Equal(t, []string{"tok-1"}, tok.revoked)
}

func TestRevokeAgentShareNotFound(t *testing.T) {
	store, tok, dir := agentShareFakes()
	svc := newAgentShareService(store, tok, dir, nil)
	assertAppError(t, svc.RevokeAgentShare(context.Background(), "proj-1", "user-1", "missing"), 404)
}

func TestRotateAgentSharePreservesBinding(t *testing.T) {
	store, tok, dir := agentShareFakes()
	tok.regenerateID = "new"
	tok.regenerateToken = "emt_new"
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "old"}
	svc := newAgentShareService(store, tok, dir, nil)

	resp, err := svc.RotateAgentShare(context.Background(), "proj-1", "user-1", "s1", "http://h")
	require.NoError(t, err)
	assert.Equal(t, "emt_new", resp.Token)
	assert.Equal(t, "new", store.byID["s1"].TokenID)
	assert.Equal(t, agentA, store.byID["s1"].AgentID)
	assert.True(t, tok.regenerateCalled)
}

func TestRotateAgentShareFailureLeavesUnchanged(t *testing.T) {
	store, tok, dir := agentShareFakes()
	tok.regenerateErr = errors.New("boom")
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "old"}
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.RotateAgentShare(context.Background(), "proj-1", "user-1", "s1", "http://h")
	require.Error(t, err)
	assert.Equal(t, "old", store.byID["s1"].TokenID)
}

func TestRotateRevokedAgentShareRejected(t *testing.T) {
	store, tok, dir := agentShareFakes()
	revoked := time.Now()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "old", RevokedAt: &revoked}
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.RotateAgentShare(context.Background(), "proj-1", "user-1", "s1", "")
	assertAppError(t, err, 409)
}

// ============================================================================
// Authorization tests
// ============================================================================

func TestAuthorizeAgentShare(t *testing.T) {
	store, tok, dir := agentShareFakes()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "tok-1"}
	svc := newAgentShareService(store, tok, dir, nil)

	share, err := svc.AuthorizeAgentShare(context.Background(), "tok-1", agentA)
	require.NoError(t, err)
	require.NotNil(t, share)
	assert.Equal(t, "s1", share.ID)

	// Unbound token.
	_, err = svc.AuthorizeAgentShare(context.Background(), "unknown", agentA)
	assertAppError(t, err, 403)

	// Bound to a different agent.
	_, err = svc.AuthorizeAgentShare(context.Background(), "tok-1", agentB)
	assertAppError(t, err, 403)

	// Empty token.
	_, err = svc.AuthorizeAgentShare(context.Background(), "", agentA)
	assertAppError(t, err, 403)
}

func TestAuthorizeAgentShareRejectsRevokedOrExpiredToken(t *testing.T) {
	revoked := time.Now()
	past := time.Now().Add(-time.Hour)

	store, tok, dir := agentShareFakes()
	store.byID["revoked"] = &AgentMCPShare{ID: "revoked", ProjectID: "proj-1", AgentID: agentA, Name: "R", TokenID: "tok-revoked", TokenRevokedAt: &revoked}
	svc := newAgentShareService(store, tok, dir, nil)
	_, err := svc.AuthorizeAgentShare(context.Background(), "tok-revoked", agentA)
	assertAppError(t, err, 403)

	store2, tok2, dir2 := agentShareFakes()
	store2.byID["expired"] = &AgentMCPShare{ID: "expired", ProjectID: "proj-1", AgentID: agentA, Name: "E", TokenID: "tok-expired", TokenExpiresAt: &past}
	svc2 := newAgentShareService(store2, tok2, dir2, nil)
	_, err = svc2.AuthorizeAgentShare(context.Background(), "tok-expired", agentA)
	assertAppError(t, err, 403)
}

// ============================================================================
// CallAgentOnce mapping tests
// ============================================================================

func TestCallAgentOnceMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantKind   AgentRunErrorKind
		wantField  string
		wantSecret string
	}{
		{name: "unavailable", err: &AgentRunError{Kind: AgentRunErrorUnavailable, Message: "agent not found"}, wantKind: AgentRunErrorUnavailable, wantField: "agent not found"},
		{name: "failed", err: &AgentRunError{Kind: AgentRunErrorFailed, Message: "run failed"}, wantKind: AgentRunErrorFailed, wantField: "run failed"},
		{name: "paused", err: &AgentRunError{Kind: AgentRunErrorPaused, Message: "needs input", Question: "Which one?"}, wantKind: AgentRunErrorPaused, wantField: "Which one?"},
		{name: "budget", err: &AgentRunError{Kind: AgentRunErrorBudget, Message: "exceeded"}, wantKind: AgentRunErrorBudget, wantField: "exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newAgentShareService(newFakeAgentShareStore(), &fakeTokenSvc{}, &fakeAgentDir{}, &stubAgentHandler{runErr: tt.err})
			res := svc.CallAgentOnce(context.Background(), "proj", agentA, "hi")
			require.NotNil(t, res)
			assert.True(t, res.IsError)
			assert.Contains(t, res.Content[0].Text, string(tt.wantKind))
			assert.Contains(t, res.Content[0].Text, tt.wantField)
		})
	}
}

func TestCallAgentOnceSuccess(t *testing.T) {
	svc := newAgentShareService(newFakeAgentShareStore(), &fakeTokenSvc{}, &fakeAgentDir{}, &stubAgentHandler{runReply: "the answer"})
	res := svc.CallAgentOnce(context.Background(), "proj", agentA, "hi")
	require.NotNil(t, res)
	assert.False(t, res.IsError)
	assert.Equal(t, "the answer", res.Content[0].Text)
}

func TestCallAgentOnceUnavailableHandler(t *testing.T) {
	svc := newAgentShareService(newFakeAgentShareStore(), &fakeTokenSvc{}, &fakeAgentDir{}, nil)
	res := svc.CallAgentOnce(context.Background(), "proj", agentA, "hi")
	require.NotNil(t, res)
	assert.True(t, res.IsError)
}

// ============================================================================
// Domain-level end-to-end
// ============================================================================

func TestAgentShareEndToEnd(t *testing.T) {
	store, tok, dir := agentShareFakes()
	handler := &stubAgentHandler{runReply: "hello from the agent"}
	svc := newAgentShareService(store, tok, dir, handler)

	// 1. Create a share.
	resp, err := svc.CreateAgentShare(context.Background(), "proj-1", "user-1", "http://h", agentA, CreateAgentMCPShareRequest{Name: "E2E"})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Token)

	// 2. The minted token resolves to the share.
	share := svc.ResolveAgentShareByToken(context.Background(), "tok-agent")
	require.NotNil(t, share)
	assert.Equal(t, agentA, share.AgentID)

	// 3. The endpoint lists exactly one tool.
	ep := NewAgentEndpointHandler(svc, slog.Default())
	listResp := ep.handleToolsList(context.Background(), &Request{JSONRPC: "2.0", ID: []byte(`1`)}, share)
	require.Nil(t, listResp.Error)
	list := listResp.Result.(ToolsListResult)
	require.Len(t, list.Tools, 1)
	assert.Equal(t, agentCallToolName, list.Tools[0].Name)
	assert.Contains(t, list.Tools[0].Description, "Alpha")

	// 4. call_agent returns the reply.
	params := mustJSON(t, ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "ping"}})
	callResp := ep.handleToolsCall(context.Background(), &Request{JSONRPC: "2.0", ID: []byte(`2`), Params: params}, share)
	require.Nil(t, callResp.Error)
	tr := callResp.Result.(*ToolResult)
	assert.False(t, tr.IsError)
	assert.Equal(t, "hello from the agent", tr.Content[0].Text)
	assert.True(t, handler.runCalled)

	// 5. Revoke invalidates the token binding.
	require.NoError(t, svc.RevokeAgentShare(context.Background(), "proj-1", "user-1", resp.ID))
	assert.Nil(t, svc.ResolveAgentShareByToken(context.Background(), "tok-agent"))
	_, authErr := svc.AuthorizeAgentShare(context.Background(), "tok-agent", agentA)
	assertAppError(t, authErr, 403)
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestRotateAgentShareInvalidatesOldToken(t *testing.T) {
	store, tok, dir := agentShareFakes()
	tok.regenerateID = "new"
	tok.regenerateToken = "emt_new"
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "old"}
	svc := newAgentShareService(store, tok, dir, nil)

	_, err := svc.RotateAgentShare(context.Background(), "proj-1", "user-1", "s1", "")
	require.NoError(t, err)
	assert.Nil(t, svc.ResolveAgentShareByToken(context.Background(), "old"), "previous token no longer resolves")
	assert.NotNil(t, svc.ResolveAgentShareByToken(context.Background(), "new"), "new token authorizes")
}

func TestAgentShareProjectScoping(t *testing.T) {
	store, tok, dir := agentShareFakes()
	store.byID["s1"] = &AgentMCPShare{ID: "s1", ProjectID: "proj-1", AgentID: agentA, Name: "Team A", TokenID: "tok-1"}
	svc := newAgentShareService(store, tok, dir, nil)

	// Another project cannot see or revoke the share.
	resp, err := svc.ListProjectAgentShares(context.Background(), "proj-2", "user-1")
	require.NoError(t, err)
	assert.Zero(t, resp.Total)
	assertAppError(t, svc.RevokeAgentShare(context.Background(), "proj-2", "user-1", "s1"), 404)
}
