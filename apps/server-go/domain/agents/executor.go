// Package agents provides agent execution and coordination for the Emergent platform.
package agents

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	"github.com/emergent/emergent-core/domain/mcp"
	"github.com/emergent/emergent-core/pkg/adk"
)

// ExecutorConfig holds configuration for the AgentExecutor.
type ExecutorConfig struct {
	ModelFactory *adk.ModelFactory
	MCPService   *mcp.Service
	ToolPool     *ToolPool
	Repository   *Repository
	Logger       *slog.Logger
}

// AgentExecutor builds and runs ADK-Go pipelines from agent configurations.
// It handles lifecycle tracking, step limits, timeouts, and doom loop detection.
type AgentExecutor struct {
	modelFactory *adk.ModelFactory
	mcpService   *mcp.Service
	toolPool     *ToolPool
	repo         *Repository
	log          *slog.Logger
}

// NewAgentExecutor creates a new AgentExecutor.
func NewAgentExecutor(cfg ExecutorConfig) (*AgentExecutor, error) {
	if cfg.ModelFactory == nil {
		return nil, fmt.Errorf("model factory is required")
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	return &AgentExecutor{
		modelFactory: cfg.ModelFactory,
		mcpService:   cfg.MCPService,
		toolPool:     cfg.ToolPool,
		repo:         cfg.Repository,
		log:          log,
	}, nil
}

// ExecuteRequest holds input for executing an agent.
type ExecuteRequest struct {
	Agent           *Agent           // The agent runtime entity
	AgentDefinition *AgentDefinition // The agent definition (for tool filtering, system prompt)
	ProjectID       string           // Project context
	UserMessage     string           // Input message / task for the agent
	ParentRunID     *string          // If this is a sub-agent spawn
	MaxSteps        *int             // Override default max steps
	Timeout         *time.Duration
	Depth           int // Current spawn depth (0 = top-level)
	MaxDepth        int // Max spawn depth (0 = use DefaultMaxDepth)
}

// ExecuteResult holds the output of an agent execution.
type ExecuteResult struct {
	RunID    string         `json:"runId"`
	Status   AgentRunStatus `json:"status"`
	Summary  map[string]any `json:"summary"`
	Steps    int            `json:"steps"`
	Error    string         `json:"error,omitempty"`
	Duration time.Duration  `json:"duration"`
}

// Execute runs an agent with full lifecycle management.
// This is the main entry point for agent execution — it creates the run record internally.
func (e *AgentExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	// Determine max steps
	maxSteps := MaxTotalStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 {
		maxSteps = *req.MaxSteps
	}
	if maxSteps > MaxTotalStepsPerRun {
		maxSteps = MaxTotalStepsPerRun
	}

	// Create the run record
	run, err := e.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:     req.Agent.ID,
		ParentRunID: req.ParentRunID,
		MaxSteps:    &maxSteps,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create agent run: %w", err)
	}

	return e.executeWithRunInternal(ctx, run, req, maxSteps, nil)
}

// ExecuteWithRun runs an agent using a pre-created run record.
// Use this when the caller needs the run ID before execution starts (e.g. async dispatch).
func (e *AgentExecutor) ExecuteWithRun(ctx context.Context, run *AgentRun, req ExecuteRequest) (*ExecuteResult, error) {
	// Determine max steps
	maxSteps := MaxTotalStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 {
		maxSteps = *req.MaxSteps
	}
	if maxSteps > MaxTotalStepsPerRun {
		maxSteps = MaxTotalStepsPerRun
	}

	// Update the run's max steps
	if run.MaxSteps == nil || *run.MaxSteps != maxSteps {
		run.MaxSteps = &maxSteps
	}

	return e.executeWithRunInternal(ctx, run, req, maxSteps, nil)
}

