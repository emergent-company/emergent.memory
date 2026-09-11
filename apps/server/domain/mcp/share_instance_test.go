package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/pkg/acpslug"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// Valid-UUID agent IDs (the AllowedAgents column is uuid[]).
const (
	agentA       = "00000000-0000-0000-0000-0000000000a1"
	agentB       = "00000000-0000-0000-0000-0000000000b1"
	agentC       = "00000000-0000-0000-0000-0000000000c1"
	agentDeleted = "00000000-0000-0000-0000-00000000dead"
)

// ============================================================================
// Fakes
// ============================================================================

type fakeShareStore struct {
	byID          map[string]*MCPShareInstance
	legacy        []*LegacyTokenRef
	createErr     error
	updateErr     error
	getByTokenErr error
}

func newFakeShareStore() *fakeShareStore {
	return &fakeShareStore{byID: map[string]*MCPShareInstance{}}
}

func (f *fakeShareStore) ListByProject(_ context.Context, projectID string) ([]*MCPShareInstance, error) {
	out := []*MCPShareInstance{}
	for _, inst := range f.byID {
		if inst.ProjectID == projectID {
			out = append(out, inst)
		}
	}
	return out, nil
}

func (f *fakeShareStore) GetByID(_ context.Context, projectID, id string) (*MCPShareInstance, error) {
	inst := f.byID[id]
	if inst == nil || inst.ProjectID != projectID {
		return nil, nil
	}
	return inst, nil
}

func (f *fakeShareStore) GetByTokenID(_ context.Context, tokenID string) (*MCPShareInstance, error) {
	if f.getByTokenErr != nil {
		return nil, f.getByTokenErr
	}
	for _, inst := range f.byID {
		if inst.TokenID == tokenID && inst.RevokedAt == nil {
			return inst, nil
		}
	}
	return nil, nil
}

func (f *fakeShareStore) FindByName(_ context.Context, projectID, name string) (*MCPShareInstance, error) {
	for _, inst := range f.byID {
		if inst.ProjectID == projectID && inst.RevokedAt == nil && equalFold(inst.Name, name) {
			return inst, nil
		}
	}
	return nil, nil
}

func (f *fakeShareStore) Create(_ context.Context, inst *MCPShareInstance) error {
	if f.createErr != nil {
		return f.createErr
	}
	if inst.ID == "" {
		inst.ID = uuid.NewString()
	}
	f.byID[inst.ID] = inst
	return nil
}

func (f *fakeShareStore) Update(_ context.Context, inst *MCPShareInstance) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.byID[inst.ID] = inst
	return nil
}

func (f *fakeShareStore) ListLegacyTokens(_ context.Context, _ string) ([]*LegacyTokenRef, error) {
	return f.legacy, nil
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

type fakeTokenSvc struct {
	role             string
	createToken      string
	createID         string
	createErr        error
	regenerateToken  string
	regenerateID     string
	regenerateErr    error
	updateErr        error
	updateScopes     []string
	revoked          []string
	createdScopes    []string
	regenerateCalled bool
}

func (f *fakeTokenSvc) Create(_ context.Context, _, _, _ string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.createdScopes = scopes
	id := f.createID
	if id == "" {
		id = uuid.NewString()
	}
	return &apitoken.CreateApiTokenResponseDTO{
		ApiTokenDTO: apitoken.ApiTokenDTO{ID: id, Scopes: scopes},
		Token:       f.createToken,
	}, nil
}

func (f *fakeTokenSvc) UpdateScopes(_ context.Context, _, _, _ string, scopes []string) (*apitoken.ApiTokenDTO, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.updateScopes = scopes
	return &apitoken.ApiTokenDTO{ID: "tok", Scopes: scopes}, nil
}

func (f *fakeTokenSvc) Revoke(_ context.Context, tokenID, _, _ string) error {
	f.revoked = append(f.revoked, tokenID)
	return nil
}

func (f *fakeTokenSvc) Regenerate(_ context.Context, _, _, _ string) (*apitoken.CreateApiTokenResponseDTO, error) {
	return f.RegenerateWith(context.Background(), "", "", "", nil)
}

func (f *fakeTokenSvc) RegenerateWith(_ context.Context, _, _, _ string, _ func(context.Context, bun.Tx, string) error) (*apitoken.CreateApiTokenResponseDTO, error) {
	f.regenerateCalled = true
	if f.regenerateErr != nil {
		return nil, f.regenerateErr
	}
	id := f.regenerateID
	if id == "" {
		id = uuid.NewString()
	}
	return &apitoken.CreateApiTokenResponseDTO{
		ApiTokenDTO: apitoken.ApiTokenDTO{ID: id},
		Token:       f.regenerateToken,
	}, nil
}

func (f *fakeTokenSvc) GetUserProjectRole(_ context.Context, _, _ string) (string, error) {
	if f.role == "" {
		return "project_admin", nil
	}
	return f.role, nil
}

type fakeAgentDir struct {
	agents []AgentRef
}

func (f *fakeAgentDir) ListProjectAgents(_ context.Context, _ string) ([]AgentRef, error) {
	return f.agents, nil
}

func (f *fakeAgentDir) FindProjectAgentByID(_ context.Context, _, id string) (*AgentRef, error) {
	for i := range f.agents {
		if f.agents[i].ID == id {
			a := f.agents[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (f *fakeAgentDir) FindAgentIDByName(_ context.Context, _, name string) (string, bool, error) {
	for _, a := range f.agents {
		if a.Name == name {
			return a.ID, true, nil
		}
	}
	return "", false, nil
}

func (f *fakeAgentDir) FindAgentIDByNameOrSlug(_ context.Context, _, nameOrSlug string) (string, bool, error) {
	for _, a := range f.agents {
		if a.Name == nameOrSlug || acpslug.FromName(a.Name) == strings.ToLower(nameOrSlug) {
			return a.ID, true, nil
		}
	}
	return "", false, nil
}

// stubAgentHandler records dispatch and returns canned JSON payloads.
type stubAgentHandler struct {
	listed        []string
	getCalled     bool
	ranCalled     bool
	listJSON      string
	availableJSON string
	defsJSON      string
	defJSON       string

	// RunAgentOnce scaffolding (per-agent MCP endpoint tests).
	runReply   string
	runErr     error
	runCalled  bool
	runCount   int
	runAgentID string
}

func (s *stubAgentHandler) ExecuteListAgents(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	s.listed = append(s.listed, "list")
	if s.listJSON == "" {
		s.listJSON = `[{"id":"` + agentA + `","name":"a"},{"id":"` + agentB + `","name":"b"},{"id":"` + agentC + `","name":"c"}]`
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: s.listJSON}}}, nil
}

func (s *stubAgentHandler) ExecuteGetAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	s.getCalled = true
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: `{"id":"` + agentC + `"}`}}}, nil
}

