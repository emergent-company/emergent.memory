package agents

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// CoordinationToolDeps holds the dependencies needed by coordination tools.
// These tools need access to the executor and repository because they
// spawn sub-agents and query agent definitions at runtime.
type CoordinationToolDeps struct {
	Executor    *AgentExecutor
	Repo        *Repository
	Logger      *slog.Logger
	ProjectID   string
	ParentRunID string
	Depth       int
	MaxDepth    int
}

// --- list_available_agents ---

// ListAvailableAgentsArgs is the input for the list_available_agents tool.
type ListAvailableAgentsArgs struct {
	// No required parameters — returns all agents for the project
}

// AgentSummary is a single agent entry in the list_available_agents response.
type AgentSummary struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Tools       []string      `json:"tools,omitempty"`
	FlowType    AgentFlowType `json:"flow_type"`
	Visibility  string        `json:"visibility"`
}

// ListAvailableAgentsResult is the response from list_available_agents.
type ListAvailableAgentsResult struct {
	Agents []AgentSummary `json:"agents"`
	Count  int            `json:"count"`
}

// BuildListAvailableAgentsTool creates the list_available_agents ADK tool.
func BuildListAvailableAgentsTool(deps CoordinationToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameListAvailableAgents,
			Description: "List all available agents in the project's catalog. Returns name, description, tools list, flow_type, and visibility for each agent. Does not include system prompts.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			// Query all definitions for the project (include internal — agents should see everything)
			defs, err := deps.Repo.FindAllDefinitions(ctx, deps.ProjectID, true)
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("failed to list agents: %s", err.Error())}, nil
			}

			agents := make([]AgentSummary, 0, len(defs))
			for _, def := range defs {
				desc := ""
				if def.Description != nil {
					desc = *def.Description
				}
				agents = append(agents, AgentSummary{
					Name:        def.Name,
					Description: desc,
					Tools:       def.Tools,
					FlowType:    def.FlowType,
					Visibility:  string(def.Visibility),
				})
			}

			deps.Logger.Info("list_available_agents called",
				slog.String("project_id", deps.ProjectID),
				slog.Int("count", len(agents)),
			)

			// Return as map for ADK tool response
			return map[string]any{
				"agents": agents,
				"count":  len(agents),
			}, nil
		},
	)
}

// --- spawn_agents ---

// SpawnRequest is a single sub-agent spawn request.
type SpawnRequest struct {
	AgentName   string  `json:"agent_name"`
	Task        string  `json:"task"`
	Timeout     *int    `json:"timeout,omitempty"`       // timeout in seconds
	ResumeRunID *string `json:"resume_run_id,omitempty"` // for resuming paused runs
}

// SpawnResult is the result of a single sub-agent execution.
type SpawnResult struct {
	AgentName string         `json:"agent_name"`
	RunID     string         `json:"run_id,omitempty"`
	Status    AgentRunStatus `json:"status"`
	Summary   map[string]any `json:"summary,omitempty"`
	Steps     int            `json:"steps"`
	Error     string         `json:"error,omitempty"`
}

// SpawnAgentsResult is the aggregate response from spawn_agents.
type SpawnAgentsResult struct {
	Results []SpawnResult `json:"results"`
	Total   int           `json:"total"`
}

// BuildSpawnAgentsTool creates the spawn_agents ADK tool.
func BuildSpawnAgentsTool(deps CoordinationToolDeps) (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        ToolNameSpawnAgents,
			Description: "Spawn one or more sub-agents in parallel. Each spawn request specifies an agent_name (from the agent catalog) and a task description. Optionally include a timeout (in seconds) or resume_run_id to continue a paused agent. Returns results for each spawn including run_id, status, summary, and steps.",
		},
		func(ctx tool.Context, args map[string]any) (map[string]any, error) {
			// Parse the spawn requests from args
			requests, err := parseSpawnRequests(args)
			if err != nil {
				return map[string]any{"error": fmt.Sprintf("invalid spawn request: %s", err.Error())}, nil
			}

			if len(requests) == 0 {
				return map[string]any{"error": "no spawn requests provided"}, nil
			}

			deps.Logger.Info("spawn_agents called",
				slog.String("project_id", deps.ProjectID),
				slog.String("parent_run_id", deps.ParentRunID),
				slog.Int("depth", deps.Depth),
				slog.Int("request_count", len(requests)),
			)

			// Execute all spawn requests in parallel
			results := executeSpawns(ctx, deps, requests)

			return map[string]any{
				"results": results,
				"total":   len(results),
			}, nil
		},
	)
}

// executeSpawns runs all spawn requests in parallel using a WaitGroup.
// Parent context cancellation cascades to all sub-agents.
// Individual sub-agent timeouts only stop that sub-agent.
func executeSpawns(ctx context.Context, deps CoordinationToolDeps, requests []SpawnRequest) []SpawnResult {
	results := make([]SpawnResult, len(requests))
	var wg sync.WaitGroup

	for i, req := range requests {
		wg.Add(1)
		go func(idx int, spawnReq SpawnRequest) {
			defer wg.Done()
			results[idx] = executeSingleSpawn(ctx, deps, spawnReq)
		}(i, req)
	}

	wg.Wait()
	return results
}