// Resume resumes a paused agent run with full conversation context preservation.
// It loads all prior messages, reconstructs the LLM conversation, and appends
// a continuation message before starting a new execution cycle.
func (e *AgentExecutor) Resume(ctx context.Context, priorRun *AgentRun, req ExecuteRequest) (*ExecuteResult, error) {
	// Validate: only paused runs can be resumed
	if priorRun.Status != RunStatusPaused {
		return nil, fmt.Errorf("cannot resume run %s: status is %q (only paused runs can be resumed)", priorRun.ID, priorRun.Status)
	}

	// Validate: cumulative step count must not exceed MaxTotalStepsPerRun
	if priorRun.StepCount >= MaxTotalStepsPerRun {
		return nil, fmt.Errorf("cannot resume run %s: cumulative step count (%d) has reached the global cap (%d)", priorRun.ID, priorRun.StepCount, MaxTotalStepsPerRun)
	}

	// Determine max steps for this resume session (fresh budget)
	maxSteps := MaxTotalStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 {
		maxSteps = *req.MaxSteps
	}
	// Cap so cumulative doesn't exceed global limit
	remaining := MaxTotalStepsPerRun - priorRun.StepCount
	if maxSteps > remaining {
		maxSteps = remaining
	}

	// Load prior messages for conversation reconstruction
	priorMessages, err := e.repo.FindMessagesByRunID(ctx, priorRun.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load prior messages for run %s: %w", priorRun.ID, err)
	}

	e.log.Info("resuming agent run",
		slog.String("prior_run_id", priorRun.ID),
		slog.Int("prior_step_count", priorRun.StepCount),
		slog.Int("prior_messages", len(priorMessages)),
		slog.Int("fresh_max_steps", maxSteps),
	)

	// Create a new run record that references the prior run
	run, err := e.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:          priorRun.AgentID,
		ParentRunID:      req.ParentRunID,
		MaxSteps:         &maxSteps,
		ResumedFrom:      &priorRun.ID,
		InitialStepCount: priorRun.StepCount, // cumulative step counter
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create resume run: %w", err)
	}

	// Reconstruct conversation from prior messages
	conversationHistory := reconstructConversation(priorMessages)

	return e.executeWithRunInternal(ctx, run, req, maxSteps, conversationHistory)
}

// executeWithRunInternal contains the shared execution logic for Execute, ExecuteWithRun, and Resume.
// conversationHistory is non-nil for resumed runs — it contains the prior LLM conversation to prepopulate.
func (e *AgentExecutor) executeWithRunInternal(ctx context.Context, run *AgentRun, req ExecuteRequest, maxSteps int, conversationHistory []*genai.Content) (*ExecuteResult, error) {
	startTime := time.Now()

	e.log.Info("starting agent execution",
		slog.String("run_id", run.ID),
		slog.String("agent_id", req.Agent.ID),
		slog.String("agent_name", req.Agent.Name),
		slog.Int("max_steps", maxSteps),
	)

	// Apply timeout if specified
	execCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout != nil && *req.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, *req.Timeout)
		defer cancel()
	}

	// Build the ADK pipeline
	result, err := e.executePipeline(execCtx, run, req, maxSteps, conversationHistory)
	if err != nil {
		duration := time.Since(startTime)

		// Determine if this was a context cancellation/timeout
		if ctx.Err() != nil {
			// Parent context cancelled — mark as cancelled
			_ = e.repo.CancelRun(ctx, run.ID, "parent context cancelled")
			return &ExecuteResult{
				RunID:    run.ID,
				Status:   RunStatusCancelled,
				Summary:  map[string]any{},
				Error:    "parent context cancelled",
				Duration: duration,
			}, nil
		}
		if execCtx.Err() != nil {
			// Timeout — mark as paused (can be resumed)
			_ = e.repo.PauseRun(ctx, run.ID, 0, map[string]any{"reason": "timeout"})
			return &ExecuteResult{
				RunID:    run.ID,
				Status:   RunStatusPaused,
				Summary:  map[string]any{"reason": "timeout"},
				Duration: duration,
			}, nil
		}

		// Execution error — mark as failed
		_ = e.repo.FailRun(ctx, run.ID, err.Error())
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{},
			Error:    err.Error(),
			Duration: duration,
		}, nil
	}

	return result, nil
}