func (s *stubAgentHandler) ExecuteTriggerAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	s.ranCalled = true
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: `{"run_id":"r1"}`}}}, nil
}

func (s *stubAgentHandler) ExecuteListAvailableAgents(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	if s.availableJSON != "" {
		return &ToolResult{Content: []ContentBlock{{Type: "text", Text: s.availableJSON}}}, nil
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: `{"agents":[{"name":"a"},{"name":"b"},{"name":"c"}],"count":3}`}}}, nil
}

// The remaining interface methods are unused in these tests.
func (s *stubAgentHandler) ExecuteListAgentDefinitions(context.Context, string, map[string]any) (*ToolResult, error) {
	if s.defsJSON == "" {
		s.defsJSON = `[{"name":"alpha"},{"name":"gamma"}]`
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: s.defsJSON}}}, nil
}
func (s *stubAgentHandler) ExecuteGetAgentDefinition(context.Context, string, map[string]any) (*ToolResult, error) {
	if s.defJSON == "" {
		s.defJSON = `{"name":"gamma"}`
	}
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: s.defJSON}}}, nil
}
func (s *stubAgentHandler) ExecuteCreateAgentDefinition(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteUpdateAgentDefinition(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteDeleteAgentDefinition(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteCreateAgent(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteUpdateAgent(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteDeleteAgent(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteListAgentRuns(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteGetAgentRun(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteGetAgentRunMessages(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteGetAgentRunToolCalls(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteGetRunStatus(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteRememberStatus(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteListAgentQuestions(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteListProjectAgentQuestions(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteRespondToAgentQuestion(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteListAgentHooks(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteCreateAgentHook(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteDeleteAgentHook(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteListADKSessions(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteGetADKSession(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteACPListAgents(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteACPTriggerRun(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteACPGetRunStatus(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) ExecuteACPGetRunEvents(context.Context, string, map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentHandler) GetAgentToolDefinitions() []ToolDefinition { return nil }
func (s *stubAgentHandler) GetAgentToolDefinitionsForProject(context.Context, string) []ToolDefinition {
	return nil
}
func (s *stubAgentHandler) RunAgentOnce(_ context.Context, _, agentID, _ string, _ AgentRunBudget) (string, string, error) {
	s.runCalled = true
	s.runCount++
	s.runAgentID = agentID
	if s.runErr != nil {
		return "", "run-1", s.runErr
	}
	return s.runReply, "run-1", nil
}

// ============================================================================
// Pure helper tests
// ============================================================================

func TestNormalizeInstanceName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "valid", in: "  Team A  ", want: "Team A"},
		{name: "empty", in: "   ", wantErr: true},
		{name: "too long", in: string(make([]byte, 256)), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeInstanceName(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDedupeStrings(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, dedupeStrings([]string{"a", "b", "a"}))
	assert.Nil(t, dedupeStrings(nil))
}

func testLookup(defs ...ToolDefinition) toolLookupFunc {
	m := map[string]ToolDefinition{}
	for _, d := range defs {
		m[d.Name] = d
	}
	return func(name string) *ToolDefinition {
		if d, ok := m[name]; ok {
			return &d
		}
		return nil
	}
}

func TestNormalizeToolAllowlist(t *testing.T) {
	lookup := testLookup(
		ToolDefinition{Name: "entity-search", RequiredScope: "graph:read"},
		ToolDefinition{Name: "schema-list", RequiredScope: "schema:read"},
		ToolDefinition{Name: "hidden", AgentOnly: true, RequiredScope: "graph:read"},
	)

	t.Run("nil means unrestricted", func(t *testing.T) {
		got, err := normalizeToolAllowlist(nil, lookup)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("empty rejected", func(t *testing.T) {
		empty := []string{}
		_, err := normalizeToolAllowlist(&empty, lookup)
		assert.Error(t, err)
	})

	t.Run("unknown rejected", func(t *testing.T) {
		tools := []string{"nope"}
		_, err := normalizeToolAllowlist(&tools, lookup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nope")
	})

	t.Run("agent-only rejected", func(t *testing.T) {
		tools := []string{"hidden"}
		_, err := normalizeToolAllowlist(&tools, lookup)
		assert.Error(t, err)
	})

	t.Run("dedupe", func(t *testing.T) {
		tools := []string{"entity-search", "entity-search", "schema-list"}
		got, err := normalizeToolAllowlist(&tools, lookup)
		require.NoError(t, err)
		assert.Equal(t, []string{"entity-search", "schema-list"}, got)
	})
}

func TestDeriveScopesForToolNames(t *testing.T) {
	lookup := testLookup(
		ToolDefinition{Name: "entity-search", RequiredScope: "graph:read"},
		ToolDefinition{Name: "schema-list", RequiredScope: "schema:read"},
		ToolDefinition{Name: "scopeless"},
	)
	got, err := deriveScopesForToolNames([]string{"entity-search", "schema-list", "scopeless"}, lookup)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"graph:read", "schema:read", "projects:read"}, got)
}

func TestToolCategoryDerived(t *testing.T) {
	tests := map[string]string{
		"graph:read":     "Graph",
		"schema:write":   "Schema",
		"search":         "Search",
		"branches:read":  "Branches",
		"journal:read":   "Journal",
		"skills:read":    "Skills",
		"documents:read": "Documents",
		"agents:read":    "Agents",
		"admin":          "Admin",
	}
	for scope, want := range tests {
		assert.Equal(t, want, toolCategoryDerived(ToolDefinition{Name: "x", RequiredScope: scope}), scope)
	}
}

func TestShareInstanceStatus(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	assert.Equal(t, "active", shareInstanceStatus(nil, nil, nil, now))
	assert.Equal(t, "revoked", shareInstanceStatus(&past, nil, nil, now))
	assert.Equal(t, "revoked", shareInstanceStatus(nil, &past, nil, now))
	assert.Equal(t, "expired", shareInstanceStatus(nil, nil, &past, now))
	assert.Equal(t, "active", shareInstanceStatus(nil, nil, &future, now))
}

func TestInstanceToolPredicates(t *testing.T) {
	tools := []ToolDefinition{{Name: "entity-search"}, {Name: "schema-list"}}
	scoped := &InstanceScope{HasToolAllowlist: true, AllowedTools: []string{"entity-search"}}

	assert.True(t, InstanceAllowsTool(nil, "anything"))
	assert.True(t, InstanceAllowsTool(scoped, "entity-search"))
	assert.False(t, InstanceAllowsTool(scoped, "schema-list"))

	filtered := FilterToolsForInstance(tools, scoped)
	require.Len(t, filtered, 1)
	assert.Equal(t, "entity-search", filtered[0].Name)

	assert.Equal(t, tools, FilterToolsForInstance(tools, nil))
}

// ============================================================================
// Service lifecycle tests
// ============================================================================

func newTestService(store shareInstanceStore, tok shareTokenService, dir agentDirectory) *Service {
	return &Service{shareInstances: store, shareTokens: tok, agentDir: dir}
}

func TestCreateShareInstance(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt_raw", createID: "tok-1"}
	svc := newTestService(store, tok, &fakeAgentDir{})

	tools := []string{"entity-search", "schema-list"}
	resp, err := svc.CreateShareInstance(context.Background(), "proj", "user", "http://localhost:8095", CreateShareInstanceRequest{
		Name:        "Team A",
		Description: ptr("desc"),
		Tools:       &tools,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "emt_raw", resp.Token)
	assert.Equal(t, "http://localhost:8095/api/mcp", resp.MCPURL)
	assert.True(t, resp.Tools != nil && len(*resp.Tools) == 2)
	assert.ElementsMatch(t, []string{"graph:read", "schema:read", "projects:read"}, tok.createdScopes)
	assert.Len(t, store.byID, 1)
}

func TestCreateShareInstanceNoTools(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt_raw", createID: "tok-1"}
	svc := newTestService(store, tok, &fakeAgentDir{})

	resp, err := svc.CreateShareInstance(context.Background(), "proj", "user", "http://h", CreateShareInstanceRequest{Name: "All"})
	require.NoError(t, err)
	assert.Nil(t, resp.Tools)
	// A null (unrestricted) allowlist grants the legacy read-only scope set so
	// the token is actually usable.
	assert.ElementsMatch(t, readOnlyMCPScopes, tok.createdScopes)
}

func TestCreateShareInstanceEmptyToolsRejected(t *testing.T) {
	store := newFakeShareStore()
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})
	empty := []string{}
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{Name: "X", Tools: &empty})
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 422, appErr.HTTPStatus)
	assert.Empty(t, store.byID)
}

func TestCreateShareInstanceUnknownToolRejected(t *testing.T) {
	store := newFakeShareStore()
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})
	tools := []string{"definitely-not-a-tool"}
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{Name: "X", Tools: &tools})
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 422, appErr.HTTPStatus)
}

func TestCreateShareInstanceNonAdmin(t *testing.T) {
	store := newFakeShareStore()
	svc := newTestService(store, &fakeTokenSvc{role: "project_user"}, &fakeAgentDir{})
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{Name: "X"})
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 403, appErr.HTTPStatus)
}

func TestCreateShareInstanceDuplicateName(t *testing.T) {
	store := newFakeShareStore()
	store.byID["existing"] = &MCPShareInstance{ID: "existing", ProjectID: "proj", Name: "Team A", TokenID: "t0"}
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{Name: "team a"})
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.HTTPStatus)
}

func TestUpdateShareInstanceScopesFollowAllowlist(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{}
	svc := newTestService(store, tok, &fakeAgentDir{})

	tools := []string{"entity-search"}
	dto, err := svc.UpdateShareInstance(context.Background(), "proj", "user", "i1", UpdateShareInstanceRequest{Tools: &tools})
	require.NoError(t, err)
	require.NotNil(t, dto)
	assert.ElementsMatch(t, []string{"graph:read", "projects:read"}, tok.updateScopes)
	assert.Equal(t, "tok-1", store.byID["i1"].TokenID, "token secret/identity unchanged")
}

func TestUpdateRevokedRejected(t *testing.T) {
	store := newFakeShareStore()
	revoked := time.Now()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1", RevokedAt: &revoked}
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})
	name := "New"
	_, err := svc.UpdateShareInstance(context.Background(), "proj", "user", "i1", UpdateShareInstanceRequest{Name: &name})
	require.Error(t, err)
}

func TestRevokeShareInstanceIdempotent(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1"}
	tok := &fakeTokenSvc{}
	svc := newTestService(store, tok, &fakeAgentDir{})

	require.NoError(t, svc.RevokeShareInstance(context.Background(), "proj", "user", "i1"))
	assert.NotNil(t, store.byID["i1"].RevokedAt)
	assert.Equal(t, []string{"tok-1"}, tok.revoked)

	// Second revoke is a no-op success.
	require.NoError(t, svc.RevokeShareInstance(context.Background(), "proj", "user", "i1"))
	assert.Equal(t, []string{"tok-1"}, tok.revoked)
}

func TestRotateShareInstance(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "old",
		AllowedTools: []string{"entity-search"},
	}
	tok := &fakeTokenSvc{regenerateID: "new", regenerateToken: "emt_new"}
	svc := newTestService(store, tok, &fakeAgentDir{})

	resp, err := svc.RotateShareInstance(context.Background(), "proj", "user", "i1", "http://h")
	require.NoError(t, err)
	assert.True(t, tok.regenerateCalled)
	assert.Equal(t, "emt_new", resp.Token)
	assert.Equal(t, "new", store.byID["i1"].TokenID)
	assert.Equal(t, []string{"entity-search"}, store.byID["i1"].AllowedTools)
}

func TestListShareInstancesIncludesLegacy(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1"}
	store.legacy = []*LegacyTokenRef{
		{ID: "legacy-1", Name: "MCP Read-Only Share — 2020"},
		{ID: "tok-1", Name: "MCP Share: Team A"}, // bound -> excluded
	}
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})

	resp, err := svc.ListShareInstances(context.Background(), "proj", "user")
	require.NoError(t, err)
	require.Len(t, resp.Instances, 2)
	assert.Equal(t, 2, resp.Total)

	var legacy *ShareInstanceDTO
	for i := range resp.Instances {
		if resp.Instances[i].IsLegacy {
			legacy = &resp.Instances[i]
		}
	}
	require.NotNil(t, legacy)
	assert.Equal(t, "legacy-1", legacy.ID)
}

func TestResolveInstanceScope(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1",
		AllowedTools:  []string{"entity-search"},
		AllowedAgents: []uuid.UUID{uuid.MustParse(agentA)},
	}
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})

	t.Run("present instance", func(t *testing.T) {
		scope, err := svc.ResolveInstanceScope(context.Background(), "tok-1")
		require.NoError(t, err)
		require.NotNil(t, scope)
		assert.True(t, scope.HasToolAllowlist)
		assert.True(t, scope.HasAgentAllowlist)
	})

	t.Run("absent instance is legacy", func(t *testing.T) {
		s1, err := svc.ResolveInstanceScope(context.Background(), "unknown")
		require.NoError(t, err)
		assert.Nil(t, s1)
		s2, err := svc.ResolveInstanceScope(context.Background(), "")
		require.NoError(t, err)
		assert.Nil(t, s2)
	})
}

func TestBuildToolCatalogExcludesAgentOnlyAndSorts(t *testing.T) {
	svc := &Service{}
	got := BuildToolCatalog(context.Background(), svc, "")
	require.NotEmpty(t, got)
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if prev.Category == cur.Category {
			assert.LessOrEqual(t, prev.Name, cur.Name)
		} else {
			assert.Less(t, prev.Category, cur.Category)
		}
	}
}

// ============================================================================
// Agent scoping tests
// ============================================================================

func agentScope(ids ...string) *InstanceScope {
	return &InstanceScope{HasAgentAllowlist: true, AllowedAgents: ids}
}

func TestAgentListFiltered(t *testing.T) {
	h := &stubAgentHandler{}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{}}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA, agentB))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-list", nil)
	require.NoError(t, err)

	var arr []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &arr))
	ids := []string{}
	for _, item := range arr {
		ids = append(ids, item["id"].(string))
	}
	assert.ElementsMatch(t, []string{agentA, agentB}, ids)
}