// executeSingleSpawn handles a single sub-agent spawn request.
func executeSingleSpawn(ctx context.Context, deps CoordinationToolDeps, req SpawnRequest) SpawnResult {
	// Look up the agent definition by name
	def, err := deps.Repo.FindDefinitionByName(ctx, deps.ProjectID, req.AgentName)
	if err != nil {
		return SpawnResult{
			AgentName: req.AgentName,
			Status:    RunStatusError,
			Error:     fmt.Sprintf("failed to look up agent: %s", err.Error()),
		}
	}
	if def == nil {
		// Invalid agent name — fail this spawn, not others
		return SpawnResult{
			AgentName: req.AgentName,
			Status:    RunStatusError,
			Error:     fmt.Sprintf("agent %q not found in project catalog", req.AgentName),
		}
	}

	// Find the corresponding runtime Agent entity by name
	agent, err := deps.Repo.FindByName(ctx, deps.ProjectID, req.AgentName)
	if err != nil {
		return SpawnResult{
			AgentName: req.AgentName,
			Status:    RunStatusError,
			Error:     fmt.Sprintf("failed to find agent runtime entity: %s", err.Error()),
		}
	}
	if agent == nil {
		// Create a transient Agent entity for execution if one doesn't exist
		// This can happen when an agent definition exists but no runtime agent was created
		agent = &Agent{
			ProjectID: deps.ProjectID,
			Name:      def.Name,
			Prompt:    def.SystemPrompt,
		}
		if def.Description != nil {
			agent.Description = def.Description
		}
	}

	// Determine timeout — explicit timeout overrides agent definition's default_timeout
	var timeout *time.Duration
	if req.Timeout != nil && *req.Timeout > 0 {
		d := time.Duration(*req.Timeout) * time.Second
		timeout = &d
	} else if def.DefaultTimeout != nil && *def.DefaultTimeout > 0 {
		d := time.Duration(*def.DefaultTimeout) * time.Second
		timeout = &d
	}

	// Build the execute request for the sub-agent
	parentRunID := deps.ParentRunID
	execReq := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       deps.ProjectID,
		UserMessage:     req.Task,
		ParentRunID:     &parentRunID,
		MaxSteps:        def.MaxSteps,
		Timeout:         timeout,
		Depth:           deps.Depth + 1,
		MaxDepth:        deps.MaxDepth,
	}

	// Handle resume_run_id: resume a paused prior run instead of starting fresh
	if req.ResumeRunID != nil && *req.ResumeRunID != "" {
		priorRun, err := deps.Repo.FindRunByID(ctx, *req.ResumeRunID)
		if err != nil {
			return SpawnResult{
				AgentName: req.AgentName,
				Status:    RunStatusError,
				Error:     fmt.Sprintf("failed to load prior run %s: %s", *req.ResumeRunID, err.Error()),
			}
		}
		if priorRun == nil {
			return SpawnResult{
				AgentName: req.AgentName,
				Status:    RunStatusError,
				Error:     fmt.Sprintf("prior run %s not found", *req.ResumeRunID),
			}
		}

		deps.Logger.Info("resuming paused sub-agent",
			slog.String("agent_name", req.AgentName),
			slog.String("resume_run_id", *req.ResumeRunID),
			slog.Int("depth", deps.Depth+1),
		)

		result, err := deps.Executor.Resume(ctx, priorRun, execReq)
		if err != nil {
			return SpawnResult{
				AgentName: req.AgentName,
				Status:    RunStatusError,
				Error:     fmt.Sprintf("resume failed: %s", err.Error()),
			}
		}

		return SpawnResult{
			AgentName: req.AgentName,
			RunID:     result.RunID,
			Status:    result.Status,
			Summary:   result.Summary,
			Steps:     result.Steps,
		}
	}

	deps.Logger.Info("spawning sub-agent",
		slog.String("agent_name", req.AgentName),
		slog.String("project_id", deps.ProjectID),
		slog.Int("depth", deps.Depth+1),
	)

	// Execute the sub-agent (context propagation happens automatically via ctx)
	result, err := deps.Executor.Execute(ctx, execReq)
	if err != nil {
		return SpawnResult{
			AgentName: req.AgentName,
			Status:    RunStatusError,
			Error:     fmt.Sprintf("execution failed: %s", err.Error()),
		}
	}

	return SpawnResult{
		AgentName: req.AgentName,
		RunID:     result.RunID,
		Status:    result.Status,
		Summary:   result.Summary,
		Steps:     result.Steps,
	}
}

// parseSpawnRequests extracts SpawnRequest objects from the tool call args.
func parseSpawnRequests(args map[string]any) ([]SpawnRequest, error) {
	agentsRaw, ok := args["agents"]
	if !ok {
		// Try "requests" as an alternate key
		agentsRaw, ok = args["requests"]
		if !ok {
			return nil, fmt.Errorf("missing 'agents' array in spawn request")
		}
	}

	agentsList, ok := agentsRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("'agents' must be an array")
	}

	requests := make([]SpawnRequest, 0, len(agentsList))
	for i, item := range agentsList {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("spawn request at index %d is not an object", i)
		}

		agentName, _ := m["agent_name"].(string)
		if agentName == "" {
			return nil, fmt.Errorf("spawn request at index %d missing 'agent_name'", i)
		}

		task, _ := m["task"].(string)
		if task == "" {
			return nil, fmt.Errorf("spawn request at index %d missing 'task'", i)
		}

		req := SpawnRequest{
			AgentName: agentName,
			Task:      task,
		}

		// Optional timeout (seconds)
		if timeoutRaw, ok := m["timeout"]; ok {
			switch v := timeoutRaw.(type) {
			case float64:
				t := int(v)
				req.Timeout = &t
			case int:
				req.Timeout = &v
			}
		}

		// Optional resume_run_id
		if resumeID, ok := m["resume_run_id"].(string); ok && resumeID != "" {
			req.ResumeRunID = &resumeID
		}

		requests = append(requests, req)
	}

	return requests, nil
}