// executePipeline builds and runs the ADK pipeline for an agent.
// conversationHistory is non-nil for resumed runs — prior messages are injected into the session.
func (e *AgentExecutor) executePipeline(ctx context.Context, run *AgentRun, req ExecuteRequest, maxSteps int, conversationHistory []*genai.Content) (*ExecuteResult, error) {
	startTime := time.Now()

	// Create the LLM model
	llm, err := e.modelFactory.CreateModel(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model: %w", err)
	}

	// Build tools for the agent via ToolPool filtering
	tools, err := e.buildTools(req.ProjectID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to build tools: %w", err)
	}

	// Inject coordination tools if the agent is allowed to have them.
	// Coordination tools are built at runtime because they need execution context
	// (project ID, parent run ID, depth) that isn't known at ToolPool build time.
	coordTools, err := e.buildCoordinationTools(run, req)
	if err != nil {
		e.log.Warn("failed to build coordination tools, continuing without them",
			slog.String("run_id", run.ID),
			slog.String("error", err.Error()),
		)
	} else {
		tools = append(tools, coordTools...)
	}

	// Create doom loop detector
	doomDetector := NewDoomLoopDetector(3, 5)

	// Step counter (thread-safe for callbacks)
	stepCounter := &stepTracker{maxSteps: maxSteps}

	// State persister — records messages and tool calls during execution
	persister := newStatePersister(e.repo, e.log, run.ID)

	// Build the agent instruction — prefer AgentDefinition.SystemPrompt, fall back to Agent.Prompt
	instruction := "You are a helpful AI agent."
	if req.AgentDefinition != nil && req.AgentDefinition.SystemPrompt != nil && *req.AgentDefinition.SystemPrompt != "" {
		instruction = *req.AgentDefinition.SystemPrompt
	} else if req.Agent.Prompt != nil && *req.Agent.Prompt != "" {
		instruction = *req.Agent.Prompt
	}

	// Create the LLM agent with callbacks for step/doom tracking and state persistence
	agentCfg := llmagent.Config{
		Name:        req.Agent.Name,
		Description: descriptionOrDefault(req.Agent.Description),
		Model:       llm,
		Tools:       tools,
		Instruction: instruction,
		GenerateContentConfig: &genai.GenerateContentConfig{
			Temperature: ptrFloat32AgentExec(0.2),
		},
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			func(cbCtx agent.CallbackContext, llmReq *model.LLMRequest) (*model.LLMResponse, error) {
				step := stepCounter.Increment()

				e.log.Debug("agent step",
					slog.String("run_id", run.ID),
					slog.Int("step", step),
					slog.Int("max_steps", maxSteps),
				)

				// Update step in persister for accurate step_number on persisted records
				persister.SetStep(step)

				// Persist step count periodically
				if step%5 == 0 {
					_ = e.repo.UpdateStepCount(ctx, run.ID, step)
				}

				// Check step limits
				if step >= maxSteps {
					e.log.Warn("agent reached step limit",
						slog.String("run_id", run.ID),
						slog.Int("step", step),
						slog.Int("max_steps", maxSteps),
					)
					// Soft stop: tell the model to summarize and finish
					if step == maxSteps {
						injectSystemMessage(llmReq, "SYSTEM: You have reached the maximum number of steps. Summarize your progress and provide a final answer. Do not use any more tools.")
					}
					// Hard stop: return empty response to end the loop
					if step > maxSteps {
						return &model.LLMResponse{
							Content: genai.NewContentFromText("Step limit exceeded. Stopping execution.", "model"),
						}, nil
					}
				}

				return nil, nil
			},
		},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{
			func(toolCtx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
				// Record tool call start time for duration tracking
				persister.RecordToolStart(t.Name(), args)
				return args, nil
			},
		},
		AfterToolCallbacks: []llmagent.AfterToolCallback{
			func(toolCtx tool.Context, t tool.Tool, args map[string]any, resp map[string]any, toolErr error) (map[string]any, error) {
				toolName := t.Name()

				// Persist the tool call
				persister.PersistToolCall(ctx, toolName, args, resp, toolErr)

				// Check for doom loop
				if action := doomDetector.RecordCall(toolName, args); action != DoomLoopOK {
					switch action {
					case DoomLoopWarn:
						e.log.Warn("doom loop detected: repeated tool call",
							slog.String("run_id", run.ID),
							slog.String("tool", toolName),
							slog.Int("count", doomDetector.ConsecutiveCount(toolName, args)),
						)
						// Return an error response to the agent
						return map[string]any{
							"error": fmt.Sprintf("WARNING: You have called '%s' with the same arguments %d times consecutively. This appears to be a loop. Try a different approach or provide your final answer.", toolName, doomDetector.ConsecutiveCount(toolName, args)),
						}, nil
					case DoomLoopStop:
						e.log.Error("doom loop hard stop: too many identical calls",
							slog.String("run_id", run.ID),
							slog.String("tool", toolName),
						)
						return nil, fmt.Errorf("doom loop detected: '%s' called %d times with identical arguments", toolName, doomDetector.ConsecutiveCount(toolName, args))
					}
				}
				return resp, toolErr
			},
		},
	}

	llmAgent, err := llmagent.New(agentCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	// Create in-memory session
	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: "agents",
		UserID:  "system",
		State: map[string]any{
			"project_id": req.ProjectID,
			"agent_id":   req.Agent.ID,
			"run_id":     run.ID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	sess := createResp.Session

	// Create runner
	r, err := runner.New(runner.Config{
		Agent:          llmAgent,
		SessionService: sessionService,
		AppName:        "agents",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	// Build user message
	userMessage := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText(req.UserMessage)},
	}

	// Persist the initial user message
	persister.PersistMessage(ctx, "user", userMessage)

	// Execute the agent — persist each event as it streams
	var lastEvent *session.Event
	for event, err := range r.Run(ctx, "system", sess.ID(), userMessage, agent.RunConfig{}) {
		if err != nil {
			finalSteps := stepCounter.Count()
			_ = e.repo.UpdateStepCount(ctx, run.ID, finalSteps)
			_ = e.repo.FailRunWithSteps(ctx, run.ID, err.Error(), finalSteps)
			return &ExecuteResult{
				RunID:    run.ID,
				Status:   RunStatusError,
				Summary:  map[string]any{},
				Steps:    finalSteps,
				Error:    err.Error(),
				Duration: time.Since(startTime),
			}, nil
		}
		if event != nil {
			// Persist the event as a message (skips partial/incomplete events)
			persister.PersistEvent(ctx, event)
			lastEvent = event
		}
	}

	// Extract final response
	finalSteps := stepCounter.Count()
	_ = e.repo.UpdateStepCount(ctx, run.ID, finalSteps)

	summary := map[string]any{
		"steps": finalSteps,
	}

	// Extract the last text response
	if lastEvent != nil && lastEvent.Content != nil {
		var responseText string
		for _, part := range lastEvent.Content.Parts {
			if part.Text != "" {
				responseText += part.Text
			}
		}
		if responseText != "" {
			summary["response"] = responseText
		}
	}

	// Determine final status
	finalStatus := RunStatusSuccess
	if finalSteps >= maxSteps {
		finalStatus = RunStatusPaused
		summary["reason"] = "step_limit_reached"
		_ = e.repo.PauseRun(ctx, run.ID, finalSteps, summary)
	} else {
		_ = e.repo.CompleteRun(ctx, run.ID, summary)
	}

	duration := time.Since(startTime)
	durationMs := int(duration.Milliseconds())
	_, _ = e.repo.db.NewUpdate().
		Model((*AgentRun)(nil)).
		Set("duration_ms = ?", durationMs).
		Where("id = ?", run.ID).
		Exec(ctx)

	e.log.Info("agent execution completed",
		slog.String("run_id", run.ID),
		slog.String("status", string(finalStatus)),
		slog.Int("steps", finalSteps),
		slog.Duration("duration", duration),
	)

	return &ExecuteResult{
		RunID:    run.ID,
		Status:   finalStatus,
		Summary:  summary,
		Steps:    finalSteps,
		Duration: duration,
	}, nil
}

// buildTools resolves the tools for an agent execution.
// If a ToolPool is configured, it uses the pool with agent definition filtering.
// Otherwise, falls back to wrapping all MCP tools directly (legacy behavior).
func (e *AgentExecutor) buildTools(projectID string, req ExecuteRequest) ([]tool.Tool, error) {
	// Use ToolPool if available (preferred path)
	if e.toolPool != nil {
		return e.toolPool.ResolveTools(projectID, req.AgentDefinition, req.Depth, req.MaxDepth)
	}

	// Legacy fallback: wrap all MCP tools directly (no filtering)
	if e.mcpService == nil {
		return nil, nil
	}

	toolDefs := e.mcpService.GetToolDefinitions()
	tools := make([]tool.Tool, 0, len(toolDefs))

	for _, td := range toolDefs {
		// Capture loop variable
		toolDef := td
		svc := e.mcpService
		pid := projectID

		// Create a function tool that wraps the MCP service call
		t, err := functiontool.New(
			functiontool.Config{
				Name:        toolDef.Name,
				Description: toolDef.Description,
			},
			func(ctx tool.Context, args map[string]any) (map[string]any, error) {
				result, err := svc.ExecuteTool(ctx, pid, toolDef.Name, args)
				if err != nil {
					return map[string]any{"error": err.Error()}, nil
				}

				// Convert MCP ToolResult to map
				if result != nil && len(result.Content) > 0 {
					var textParts []string
					for _, block := range result.Content {
						if block.Text != "" {
							textParts = append(textParts, block.Text)
						}
					}
					if len(textParts) == 1 {
						// Try to parse as JSON first
						var parsed map[string]any
						if err := json.Unmarshal([]byte(textParts[0]), &parsed); err == nil {
							return parsed, nil
						}
						return map[string]any{"result": textParts[0]}, nil
					}
					return map[string]any{"results": textParts}, nil
				}

				return map[string]any{"result": "ok"}, nil
			},
		)
		if err != nil {
			e.log.Warn("failed to create tool wrapper, skipping",
				slog.String("tool", toolDef.Name),
				slog.String("error", err.Error()),
			)
			continue
		}

		tools = append(tools, t)
	}

	return tools, nil
}

// buildCoordinationTools creates the spawn_agents and list_available_agents tools
// if the agent is allowed to have them. These are built at runtime because they
// need execution context (project ID, parent run ID, depth).
func (e *AgentExecutor) buildCoordinationTools(run *AgentRun, req ExecuteRequest) ([]tool.Tool, error) {
	// Check if coordination tools should be included.
	// The ToolPool already handles depth restrictions for tool filtering,
	// but coordination tools are injected separately because they're not MCP tools.
	// We need to apply the same depth restrictions here.
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}

	// At depth >= maxDepth, no coordination tools
	if req.Depth >= maxDepth {
		return nil, nil
	}

	// At depth > 0, only include if explicitly in the definition's tools list
	if req.Depth > 0 && req.AgentDefinition != nil {
		hasSpawn := false
		hasList := false
		for _, t := range req.AgentDefinition.Tools {
			if t == ToolNameSpawnAgents {
				hasSpawn = true
			}
			if t == ToolNameListAvailableAgents {
				hasList = true
			}
		}
		if !hasSpawn && !hasList {
			return nil, nil
		}
	}

	// For top-level agents (depth == 0) with no definition or wildcard tools,
	// or agents that explicitly include coordination tools, build them.
	deps := CoordinationToolDeps{
		Executor:    e,
		Repo:        e.repo,
		Logger:      e.log,
		ProjectID:   req.ProjectID,
		ParentRunID: run.ID,
		Depth:       req.Depth,
		MaxDepth:    maxDepth,
	}

	var tools []tool.Tool

	// Determine which coordination tools to include
	includeSpawn := true
	includeList := true

	if req.Depth > 0 && req.AgentDefinition != nil {
		// At depth > 0, only include explicitly requested tools
		includeSpawn = false
		includeList = false
		for _, t := range req.AgentDefinition.Tools {
			if t == ToolNameSpawnAgents {
				includeSpawn = true
			}
			if t == ToolNameListAvailableAgents {
				includeList = true
			}
		}
	} else if req.AgentDefinition != nil && len(req.AgentDefinition.Tools) > 0 {
		// At depth 0 with a definition, check if tools whitelist includes coordination tools
		// Wildcard "*" means include everything
		hasWildcard := false
		includeSpawn = false
		includeList = false
		for _, t := range req.AgentDefinition.Tools {
			if t == "*" {
				hasWildcard = true
				break
			}
			if t == ToolNameSpawnAgents {
				includeSpawn = true
			}
			if t == ToolNameListAvailableAgents {
				includeList = true
			}
		}
		if hasWildcard {
			includeSpawn = true
			includeList = true
		}
	}

	if includeList {
		listTool, err := BuildListAvailableAgentsTool(deps)
		if err != nil {
			return nil, fmt.Errorf("failed to build list_available_agents: %w", err)
		}
		tools = append(tools, listTool)
	}

	if includeSpawn {
		spawnTool, err := BuildSpawnAgentsTool(deps)
		if err != nil {
			return nil, fmt.Errorf("failed to build spawn_agents: %w", err)
		}
		tools = append(tools, spawnTool)
	}

	return tools, nil
}