func TestAgentGetNonAllowedRejected(t *testing.T) {
	h := &stubAgentHandler{}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{}}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-get", map[string]any{"agent_id": agentC})
	require.NoError(t, err)
	assert.False(t, h.getCalled, "underlying handler must not run")
	assert.Contains(t, res.Content[0].Text, "agent not found")
}

func TestTriggerAgentNonAllowedRejected(t *testing.T) {
	h := &stubAgentHandler{}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "alpha"}, {ID: agentC, Name: "gamma"}}}}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	res, err := svc.ExecuteTool(ctx, "proj", "trigger_agent", map[string]any{"agent_name": "gamma"})
	require.NoError(t, err)
	assert.False(t, h.ranCalled, "run must not start")
	assert.Contains(t, res.Content[0].Text, "agent not found")
}

func TestACPSlugTriggerNonAllowedRejected(t *testing.T) {
	h := &stubAgentHandler{}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "My Agent"}, {ID: agentC, Name: "Gamma Agent"}}}}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	// acp-trigger-run passes the ACP slug, not the raw agent name.
	res, err := svc.ExecuteTool(ctx, "proj", "acp-trigger-run", map[string]any{"agent_name": "gamma-agent"})
	require.NoError(t, err)
	assert.Equal(t, 0, len(h.listed), "underlying handler must not run")
	assert.Contains(t, res.Content[0].Text, "agent not found")
}

