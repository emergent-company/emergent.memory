package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Fakes for the agent-owned endpoint + keys
// ============================================================================

type fakeEndpointStore struct {
	byID      map[string]*AgentMCPEndpoint
	createErr error
}

func newFakeEndpointStore() *fakeEndpointStore {
	return &fakeEndpointStore{byID: map[string]*AgentMCPEndpoint{}}
}

func (f *fakeEndpointStore) GetEndpointByID(_ context.Context, id string) (*AgentMCPEndpoint, error) {
	return f.byID[id], nil
}

func (f *fakeEndpointStore) GetActiveEndpointByAgentID(_ context.Context, projectID, agentID string) (*AgentMCPEndpoint, error) {
	for _, ep := range f.byID {
		if ep.ProjectID == projectID && ep.AgentID == agentID && ep.RevokedAt == nil {
			return ep, nil
		}
	}
	return nil, nil
}

func (f *fakeEndpointStore) CreateEndpoint(_ context.Context, ep *AgentMCPEndpoint) error {
	if f.createErr != nil {
		return f.createErr
	}
	if ep.ID == "" {
		ep.ID = uuid.NewString()
	}
	f.byID[ep.ID] = ep
	return nil
}

func (f *fakeEndpointStore) RevokeEndpoint(_ context.Context, id string, revokedAt time.Time) error {
	if ep := f.byID[id]; ep != nil && ep.RevokedAt == nil {
		ep.RevokedAt = &revokedAt
	}
	return nil
}

func (f *fakeEndpointStore) TouchEndpoint(_ context.Context, id string, at time.Time) error {
	if ep := f.byID[id]; ep != nil {
		ep.UpdatedAt = at
	}
	return nil
}

type fakeKeyStore struct {
	byID      map[string]*AgentMCPKey
	endpoints map[string]*AgentMCPEndpoint
	createErr error
}

func newFakeKeyStore() *fakeKeyStore {
	return &fakeKeyStore{byID: map[string]*AgentMCPKey{}, endpoints: map[string]*AgentMCPEndpoint{}}
}

func (f *fakeKeyStore) detail(k *AgentMCPKey) *AgentMCPKeyDetail {
	d := &AgentMCPKeyDetail{
		ID:              k.ID,
		EndpointID:      k.EndpointID,
		TokenID:         k.TokenID,
		Label:           k.Label,
		CreatedBy:       k.CreatedBy,
		CreatedAt:       k.CreatedAt,
		UpdatedAt:       k.UpdatedAt,
		RevokedAt:       k.RevokedAt,
		TokenLastUsedAt: k.TokenLastUsedAt,
		TokenRevokedAt:  k.TokenRevokedAt,
		TokenExpiresAt:  k.TokenExpiresAt,
	}
	if ep := f.endpoints[k.EndpointID]; ep != nil {
		d.EndpointProjectID = ep.ProjectID
		d.EndpointAgentID = ep.AgentID
		d.EndpointRevokedAt = ep.RevokedAt
	}
	return d
}

func (f *fakeKeyStore) GetActiveKeyByTokenID(_ context.Context, tokenID string) (*AgentMCPKey, error) {
	for _, k := range f.byID {
		if k.TokenID == tokenID && k.RevokedAt == nil {
			return k, nil
		}
	}
	return nil, nil
}

func (f *fakeKeyStore) GetKeyByID(_ context.Context, id string) (*AgentMCPKeyDetail, error) {
	k := f.byID[id]
	if k == nil {
		return nil, nil
	}
	return f.detail(k), nil
}

func (f *fakeKeyStore) ListKeysByEndpoint(_ context.Context, endpointID string) ([]*AgentMCPKeyDetail, error) {
	out := []*AgentMCPKeyDetail{}
	for _, k := range f.byID {
		if k.EndpointID == endpointID {
			out = append(out, f.detail(k))
		}
	}
	return out, nil
}

func (f *fakeKeyStore) CreateKey(_ context.Context, k *AgentMCPKey) error {
	if f.createErr != nil {
		return f.createErr
	}
	for _, existing := range f.byID {
		if existing.TokenID == k.TokenID {
			return apperror.New(409, "agent_mcp_key_token_exists", "This credential is already bound to a key")
		}
		if existing.EndpointID == k.EndpointID && existing.RevokedAt == nil && strings.EqualFold(existing.Label, k.Label) {
			return apperror.New(409, "agent_mcp_key_label_exists", "An active key with this label already exists for this endpoint")
		}
	}
	if k.ID == "" {
		k.ID = uuid.NewString()
	}
	f.byID[k.ID] = k
	return nil
}