// injectSystemMessage prepends a system message to the LLM request contents.
func injectSystemMessage(req *model.LLMRequest, message string) {
	systemContent := &genai.Content{
		Role:  "user",
		Parts: []*genai.Part{genai.NewPartFromText(message)},
	}
	req.Contents = append([]*genai.Content{systemContent}, req.Contents...)
}

// descriptionOrDefault returns the description or a default string.
func descriptionOrDefault(desc *string) string {
	if desc != nil && *desc != "" {
		return *desc
	}
	return "An AI agent"
}

// ptrFloat32 returns a pointer to a float32 value.
func ptrFloat32AgentExec(v float32) *float32 {
	return &v
}

// --- Step Tracker ---

// stepTracker is a thread-safe step counter.
type stepTracker struct {
	mu       sync.Mutex
	count    int
	maxSteps int
}

func (s *stepTracker) Increment() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	return s.count
}

func (s *stepTracker) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// --- Doom Loop Detector ---

// DoomLoopAction represents what the detector recommends.
type DoomLoopAction int

const (
	DoomLoopOK   DoomLoopAction = iota // No issue
	DoomLoopWarn                       // Warning threshold reached
	DoomLoopStop                       // Hard stop threshold reached
)

// DoomLoopDetector tracks consecutive identical tool calls to detect infinite loops.
type DoomLoopDetector struct {
	mu               sync.Mutex
	warnThreshold    int
	stopThreshold    int
	lastCallHash     string
	consecutiveCount int
}