func TestAgentDeniedToolsWhenAgentAllowlistActive(t *testing.T) {
	// Run-inspection and agent-mutation tools cannot be mapped to the
	// runtime-agent allowlist, so the central ExecuteTool guard rejects them.
	for _, tool := range []string{"agent-run-get", "agent-hook-create", "agent-question-respond"} {
		h := &stubAgentHandler{}
		svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{}}
		ctx := WithInstanceScope(context.Background(), agentScope(agentA))
		_, err := svc.ExecuteTool(ctx, "proj", tool, map[string]any{})
		require.Error(t, err, tool)
	}
}

func TestAgentDefinitionListFiltered(t *testing.T) {
	svc := &Service{
		agentToolHandler: &stubAgentHandler{},
		agentDir:         &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "alpha"}, {ID: agentC, Name: "gamma"}}},
	}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-def-list", nil)
	require.NoError(t, err)
	var arr []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &arr))
	require.Len(t, arr, 1)
	assert.Equal(t, "alpha", arr[0]["name"])
}

func TestAgentDefinitionGetNonAllowedRejected(t *testing.T) {
	svc := &Service{
		agentToolHandler: &stubAgentHandler{defJSON: `{"name":"gamma"}`},
		agentDir:         &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "alpha"}, {ID: agentC, Name: "gamma"}}},
	}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-def-get", map[string]any{"definition_id": "d1"})
	require.NoError(t, err)
	assert.Contains(t, res.Content[0].Text, "not found")
}

