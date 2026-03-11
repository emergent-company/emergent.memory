package mcp

import (
	"context"
	"fmt"
)

// ============================================================================
// Agent Questions, Hooks, and ADK Sessions Tool Definitions
// ============================================================================

func agentExtToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		// --- Questions ---
		{
			Name:        "list_agent_questions",
			Description: "List all questions asked by an agent during a specific run. Returns question text, status, and any response.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"run_id": {
						Type:        "string",
						Description: "UUID of the agent run",
					},
				},
				Required: []string{"run_id"},
			},
		},
		{
			Name:        "list_project_agent_questions",
			Description: "List agent questions across all runs in the current project. Optionally filter by status.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"status": {
						Type:        "string",
						Description: "Filter by question status",
						Enum:        []string{"pending", "answered", "expired", "cancelled"},
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "respond_to_agent_question",
			Description: "Submit a response to a pending agent question. The agent will be resumed with the provided answer.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"question_id": {
						Type:        "string",
						Description: "UUID of the agent question to respond to",
					},
					"response": {
						Type:        "string",
						Description: "The response text to send to the agent",
					},
				},
				Required: []string{"question_id", "response"},
			},
		},
		// --- Hooks ---
		{
			Name:        "list_agent_hooks",
			Description: "List all webhook hooks configured for an agent.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "UUID of the agent",
					},
				},
				Required: []string{"agent_id"},
			},
		},
		{
			Name:        "create_agent_hook",
			Description: "Create a new webhook hook for an agent. Returns the hook id and a one-time token for authenticating webhook calls.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"agent_id": {
						Type:        "string",
						Description: "UUID of the agent to add the hook to",
					},
					"label": {
						Type:        "string",
						Description: "Human-readable label for this hook",
					},
				},
				Required: []string{"agent_id", "label"},
			},
		},
		{
			Name:        "delete_agent_hook",
			Description: "Delete a webhook hook by its ID.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"hook_id": {
						Type:        "string",
						Description: "UUID of the hook to delete",
					},
				},
				Required: []string{"hook_id"},
			},
		},
		// --- ADK Sessions ---
		{
			Name:        "list_adk_sessions",
			Description: "List ADK (Agent Development Kit) sessions for the current project. Returns session IDs, app names, user IDs, state, and timestamps.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of sessions to return (default 50)",
					},
					"offset": {
						Type:        "integer",
						Description: "Number of sessions to skip for pagination (default 0)",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "get_adk_session",
			Description: "Get a single ADK session by its ID, including all events (messages, tool calls, etc.).",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"session_id": {
						Type:        "string",
						Description: "ID of the ADK session",
					},
				},
				Required: []string{"session_id"},
			},
		},
	}
}

// ============================================================================
// Agent Extension Tool Handlers (delegated to AgentToolHandler)
// ============================================================================

// requireAgentHandler returns an error if the agent tool handler is not configured.
// Call this at the start of every executeXxx function in this file.
func (s *Service) requireAgentHandler(toolName string) error {
	if s.agentToolHandler == nil {
		return fmt.Errorf("%s: agent tools not available: handler not configured", toolName)
	}
	return nil
}

func (s *Service) executeListAgentQuestions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("list_agent_questions"); err != nil {
		return nil, err
	}
	runID, _ := args["run_id"].(string)
	if runID == "" {
		return nil, fmt.Errorf("list_agent_questions: 'run_id' is required")
	}
	return s.agentToolHandler.ExecuteListAgentQuestions(ctx, projectID, args)
}

func (s *Service) executeListProjectAgentQuestions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("list_project_agent_questions"); err != nil {
		return nil, err
	}
	return s.agentToolHandler.ExecuteListProjectAgentQuestions(ctx, projectID, args)
}

func (s *Service) executeRespondToAgentQuestion(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("respond_to_agent_question"); err != nil {
		return nil, err
	}
	questionID, _ := args["question_id"].(string)
	response, _ := args["response"].(string)
	if questionID == "" {
		return nil, fmt.Errorf("respond_to_agent_question: 'question_id' is required")
	}
	if response == "" {
		return nil, fmt.Errorf("respond_to_agent_question: 'response' is required")
	}
	return s.agentToolHandler.ExecuteRespondToAgentQuestion(ctx, projectID, args)
}

func (s *Service) executeListAgentHooks(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("list_agent_hooks"); err != nil {
		return nil, err
	}
	agentID, _ := args["agent_id"].(string)
	if agentID == "" {
		return nil, fmt.Errorf("list_agent_hooks: 'agent_id' is required")
	}
	return s.agentToolHandler.ExecuteListAgentHooks(ctx, projectID, args)
}

func (s *Service) executeCreateAgentHook(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("create_agent_hook"); err != nil {
		return nil, err
	}
	agentID, _ := args["agent_id"].(string)
	label, _ := args["label"].(string)
	if agentID == "" {
		return nil, fmt.Errorf("create_agent_hook: 'agent_id' is required")
	}
	if label == "" {
		return nil, fmt.Errorf("create_agent_hook: 'label' is required")
	}
	return s.agentToolHandler.ExecuteCreateAgentHook(ctx, projectID, args)
}

func (s *Service) executeDeleteAgentHook(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("delete_agent_hook"); err != nil {
		return nil, err
	}
	hookID, _ := args["hook_id"].(string)
	if hookID == "" {
		return nil, fmt.Errorf("delete_agent_hook: 'hook_id' is required")
	}
	return s.agentToolHandler.ExecuteDeleteAgentHook(ctx, projectID, args)
}

func (s *Service) executeListADKSessions(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("list_adk_sessions"); err != nil {
		return nil, err
	}
	return s.agentToolHandler.ExecuteListADKSessions(ctx, projectID, args)
}

func (s *Service) executeGetADKSession(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if err := s.requireAgentHandler("get_adk_session"); err != nil {
		return nil, err
	}
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return nil, fmt.Errorf("get_adk_session: 'session_id' is required")
	}
	return s.agentToolHandler.ExecuteGetADKSession(ctx, projectID, args)
}