// NewDoomLoopDetector creates a new detector with warn and stop thresholds.
func NewDoomLoopDetector(warnThreshold, stopThreshold int) *DoomLoopDetector {
	return &DoomLoopDetector{
		warnThreshold: warnThreshold,
		stopThreshold: stopThreshold,
	}
}

// RecordCall records a tool call and returns the recommended action.
func (d *DoomLoopDetector) RecordCall(toolName string, args map[string]any) DoomLoopAction {
	d.mu.Lock()
	defer d.mu.Unlock()

	hash := hashToolCall(toolName, args)

	if hash == d.lastCallHash {
		d.consecutiveCount++
	} else {
		d.lastCallHash = hash
		d.consecutiveCount = 1
	}

	if d.consecutiveCount >= d.stopThreshold {
		return DoomLoopStop
	}
	if d.consecutiveCount >= d.warnThreshold {
		return DoomLoopWarn
	}
	return DoomLoopOK
}

// ConsecutiveCount returns the current consecutive count for a tool+args combination.
func (d *DoomLoopDetector) ConsecutiveCount(toolName string, args map[string]any) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.consecutiveCount
}

// hashToolCall creates a hash of the tool name and arguments for comparison.
func hashToolCall(toolName string, args map[string]any) string {
	data, _ := json.Marshal(map[string]any{
		"tool": toolName,
		"args": args,
	})
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// --- State Persister ---

// statePersister handles real-time persistence of messages and tool calls during agent execution.
// It is created per-execution and tracks the current step number and the last assistant message ID
// so that tool calls can be linked back to the message that triggered them.
type statePersister struct {
	repo  *Repository
	log   *slog.Logger
	runID string

	mu                   sync.Mutex
	currentStep          int
	lastAssistantMsgID   string               // ID of the most recent assistant message (for linking tool calls)
	toolStartTimes       map[string]time.Time // key: "toolName:argsHash" -> start time
	persistedEventHashes map[string]bool      // avoid persisting duplicate events
}

// newStatePersister creates a new state persister for an agent execution.
func newStatePersister(repo *Repository, log *slog.Logger, runID string) *statePersister {
	return &statePersister{
		repo:                 repo,
		log:                  log,
		runID:                runID,
		toolStartTimes:       make(map[string]time.Time),
		persistedEventHashes: make(map[string]bool),
	}
}

// SetStep updates the current step number (called from BeforeModelCallback).
func (sp *statePersister) SetStep(step int) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.currentStep = step
}