func TestAgentListFailsClosedOnMalformedResult(t *testing.T) {
	h := &stubAgentHandler{listJSON: "not-json"}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{}}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-list", nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", res.Content[0].Text)
}

func TestAgentListAvailableFailsClosedOnMalformedResult(t *testing.T) {
	svc := &Service{
		agentToolHandler: &stubAgentHandler{availableJSON: "not-json"},
		agentDir:         &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "alpha"}}},
	}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-list-available", nil)
	require.NoError(t, err)
	assert.JSONEq(t, emptyAvailableAgentsText, res.Content[0].Text)
}

func TestNullAgentAllowlistExposesAll(t *testing.T) {
	h := &stubAgentHandler{}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{}}
	res, err := svc.ExecuteTool(context.Background(), "proj", "agent-list", nil)
	require.NoError(t, err)
	var arr []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &arr))
	assert.Len(t, arr, 3)
}

func TestDeletedAllowedAgentDoesNotError(t *testing.T) {
	h := &stubAgentHandler{}
	svc := &Service{agentToolHandler: h, agentDir: &fakeAgentDir{}}
	ctx := WithInstanceScope(context.Background(), agentScope(agentA, agentDeleted))
	res, err := svc.ExecuteTool(ctx, "proj", "agent-list", nil)
	require.NoError(t, err)
	var arr []map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content[0].Text), &arr))
	require.Len(t, arr, 1)
	assert.Equal(t, agentA, arr[0]["id"])
}