func (f *fakeKeyStore) RevokeKey(_ context.Context, id string, revokedAt time.Time) error {
	if k := f.byID[id]; k != nil && k.RevokedAt == nil {
		k.RevokedAt = &revokedAt
	}
	return nil
}

func (f *fakeKeyStore) SetKeyToken(_ context.Context, id, tokenID string, at time.Time) error {
	k := f.byID[id]
	if k == nil {
		return apperror.NewNotFound("Agent MCP key", id)
	}
	for _, existing := range f.byID {
		if existing.ID != id && existing.TokenID == tokenID {
			return apperror.New(409, "agent_mcp_key_token_exists", "This credential is already bound to a key")
		}
	}
	k.TokenID = tokenID
	k.UpdatedAt = at
	return nil
}

// endpointTestFixture wires an endpoint ep-1 (agent A, project proj-1) with one
// key bound to tok-1, plus the surrounding fakes.
type endpointTestFixture struct {
	endpoints *fakeEndpointStore
	keys      *fakeKeyStore
	tokenSvc  *fakeTokenSvc
	dir       *fakeAgentDir
	handler   *stubAgentHandler
	svc       *Service
}

func newEndpointTestFixture() *endpointTestFixture {
	endpoints := newFakeEndpointStore()
	keys := newFakeKeyStore()
	endpoints.byID["ep-1"] = &AgentMCPEndpoint{
		ID: "ep-1", ProjectID: "proj-1", AgentID: agentA, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	keys.endpoints["ep-1"] = endpoints.byID["ep-1"]
	keys.byID["key-1"] = &AgentMCPKey{
		ID: "key-1", EndpointID: "ep-1", TokenID: "tok-1", Label: "default",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	tokenSvc := &fakeTokenSvc{createToken: "emt_key", createID: "tok-key"}
	dir := &fakeAgentDir{agents: []AgentRef{
		{ID: agentA, Name: "Alpha", Enabled: true},
		{ID: agentB, Name: "Beta", Enabled: true},
	}}
	handler := &stubAgentHandler{runReply: "the reply"}
	svc := &Service{
		agentEndpoints:   endpoints,
		agentKeys:        keys,
		shareTokens:      tokenSvc,
		agentDir:         dir,
		agentToolHandler: handler,
	}
	return &endpointTestFixture{endpoints: endpoints, keys: keys, tokenSvc: tokenSvc, dir: dir, handler: handler, svc: svc}
}

// ============================================================================
// AuthorizeAgentEndpoint branch table
// ============================================================================

func TestAuthorizeAgentEndpointBranches(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)

	tests := []struct {
		name      string
		mutate    func(f *endpointTestFixture)
		token     string
		agentID   string
		wantErr   bool
		wantCode  string
		wantAgent string
	}{
		{
			name:      "bound key accepted",
			mutate:    func(f *endpointTestFixture) {},
			token:     "tok-1",
			agentID:   agentA,
			wantAgent: "ep-1",
		},
		{
			name:    "unbound credential rejected",
			mutate:  func(f *endpointTestFixture) {},
			token:   "other",
			agentID: agentA,
			wantErr: true,
		},
		{
			name:    "empty credential rejected",
			mutate:  func(f *endpointTestFixture) {},
			token:   "",
			agentID: agentA,
			wantErr: true,
		},
		{
			name:    "credential for different agent rejected",
			mutate:  func(f *endpointTestFixture) {},
			token:   "tok-1",
			agentID: agentB,
			wantErr: true,
		},
		{
			name: "revoked key rejected",
			mutate: func(f *endpointTestFixture) {
				revoked := now
				f.keys.byID["key-1"].RevokedAt = &revoked
			},
			token:   "tok-1",
			agentID: agentA,
			wantErr: true,
		},
		{
			name: "revoked endpoint rejected",
			mutate: func(f *endpointTestFixture) {
				revoked := now
				f.endpoints.byID["ep-1"].RevokedAt = &revoked
			},
			token:   "tok-1",
			agentID: agentA,
			wantErr: true,
		},
		{
			name: "revoked token rejected",
			mutate: func(f *endpointTestFixture) {
				revoked := now
				f.keys.byID["key-1"].TokenRevokedAt = &revoked
			},
			token:   "tok-1",
			agentID: agentA,
			wantErr: true,
		},
		{
			name: "expired token rejected",
			mutate: func(f *endpointTestFixture) {
				f.keys.byID["key-1"].TokenExpiresAt = &past
			},
			token:   "tok-1",
			agentID: agentA,
			wantErr: true,
		},
		{
			name: "disabled agent rejected",
			mutate: func(f *endpointTestFixture) {
				f.dir.agents[0].Enabled = false
			},
			token:   "tok-1",
			agentID: agentA,
			wantErr: true,
		},
		{
			name: "missing agent rejected",
			mutate: func(f *endpointTestFixture) {
				f.dir.agents = nil
			},
			token:   "tok-1",
			agentID: agentA,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newEndpointTestFixture()
			tt.mutate(f)
			endpoint, key, err := f.svc.AuthorizeAgentEndpoint(context.Background(), tt.token, tt.agentID)
			if tt.wantErr {
				require.Error(t, err)
				assertAppError(t, err, 403)
				assert.Nil(t, endpoint)
				assert.Nil(t, key)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, endpoint)
			require.NotNil(t, key)
			assert.Equal(t, tt.wantAgent, endpoint.ID)
		})
	}
}

