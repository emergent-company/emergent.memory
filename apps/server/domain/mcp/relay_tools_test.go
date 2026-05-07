package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// relaySessionTools
// =============================================================================

func TestRelaySessionTools_NilSession(t *testing.T) {
	result := relaySessionTools(nil)
	assert.Nil(t, result)
}

func TestRelaySessionTools_NilTools(t *testing.T) {
	sess := &RelaySession{InstanceID: "inst", Tools: nil}
	result := relaySessionTools(sess)
	assert.Nil(t, result)
}

func TestRelaySessionTools_MissingToolsKey(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "inst",
		Tools:      map[string]any{"other": "value"},
	}
	result := relaySessionTools(sess)
	assert.Nil(t, result)
}

func TestRelaySessionTools_EmptyToolsList(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "inst",
		Tools:      map[string]any{"tools": []any{}},
	}
	result := relaySessionTools(sess)
	assert.Empty(t, result)
}

func TestRelaySessionTools_SkipsEntriesWithNoName(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "inst",
		Tools: map[string]any{
			"tools": []any{
				map[string]any{"description": "no name tool"},
				map[string]any{"name": "", "description": "empty name"},
			},
		},
	}
	result := relaySessionTools(sess)
	assert.Empty(t, result)
}

func TestRelaySessionTools_PrefixesToolNames(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "mcj-mini",
		Tools: map[string]any{
			"tools": []any{
				map[string]any{
					"name":        "echo_text",
					"description": "Echoes text back",
				},
				map[string]any{
					"name":        "search_repos",
					"description": "Search GitHub repos",
				},
			},
		},
	}
	result := relaySessionTools(sess)
	require.Len(t, result, 2)
	assert.Equal(t, "mcj-mini_echo_text", result[0].Name)
	assert.Equal(t, "Echoes text back", result[0].Description)
	assert.Equal(t, "mcj-mini_search_repos", result[1].Name)
}

func TestRelaySessionTools_PreservesInputSchema(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "relay",
		Tools: map[string]any{
			"tools": []any{
				map[string]any{
					"name":        "my_tool",
					"description": "A tool",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"text": map[string]any{"type": "string", "description": "Input text"},
						},
						"required": []any{"text"},
					},
				},
			},
		},
	}
	result := relaySessionTools(sess)
	require.Len(t, result, 1)
	td := result[0]
	assert.Equal(t, "relay_my_tool", td.Name)
	assert.Equal(t, "object", td.InputSchema.Type)
	props := td.InputSchema.Properties
	require.Contains(t, props, "text")
	assert.Equal(t, "string", props["text"].Type)
	assert.Contains(t, td.InputSchema.Required, "text")
}

func TestRelaySessionTools_SkipsNonMapEntries(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "relay",
		Tools: map[string]any{
			"tools": []any{
				"not-a-map",
				42,
				map[string]any{"name": "valid_tool"},
			},
		},
	}
	result := relaySessionTools(sess)
	require.Len(t, result, 1)
	assert.Equal(t, "relay_valid_tool", result[0].Name)
}

func TestRelaySessionTools_ToolsNotSlice(t *testing.T) {
	sess := &RelaySession{
		InstanceID: "relay",
		Tools:      map[string]any{"tools": "not-a-slice"},
	}
	result := relaySessionTools(sess)
	assert.Nil(t, result)
}

// =============================================================================
// GetToolDefinitionsForProject — relay tools included
// =============================================================================

// mockRelayProvider is a simple in-memory RelayToolProvider for testing.
type mockRelayProvider struct {
	sessions map[string][]*RelaySession
}

func (m *mockRelayProvider) ListByProject(projectID string) []*RelaySession {
	return m.sessions[projectID]
}

func (m *mockRelayProvider) CallTool(_ context.Context, _, _, _ string, _ map[string]any) (map[string]any, error) {
	return map[string]any{"result": "ok"}, nil
}

func TestGetToolDefinitionsForProject_NoRelay(t *testing.T) {
	svc := &Service{}
	tools := svc.GetToolDefinitionsForProject(context.Background(), "")
	// Should fall back to GetToolDefinitions without panic
	assert.NotNil(t, tools)
}