func TestFilterToolsForInstanceRemovesAgentDeniedTools(t *testing.T) {
	tools := []ToolDefinition{{Name: "agent-run-get"}, {Name: "agent-hook-create"}, {Name: "agent-list"}, {Name: "agent-def-list"}}
	scope := &InstanceScope{HasAgentAllowlist: true, AllowedAgents: []string{agentA}}
	got := FilterToolsForInstance(tools, scope)
	require.Len(t, got, 2)
	assert.Equal(t, "agent-list", got[0].Name)
	assert.Equal(t, "agent-def-list", got[1].Name)
}

func TestCreateShareInstanceUnknownAgentRejected(t *testing.T) {
	store := newFakeShareStore()
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "alpha"}}})
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{
		Name:   "X",
		Agents: []string{"ghost"},
	})
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 422, appErr.HTTPStatus)
}

func TestCreateShareInstanceAgentsDeduped(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt", createID: "tok-1"}
	svc := newTestService(store, tok, &fakeAgentDir{agents: []AgentRef{{ID: agentA, Name: "alpha"}}})
	resp, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{
		Name:   "X",
		Agents: []string{agentA, agentA},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Agents)
	assert.Equal(t, []string{agentA}, *resp.Agents)
}

func TestResolveInstanceScopeRevokedIsNil(t *testing.T) {
	store := newFakeShareStore()
	revoked := time.Now()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"}, RevokedAt: &revoked,
	}
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})
	scope, err := svc.ResolveInstanceScope(context.Background(), "tok-1")
	require.NoError(t, err)
	assert.Nil(t, scope)
}

func TestToolAllowlistRejectsAgentExecutionTool(t *testing.T) {
	store := newFakeShareStore()
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})
	tools := []string{"entity-search", "trigger_agent"}
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{Name: "X", Tools: &tools})
	require.Error(t, err)
	var appErr *apperror.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 422, appErr.HTTPStatus)
	assert.Empty(t, store.byID)

	// Mutation tools are likewise rejected.
	mut := []string{"agent-create"}
	_, err = svc.CreateShareInstance(context.Background(), "proj", "user", "", CreateShareInstanceRequest{Name: "Y", Tools: &mut})
	require.Error(t, err)
	assert.Empty(t, store.byID)
}

func TestCatalogExcludesAgentExecutionTools(t *testing.T) {
	got := BuildToolCatalog(context.Background(), &Service{}, "")
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name] = true
	}
	assert.False(t, names["trigger_agent"])
	assert.False(t, names["acp-trigger-run"])
	assert.False(t, names["agent-create"])
	assert.False(t, names["agent-def-delete"])
}