// ============================================================================
// Endpoint lifecycle
// ============================================================================

func TestCreateAgentEndpoint(t *testing.T) {
	f := newEndpointTestFixture()
	f.endpoints.byID = map[string]*AgentMCPEndpoint{} // no endpoint yet

	dto, err := f.svc.CreateAgentEndpoint(context.Background(), "proj-1", "user-1", "http://h", agentA)
	require.NoError(t, err)
	require.NotNil(t, dto)
	assert.Equal(t, agentA, dto.AgentID)
	assert.Equal(t, "active", dto.Status)
	assert.Equal(t, "http://h/api/mcp/agents/"+agentA, dto.MCPURL)
	assert.Len(t, f.endpoints.byID, 1)
}

func TestCreateAgentEndpointDuplicateRejected(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.CreateAgentEndpoint(context.Background(), "proj-1", "user-1", "", agentA)
	assertAppError(t, err, 409)
}

func TestCreateAgentEndpointUnknownAgent(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.CreateAgentEndpoint(context.Background(), "proj-1", "user-1", "", agentDefUnknown)
	assertAppError(t, err, 404)
}

func TestCreateAgentEndpointNonAdmin(t *testing.T) {
	f := newEndpointTestFixture()
	f.tokenSvc.role = "project_user"
	_, err := f.svc.CreateAgentEndpoint(context.Background(), "proj-1", "user-1", "", agentA)
	assertAppError(t, err, 403)
}

func TestCreateAgentEndpointAcceptsDefinitionID(t *testing.T) {
	f := newEndpointTestFixture()
	f.endpoints.byID = map[string]*AgentMCPEndpoint{}
	f.dir.definitions = map[string][]AgentRef{agentDefA: {{ID: agentB, Name: "Beta", Enabled: true}}}

	dto, err := f.svc.CreateAgentEndpoint(context.Background(), "proj-1", "user-1", "", agentDefA)
	require.NoError(t, err)
	assert.Equal(t, agentB, dto.AgentID)
}

func TestGetAgentEndpointNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	f.endpoints.byID = map[string]*AgentMCPEndpoint{}
	_, err := f.svc.GetAgentEndpoint(context.Background(), "proj-1", "user-1", "", agentA)
	assertAppError(t, err, 404)
}

func TestGetAgentEndpoint(t *testing.T) {
	f := newEndpointTestFixture()
	dto, err := f.svc.GetAgentEndpoint(context.Background(), "proj-1", "user-1", "http://h", agentA)
	require.NoError(t, err)
	assert.Equal(t, "ep-1", dto.ID)
	assert.Equal(t, "http://h/api/mcp/agents/"+agentA, dto.MCPURL)
}