// step returns the current step number.
func (sp *statePersister) step() int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.currentStep
}

// LastAssistantMessageID returns the ID of the most recently persisted assistant message.
func (sp *statePersister) LastAssistantMessageID() string {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.lastAssistantMsgID
}

// PersistMessage persists a single message (user, system, etc.) to the database.
func (sp *statePersister) PersistMessage(ctx context.Context, role string, content *genai.Content) {
	contentMap := contentToMap(content)

	msg := &AgentRunMessage{
		RunID:      sp.runID,
		Role:       role,
		Content:    contentMap,
		StepNumber: sp.step(),
	}

	if err := sp.repo.CreateMessage(ctx, msg); err != nil {
		sp.log.Warn("failed to persist message",
			slog.String("run_id", sp.runID),
			slog.String("role", role),
			slog.String("error", err.Error()),
		)
		return
	}

	// Track assistant message ID for tool call linking
	if role == "model" || role == "assistant" {
		sp.mu.Lock()
		sp.lastAssistantMsgID = msg.ID
		sp.mu.Unlock()
	}
}

// PersistEvent persists an ADK session event as a message.
// Skips partial/incomplete events to avoid duplicates.
func (sp *statePersister) PersistEvent(ctx context.Context, event *session.Event) {
	// Skip events without content
	if event == nil || event.Content == nil {
		return
	}

	// Skip partial (streaming) events — only persist complete events
	if event.Partial {
		return
	}

	// Deduplicate events by content hash to avoid persisting the same message twice
	eventHash := hashContent(event.Content)
	sp.mu.Lock()
	if sp.persistedEventHashes[eventHash] {
		sp.mu.Unlock()
		return
	}
	sp.persistedEventHashes[eventHash] = true
	sp.mu.Unlock()

	// Determine role from the content
	role := event.Content.Role
	if role == "" {
		role = "assistant"
	}

	sp.PersistMessage(ctx, role, event.Content)
}