// TestExecuteToolEnforcesInstanceAllowlist is the C1 defense-in-depth check:
// even a direct ExecuteTool call (e.g. from the ADK ToolPool) is denied when the
// context carries a restrictive instance scope.
func TestExecuteToolEnforcesInstanceAllowlist(t *testing.T) {
	svc := &Service{}
	scope := &InstanceScope{HasToolAllowlist: true, AllowedTools: []string{"entity-search"}}
	ctx := WithInstanceScope(context.Background(), scope)
	_, err := svc.ExecuteTool(ctx, "proj", "project-get", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

// TestExecuteToolDeniesAgentScopeTools covers tools that are dispatched directly
// (not via delegateAgentTool) so the central ExecuteTool guard is required.
func TestExecuteToolDeniesAgentScopeTools(t *testing.T) {
	svc := &Service{}
	scope := &InstanceScope{HasAgentAllowlist: true, AllowedAgents: []string{agentA}}
	ctx := WithInstanceScope(context.Background(), scope)
	for _, tool := range []string{"agent-run-get", "agent-hook-create", "agent-question-respond", "acp-get-run-status"} {
		_, err := svc.ExecuteTool(ctx, "proj", tool, nil)
		require.Error(t, err, tool)
		assert.Contains(t, err.Error(), "not allowed", tool)
	}
}

func TestInstanceDeniesToolPredicate(t *testing.T) {
	toolScope := &InstanceScope{HasToolAllowlist: true, AllowedTools: []string{"entity-search"}}
	assert.True(t, InstanceDeniesTool(toolScope, "schema-list"))
	assert.False(t, InstanceDeniesTool(toolScope, "entity-search"))

	agentScoped := &InstanceScope{HasAgentAllowlist: true, AllowedAgents: []string{agentA}}
	assert.True(t, InstanceDeniesTool(agentScoped, "agent-run-get"))
	assert.True(t, InstanceDeniesTool(agentScoped, "agent-hook-create"))
	assert.False(t, InstanceDeniesTool(agentScoped, "agent-list"))
	// Agent-definition reads are not denied but are result-filtered.
	assert.False(t, InstanceDeniesTool(agentScoped, "agent-def-list"))
	assert.False(t, InstanceDeniesTool(nil, "anything"))
}

func TestRotateFailureLeavesInstanceUnchanged(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "old"}
	tok := &fakeTokenSvc{regenerateErr: errors.New("boom")}
	svc := newTestService(store, tok, &fakeAgentDir{})
	_, err := svc.RotateShareInstance(context.Background(), "proj", "user", "i1", "http://h")
	require.Error(t, err)
	assert.Equal(t, "old", store.byID["i1"].TokenID)
}

func ptr(s string) *string { return &s }

// TestShareInstanceEndToEnd exercises the full domain flow: create an instance
// with a two-tool allowlist, resolve it by the minted token, verify listing and
// call gating, then revoke and verify the token no longer resolves.
func TestShareInstanceEndToEnd(t *testing.T) {
	store := newFakeShareStore()
	tok := &fakeTokenSvc{createToken: "emt_live", createID: "tok-live"}
	svc := newTestService(store, tok, &fakeAgentDir{})

	tools := []string{"entity-search", "schema-list"}
	_, err := svc.CreateShareInstance(context.Background(), "proj", "user", "http://h", CreateShareInstanceRequest{
		Name:  "E2E",
		Tools: &tools,
	})
	require.NoError(t, err)

	// Connect as the minted token.
	scope, err := svc.ResolveInstanceScope(context.Background(), "tok-live")
	require.NoError(t, err)
	require.NotNil(t, scope)

	all := []ToolDefinition{{Name: "entity-search"}, {Name: "schema-list"}, {Name: "graph-traverse"}}
	listed := FilterToolsForInstance(all, scope)
	require.Len(t, listed, 2)

	assert.True(t, InstanceAllowsTool(scope, "entity-search"))
	assert.False(t, InstanceAllowsTool(scope, "graph-traverse"))

	// Revoke invalidates the token.
	instances, err := svc.ListShareInstances(context.Background(), "proj", "user")
	require.NoError(t, err)
	require.Len(t, instances.Instances, 1)
	require.NoError(t, svc.RevokeShareInstance(context.Background(), "proj", "user", instances.Instances[0].ID))

	afterRevoke, err := svc.ResolveInstanceScope(context.Background(), "tok-live")
	require.NoError(t, err)
	assert.Nil(t, afterRevoke)
	assert.Contains(t, tok.revoked, "tok-live")
}

// ============================================================================
// Security-hardening tests
// ============================================================================

// TestResolveInstanceScopeStoreErrorFailsClosed verifies that a storage error
// during allowlist resolution is surfaced as an error rather than being
// silently treated as "unrestricted".
func TestResolveInstanceScopeStoreErrorFailsClosed(t *testing.T) {
	store := newFakeShareStore()
	store.getByTokenErr = errors.New("db down")
	svc := newTestService(store, &fakeTokenSvc{}, &fakeAgentDir{})

	scope, err := svc.ResolveInstanceScope(context.Background(), "tok-1")
	require.Error(t, err)
	assert.Nil(t, scope)
}

// TestInstanceRestrictsContent covers the resources/prompts gating predicate.
func TestInstanceRestrictsContent(t *testing.T) {
	assert.False(t, InstanceRestrictsContent(nil))
	assert.False(t, InstanceRestrictsContent(&InstanceScope{}))
	assert.True(t, InstanceRestrictsContent(&InstanceScope{HasToolAllowlist: true}))
	assert.True(t, InstanceRestrictsContent(&InstanceScope{HasAgentAllowlist: true}))
}

// TestNormalizeToolAllowlistRejectsAdminScope ensures an admin-scoped tool can
// never be allowlisted (which would derive the "admin" scope onto a share token
// and re-enable token chaining).
func TestNormalizeToolAllowlistRejectsAdminScope(t *testing.T) {
	lookup := testLookup(
		ToolDefinition{Name: "trace-list", RequiredScope: "admin"},
		ToolDefinition{Name: "account-key-list", RequiredScope: "account:read"},
		ToolDefinition{Name: "entity-search", RequiredScope: "graph:read"},
	)
	for _, name := range []string{"trace-list", "account-key-list"} {
		tools := []string{name}
		_, err := normalizeToolAllowlist(&tools, lookup)
		require.Error(t, err, name)
	}

	// deriveScopesForToolNames must also reject admin scopes defensively.
	_, err := deriveScopesForToolNames([]string{"trace-list"}, lookup)
	require.Error(t, err)
}

// TestBuildToolCatalogExcludesAdminScopedTools verifies the catalog never offers
// a tool that would derive an administrative scope.
func TestBuildToolCatalogExcludesAdminScopedTools(t *testing.T) {
	got := BuildToolCatalog(context.Background(), &Service{}, "")
	for _, tool := range got {
		assert.NotEqual(t, "admin", tool.RequiredScope, "tool %s must not be includable", tool.Name)
	}
}

// TestUpdateShareInstanceRollsBackAllowlistOnScopeFailure verifies atomicity:
// when the token-scope write fails after the allowlist was persisted, the prior
// allowlist is restored so scopes and allowlist cannot diverge.
func TestUpdateShareInstanceRollsBackAllowlistOnScopeFailure(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	tok := &fakeTokenSvc{updateErr: errors.New("scope write failed")}
	svc := newTestService(store, tok, &fakeAgentDir{})

	tools := []string{"schema-list"}
	_, err := svc.UpdateShareInstance(context.Background(), "proj", "user", "i1", UpdateShareInstanceRequest{Tools: &tools})
	require.Error(t, err)
	assert.Equal(t, []string{"entity-search"}, store.byID["i1"].AllowedTools,
		"allowlist must be rolled back to the prior value")
}

// TestUpdateShareInstanceNormalizesBeforePersisting verifies that an agent
// normalization failure never writes token scopes (no scope/allowlist divergence).
func TestUpdateShareInstanceNormalizesBeforePersisting(t *testing.T) {
	store := newFakeShareStore()
	store.byID["i1"] = &MCPShareInstance{
		ID: "i1", ProjectID: "proj", Name: "Team A", TokenID: "tok-1",
		AllowedTools: []string{"entity-search"},
	}
	tok := &fakeTokenSvc{}
	svc := newTestService(store, tok, &fakeAgentDir{})

	tools := []string{"schema-list"}
	badAgents := []string{"ghost"}
	_, err := svc.UpdateShareInstance(context.Background(), "proj", "user", "i1", UpdateShareInstanceRequest{
		Tools:  &tools,
		Agents: &badAgents,
	})
	require.Error(t, err)
	assert.Nil(t, tok.updateScopes, "token scopes must not be written when validation fails")
	assert.Equal(t, []string{"entity-search"}, store.byID["i1"].AllowedTools)
}

// TestSetSessionTitleEnforcesInstanceAllowlist is the fix for the in-execution
// allowlist hole: hidden built-ins are checked before dispatch.
func TestSetSessionTitleEnforcesInstanceAllowlist(t *testing.T) {
	svc := &Service{}
	restricted := &InstanceScope{HasToolAllowlist: true, AllowedTools: []string{"entity-search"}}
	ctx := WithInstanceScope(context.Background(), restricted)
	_, err := svc.ExecuteTool(ctx, "proj", "set_session_title", map[string]any{"title": "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")

	// Unrestricted (nil scope) keeps working: dispatch reaches the handler.
	_, err = svc.ExecuteTool(context.Background(), "proj", "set_session_title", nil)
	require.NoError(t, err)
}