func TestRevokeAgentEndpointIdempotentRevokesKeys(t *testing.T) {
	f := newEndpointTestFixture()
	require.NoError(t, f.svc.RevokeAgentEndpoint(context.Background(), "proj-1", "user-1", "ep-1"))
	assert.NotNil(t, f.endpoints.byID["ep-1"].RevokedAt)
	assert.NotNil(t, f.keys.byID["key-1"].RevokedAt)
	assert.Equal(t, []string{"tok-1"}, f.tokenSvc.revoked)

	// Idempotent: a second revoke succeeds and does not re-revoke.
	require.NoError(t, f.svc.RevokeAgentEndpoint(context.Background(), "proj-1", "user-1", "ep-1"))
	assert.Equal(t, []string{"tok-1"}, f.tokenSvc.revoked)
}

func TestRevokeAgentEndpointCrossProjectNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	assertAppError(t, f.svc.RevokeAgentEndpoint(context.Background(), "proj-2", "user-1", "ep-1"), 404)
	assert.Nil(t, f.endpoints.byID["ep-1"].RevokedAt)
}

// ============================================================================
// Key lifecycle
// ============================================================================

func TestCreateAgentKey(t *testing.T) {
	f := newEndpointTestFixture()
	expires := time.Now().UTC().Add(24 * time.Hour)

	resp, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "http://h", "ep-1", CreateAgentMCPKeyRequest{
		Label: "  CI Key  ", ExpiresAt: &expires,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "CI Key", resp.Label)
	assert.Equal(t, "emt_key", resp.Token)
	assert.Equal(t, "http://h/api/mcp/agents/"+agentA, resp.MCPURL)
	assert.Equal(t, &expires, f.tokenSvc.createdExpiresAt)
	assert.ElementsMatch(t, agentShareScopes, f.tokenSvc.createdScopes)
	assert.Equal(t, agentA, resp.AgentID)
	// The minted token name carries the key label and fits varchar(255).
	assert.Equal(t, "Agent MCP Key: CI Key", agentMCPKeyTokenName("CI Key"))
	assert.True(t, strings.HasPrefix(agentMCPKeyTokenName(strings.Repeat("x", 500)), "Agent MCP Key: "))
	assert.LessOrEqual(t, len(agentMCPKeyTokenName(strings.Repeat("x", 500))), 255)
}

func TestCreateAgentKeyDuplicateLabelCaseInsensitive(t *testing.T) {
	f := newEndpointTestFixture()
	// The fixture's key-1 is labeled "default"; an upper-cased duplicate must be
	// rejected case-insensitively, while a distinct label is accepted.
	_, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "DEFAULT"})
	assertAppError(t, err, 409)

	resp, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "second"})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.ID)
}

func TestCreateAgentKeyMissingLabel(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "   "})
	assertAppError(t, err, 422)
}

func TestCreateAgentKeyCrossProjectEndpointNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.CreateAgentKey(context.Background(), "proj-2", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "X"})
	assertAppError(t, err, 404)
}

// A label reused on another endpoint in the same project must not be rejected on
// the project-unique core.api_tokens name; the token name is disambiguated.
func TestCreateAgentKeyDisambiguatesTokenNameCollision(t *testing.T) {
	f := newEndpointTestFixture()
	f.tokenSvc.createErrOnce = apperror.New(409, "token_name_exists", "A token named ... already exists")
	resp, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "shared"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, f.tokenSvc.createCalls)
	assert.Equal(t, "Agent MCP Key: shared ("+agentA+")", agentMCPKeyTokenNameDisambiguated("shared", agentA))
	assert.LessOrEqual(t, len(agentMCPKeyTokenNameDisambiguated(strings.Repeat("x", 500), agentA)), 255)
}

func TestListAgentKeysOmitsSecrets(t *testing.T) {
	f := newEndpointTestFixture()
	// Create a second key so the list has two entries.
	_, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "second"})
	require.NoError(t, err)

	resp, err := f.svc.ListAgentKeys(context.Background(), "proj-1", "user-1", "ep-1")
	require.NoError(t, err)
	require.Len(t, resp.Keys, 2)
	for _, k := range resp.Keys {
		assert.NotEmpty(t, k.Label)
		assert.NotEmpty(t, k.Status)
	}
	// A revoked key is listed with a revoked status.
	require.NoError(t, f.svc.RevokeAgentKey(context.Background(), "proj-1", "user-1", "key-1"))
	resp, err = f.svc.ListAgentKeys(context.Background(), "proj-1", "user-1", "ep-1")
	require.NoError(t, err)
	statuses := map[string]string{}
	for _, k := range resp.Keys {
		statuses[k.ID] = k.Status
	}
	assert.Equal(t, "revoked", statuses["key-1"])
}

func TestListAgentKeysCrossProjectNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.ListAgentKeys(context.Background(), "proj-2", "user-1", "ep-1")
	assertAppError(t, err, 404)
}

func TestRevokeAgentKeyLeavesSiblingsWorking(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.CreateAgentKey(context.Background(), "proj-1", "user-1", "", "ep-1", CreateAgentMCPKeyRequest{Label: "second"})
	require.NoError(t, err)

	require.NoError(t, f.svc.RevokeAgentKey(context.Background(), "proj-1", "user-1", "key-1"))
	assert.NotNil(t, f.keys.byID["key-1"].RevokedAt)

	// The sibling still authorizes the endpoint.
	var siblingToken string
	for _, k := range f.keys.byID {
		if k.ID != "key-1" {
			siblingToken = k.TokenID
		}
	}
	require.NotEmpty(t, siblingToken)
	_, _, err = f.svc.AuthorizeAgentEndpoint(context.Background(), siblingToken, agentA)
	require.NoError(t, err)

	// Revoking the same key again is idempotent.
	require.NoError(t, f.svc.RevokeAgentKey(context.Background(), "proj-1", "user-1", "key-1"))
}

func TestRotateAgentKeyPreservesIdentity(t *testing.T) {
	f := newEndpointTestFixture()
	f.tokenSvc.regenerateID = "tok-rotated"
	f.tokenSvc.regenerateToken = "emt_rotated"

	resp, err := f.svc.RotateAgentKey(context.Background(), "proj-1", "user-1", "http://h", "key-1")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-1", resp.ID, "key identity is preserved")
	assert.Equal(t, "emt_rotated", resp.Token)
	assert.Equal(t, "tok-rotated", f.keys.byID["key-1"].TokenID)
	assert.True(t, f.tokenSvc.regenerateCalled)
	assert.Equal(t, "http://h/api/mcp/agents/"+agentA, resp.MCPURL)
}

func TestRotateAgentKeyCrossProjectNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	_, err := f.svc.RotateAgentKey(context.Background(), "proj-2", "user-1", "", "key-1")
	assertAppError(t, err, 404)
}

func TestRevokeAgentKeyCrossProjectNotFound(t *testing.T) {
	f := newEndpointTestFixture()
	assertAppError(t, f.svc.RevokeAgentKey(context.Background(), "proj-2", "user-1", "key-1"), 404)
}

// ============================================================================
// call_agent back-compat
// ============================================================================

// TestCallAgentOnceExactReplyAndErrorPayload pins the byte-level contract of
// call_agent: a bare text reply with no envelope, and the exact
// agentRunErrorResult error payload.
func TestCallAgentOnceExactReplyAndErrorPayload(t *testing.T) {
	success := &Service{agentToolHandler: &stubAgentHandler{runReply: "the answer"}}
	res := success.CallAgentOnce(context.Background(), "proj", agentA, "hi")
	require.NotNil(t, res)
	assert.False(t, res.IsError)
	require.Len(t, res.Content, 1)
	assert.Equal(t, "text", res.Content[0].Type)
	assert.Equal(t, "the answer", res.Content[0].Text)

	failure := &Service{agentToolHandler: &stubAgentHandler{runErr: &AgentRunError{Kind: AgentRunErrorFailed, Message: "boom"}}}
	res = failure.CallAgentOnce(context.Background(), "proj", agentA, "hi")
	require.NotNil(t, res)
	assert.True(t, res.IsError)
	require.Len(t, res.Content, 1)
	assert.Equal(t, `{"error":"boom","kind":"run_failed"}`, res.Content[0].Text)

	paused := &Service{agentToolHandler: &stubAgentHandler{runErr: &AgentRunError{
		Kind: AgentRunErrorPaused, Message: "needs input", Question: "Which one?", RunID: "run-9",
	}}}
	res = paused.CallAgentOnce(context.Background(), "proj", agentA, "hi")
	assert.Equal(t, `{"error":"needs input","kind":"input_required","question":"Which one?","runId":"run-9"}`, res.Content[0].Text)
	assert.True(t, res.IsError)
}