// stubAgentToolHandler is a minimal AgentToolHandler that does nothing,
// used to satisfy the non-nil check in GetToolDefinitionsForProject.
type stubAgentToolHandler struct{}

func (s *stubAgentToolHandler) ExecuteListAgentDefinitions(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetAgentDefinition(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteCreateAgentDefinition(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteUpdateAgentDefinition(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteDeleteAgentDefinition(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListAgents(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteCreateAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteUpdateAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteDeleteAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteTriggerAgent(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListAgentRuns(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetAgentRun(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetAgentRunMessages(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetAgentRunToolCalls(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetRunStatus(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListAvailableAgents(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListAgentQuestions(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListProjectAgentQuestions(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteRespondToAgentQuestion(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListAgentHooks(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteCreateAgentHook(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteDeleteAgentHook(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteListADKSessions(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteGetADKSession(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteACPListAgents(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteACPTriggerRun(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteACPGetRunStatus(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) ExecuteACPGetRunEvents(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
	return nil, nil
}
func (s *stubAgentToolHandler) GetAgentToolDefinitions() []ToolDefinition { return nil }
func (s *stubAgentToolHandler) GetAgentToolDefinitionsForProject(_ context.Context, _ string) []ToolDefinition {
	return nil
}

func TestGetToolDefinitionsForProject_RelayToolsAppended(t *testing.T) {
	projectID := uuid.New().String()
	relay := &mockRelayProvider{
		sessions: map[string][]*RelaySession{
			projectID: {
				{
					ProjectID:  projectID,
					InstanceID: "diane",
					Tools: map[string]any{
						"tools": []any{
							map[string]any{"name": "list_files", "description": "List files"},
						},
					},
				},
			},
		},
	}

	svc := &Service{}
	svc.SetRelayProvider(relay)
	svc.SetAgentToolHandler(&stubAgentToolHandler{})

	tools := svc.GetToolDefinitionsForProject(context.Background(), projectID)
	var found bool
	for _, td := range tools {
		if td.Name == "diane_list_files" {
			found = true
			assert.Equal(t, "List files", td.Description)
		}
	}
	assert.True(t, found, "expected relay tool diane_list_files in definitions")
}

func TestGetToolDefinitionsForProject_NoRelaySessionsForProject(t *testing.T) {
	projectID := uuid.New().String()
	relay := &mockRelayProvider{
		sessions: map[string][]*RelaySession{}, // no sessions
	}
	svc := &Service{}
	svc.SetRelayProvider(relay)

	tools := svc.GetToolDefinitionsForProject(context.Background(), projectID)
	for _, td := range tools {
		assert.False(t, len(td.Name) > 0 && td.Name[0] == '_', "unexpected tool with leading underscore: %s", td.Name)
	}
}

// =============================================================================
// executeGetSessionMessages — validation
// =============================================================================

func TestExecuteGetSessionMessages_NilService(t *testing.T) {
	svc := &Service{graphSessionSvc: nil}
	_, err := svc.executeGetSessionMessages(context.Background(), uuid.New().String(), map[string]any{
		"session_id": uuid.New().String(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session service not available")
}

func TestExecuteGetSessionMessages_InvalidProjectID(t *testing.T) {
	svc := &Service{graphSessionSvc: nil}
	_, err := svc.executeGetSessionMessages(context.Background(), "not-a-uuid", map[string]any{
		"session_id": uuid.New().String(),
	})
	require.Error(t, err)
	// will hit nil service check first since graphSessionSvc is nil
	assert.Contains(t, err.Error(), "session service not available")
}

func TestExecuteGetSessionMessages_MissingSessionID(t *testing.T) {
	// We need a non-nil graphSessionSvc to get past the nil check.
	// Use a minimal stub: graphSessionSvc field accepts *graph.SessionService.
	// Since we can't construct one without a DB, test only the nil-service guard path.
	svc := &Service{graphSessionSvc: nil}
	_, err := svc.executeGetSessionMessages(context.Background(), uuid.New().String(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session service not available")
}