// RecordToolStart records the start time of a tool invocation for duration tracking.
// Called from BeforeToolCallback.
func (sp *statePersister) RecordToolStart(toolName string, args map[string]any) {
	key := toolCallKey(toolName, args)
	sp.mu.Lock()
	sp.toolStartTimes[key] = time.Now()
	sp.mu.Unlock()
}

// PersistToolCall persists a tool call record after the tool has completed.
// Called from AfterToolCallback.
func (sp *statePersister) PersistToolCall(ctx context.Context, toolName string, args map[string]any, result map[string]any, toolErr error) {
	key := toolCallKey(toolName, args)

	// Get start time for duration calculation
	sp.mu.Lock()
	startTime, hasStart := sp.toolStartTimes[key]
	if hasStart {
		delete(sp.toolStartTimes, key)
	}
	assistantMsgID := sp.lastAssistantMsgID
	sp.mu.Unlock()

	// Calculate duration
	var durationMs *int
	if hasStart {
		d := int(time.Since(startTime).Milliseconds())
		durationMs = &d
	}

	// Determine status
	status := "completed"
	output := result
	if toolErr != nil {
		status = "error"
		if output == nil {
			output = make(map[string]any)
		}
		output["error"] = toolErr.Error()
	}
	if output == nil {
		output = make(map[string]any)
	}

	// Prepare input
	input := args
	if input == nil {
		input = make(map[string]any)
	}

	// Build the tool call record
	tc := &AgentRunToolCall{
		RunID:      sp.runID,
		ToolName:   toolName,
		Input:      input,
		Output:     output,
		Status:     status,
		DurationMs: durationMs,
		StepNumber: sp.step(),
	}

	// Link to the assistant message that triggered this tool call
	if assistantMsgID != "" {
		tc.MessageID = &assistantMsgID
	}

	if err := sp.repo.CreateToolCall(ctx, tc); err != nil {
		sp.log.Warn("failed to persist tool call",
			slog.String("run_id", sp.runID),
			slog.String("tool", toolName),
			slog.String("error", err.Error()),
		)
	}
}

// contentToMap serializes a genai.Content to map[string]any for JSONB storage.
// This preserves the complete message structure including tool call IDs,
// function names, and arguments for conversation reconstruction.
func contentToMap(content *genai.Content) map[string]any {
	if content == nil {
		return map[string]any{}
	}

	// Marshal to JSON then unmarshal to map to get a clean JSONB-compatible structure
	data, err := json.Marshal(content)
	if err != nil {
		// Fallback: extract what we can
		result := map[string]any{"role": content.Role}
		if len(content.Parts) > 0 {
			var textParts []string
			for _, part := range content.Parts {
				if part.Text != "" {
					textParts = append(textParts, part.Text)
				}
			}
			if len(textParts) > 0 {
				result["text"] = textParts
			}
		}
		return result
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{"role": content.Role, "raw": string(data)}
	}
	return result
}

// hashContent creates a hash of genai.Content for deduplication.
func hashContent(content *genai.Content) string {
	data, _ := json.Marshal(content)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// reconstructConversation converts persisted AgentRunMessages back into genai.Content
// for LLM conversation history. Messages are assumed to be in chronological order.
// Messages whose Content cannot be deserialized are skipped with a warning logged.
func reconstructConversation(messages []*AgentRunMessage) []*genai.Content {
	var history []*genai.Content
	for _, msg := range messages {
		if msg.Content == nil {
			continue
		}
		// The Content map was created by contentToMap which JSON-roundtrips genai.Content.
		// Reverse the process: marshal the map back to JSON, then unmarshal into genai.Content.
		data, err := json.Marshal(msg.Content)
		if err != nil {
			continue
		}
		var content genai.Content
		if err := json.Unmarshal(data, &content); err != nil {
			continue
		}
		// Ensure role is set (contentToMap may have stored it in the map already,
		// but fall back to the message's Role field if the deserialized role is empty).
		if content.Role == "" {
			content.Role = msg.Role
		}
		history = append(history, &content)
	}
	return history
}

// toolCallKey creates a key for tracking tool call start times.
// Uses a simple hash to keep the map keys short.
func toolCallKey(toolName string, args map[string]any) string {
	return hashToolCall(toolName, args)
}
