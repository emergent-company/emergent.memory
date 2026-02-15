package agents

import (
	"context"
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
	"google.golang.org/genai"

	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/logger"
)

// ExecuteRequest defines the parameters for executing an agent.
type ExecuteRequest struct {
	Agent           *Agent
	AgentDefinition *AgentDefinition
	ProjectID       string
	UserMessage     string
	ParentRunID     *string
	MaxSteps        *int
	Timeout         *time.Duration
	Depth           int
	MaxDepth        int
}

// ExecuteResult is the outcome of an agent execution.
type ExecuteResult struct {
	RunID    string
	Status   AgentRunStatus
	Summary  map[string]any
	Steps    int
	Duration time.Duration
}

// AgentExecutor is the core execution engine for running agents via ADK.
// It builds an LLM agent pipeline with tools from the ToolPool, runs it
// via the ADK runner, tracks steps, detects doom loops, and persists
// all messages and tool calls to the database for full state recovery.
type AgentExecutor struct {
	modelFactory *adk.ModelFactory
	toolPool     *ToolPool
	repo         *Repository
	log          *slog.Logger
}

// NewAgentExecutor creates a new AgentExecutor.
func NewAgentExecutor(
	modelFactory *adk.ModelFactory,
	toolPool *ToolPool,
	repo *Repository,
	log *slog.Logger,
) *AgentExecutor {
	return &AgentExecutor{
		modelFactory: modelFactory,
		toolPool:     toolPool,
		repo:         repo,
		log:          log.With(logger.Scope("agents.executor")),
	}
}

// Execute runs an agent from scratch using the provided request.
func (ae *AgentExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	startTime := time.Now()

	// Validate depth
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if req.Depth > maxDepth {
		return nil, fmt.Errorf("max agent depth %d exceeded (current depth: %d)", maxDepth, req.Depth)
	}

	// Determine max steps for this run
	maxSteps := MaxTotalStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 && *req.MaxSteps < maxSteps {
		maxSteps = *req.MaxSteps
	}

	// Create the run record
	run, err := ae.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:     ae.resolveAgentID(req),
		ParentRunID: req.ParentRunID,
		MaxSteps:    &maxSteps,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create agent run: %w", err)
	}

	ae.log.Info("executing agent",
		slog.String("run_id", run.ID),
		slog.String("project_id", req.ProjectID),
		slog.String("agent_name", ae.resolveAgentName(req)),
		slog.Int("depth", req.Depth),
		slog.Int("max_steps", maxSteps),
	)

	// Build and run the pipeline
	result, err := ae.runPipeline(ctx, run, req, maxSteps, 0, startTime)
	if err != nil {
		// Mark run as failed
		_ = ae.repo.FailRunWithSteps(ctx, run.ID, err.Error(), 0)
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": err.Error()},
			Steps:    0,
			Duration: time.Since(startTime),
		}, nil
	}

	return result, nil
}

// Resume continues a previously paused agent run from its saved state.
func (ae *AgentExecutor) Resume(ctx context.Context, priorRun *AgentRun, req ExecuteRequest) (*ExecuteResult, error) {
	startTime := time.Now()

	if priorRun.Status != RunStatusPaused {
		return nil, fmt.Errorf("cannot resume run %s: status is %s (expected paused)", priorRun.ID, priorRun.Status)
	}

	// Determine max steps, considering cumulative step count
	maxSteps := MaxTotalStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 && *req.MaxSteps < maxSteps {
		maxSteps = *req.MaxSteps
	}
	if priorRun.StepCount >= maxSteps {
		return nil, fmt.Errorf("run %s already at step limit (%d/%d)", priorRun.ID, priorRun.StepCount, maxSteps)
	}

	// Create a new run record that tracks the resume chain
	resumedFrom := priorRun.ID
	newRun, err := ae.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:          priorRun.AgentID,
		ParentRunID:      req.ParentRunID,
		MaxSteps:         &maxSteps,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: priorRun.StepCount,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create resumed run: %w", err)
	}

	ae.log.Info("resuming agent",
		slog.String("run_id", newRun.ID),
		slog.String("resumed_from", priorRun.ID),
		slog.Int("prior_steps", priorRun.StepCount),
		slog.Int("max_steps", maxSteps),
	)

	// Build and run the pipeline with accumulated step count
	result, err := ae.runPipeline(ctx, newRun, req, maxSteps, priorRun.StepCount, startTime)
	if err != nil {
		_ = ae.repo.FailRunWithSteps(ctx, newRun.ID, err.Error(), priorRun.StepCount)
		return &ExecuteResult{
			RunID:    newRun.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": err.Error()},
			Steps:    priorRun.StepCount,
			Duration: time.Since(startTime),
		}, nil
	}

	return result, nil
}

// runPipeline builds the ADK agent, resolves tools, creates the runner, and
// iterates over events until the agent is done or a safety limit is reached.
func (ae *AgentExecutor) runPipeline(
	ctx context.Context,
	run *AgentRun,
	req ExecuteRequest,
	maxSteps int,
	initialSteps int,
	startTime time.Time,
) (*ExecuteResult, error) {
	// Apply timeout if specified
	if req.Timeout != nil && *req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *req.Timeout)
		defer cancel()
	}

	// Create the LLM model
	modelName := ""
	if req.AgentDefinition != nil && req.AgentDefinition.Model != nil && req.AgentDefinition.Model.Name != "" {
		modelName = req.AgentDefinition.Model.Name
	}

	var llm model.LLM
	var err error
	if modelName != "" {
		llm, err = ae.modelFactory.CreateModelWithName(ctx, modelName)
	} else {
		llm, err = ae.modelFactory.CreateModel(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM model: %w", err)
	}

	// Resolve tools from the tool pool
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	resolvedTools, err := ae.toolPool.ResolveTools(req.ProjectID, req.AgentDefinition, req.Depth, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tools: %w", err)
	}

	// Add coordination tools (spawn_agents, list_available_agents) for top-level or opted-in agents
	coordTools, err := ae.buildCoordinationTools(req, run.ID)
	if err != nil {
		ae.log.Warn("failed to build coordination tools, continuing without them",
			slog.String("error", err.Error()),
		)
	} else {
		resolvedTools = append(resolvedTools, coordTools...)
	}

	// Build the LLM agent
	agentName := ae.resolveAgentName(req)
	instruction := ae.resolveInstruction(req)

	genConfig := ae.modelFactory.DefaultGenerateConfig()
	if req.AgentDefinition != nil && req.AgentDefinition.Model != nil {
		if req.AgentDefinition.Model.Temperature != nil {
			genConfig.Temperature = req.AgentDefinition.Model.Temperature
		}
		if req.AgentDefinition.Model.MaxTokens != nil {
			genConfig.MaxOutputTokens = int32(*req.AgentDefinition.Model.MaxTokens)
		}
	}

	// Create the step tracker
	tracker := newStepTracker(maxSteps, initialSteps)

	// Create the doom loop detector
	doomDetector := newDoomLoopDetector(ae.log)

	// Set up before-model callback for step tracking
	beforeModelCb := func(cbCtx agent.CallbackContext, llmReq *model.LLMRequest) (*model.LLMResponse, error) {
		currentStep := tracker.increment()

		// Check step limit
		if tracker.exceeded() {
			ae.log.Warn("step limit reached, stopping agent",
				slog.String("run_id", run.ID),
				slog.Int("step", currentStep),
				slog.Int("max_steps", maxSteps),
			)
			// Pause the run instead of failing
			_ = ae.repo.PauseRun(ctx, run.ID, currentStep)
			return &model.LLMResponse{
				Content: genai.NewContentFromText("Step limit reached. Run has been paused.", genai.RoleModel),
			}, nil
		}

		// Periodically persist step count
		if currentStep%5 == 0 {
			_ = ae.repo.UpdateStepCount(ctx, run.ID, currentStep)
		}

		return nil, nil
	}

	// Set up after-tool callback for doom loop detection and state persistence
	afterToolCb := func(tCtx tool.Context, t tool.Tool, args, result map[string]any, toolErr error) (map[string]any, error) {
		toolName := t.Name()
		currentStep := tracker.current()

		// Record the tool call
		status := "completed"
		if toolErr != nil {
			status = "error"
		}
		output := result
		if output == nil {
			output = map[string]any{}
		}
		if toolErr != nil {
			output["error"] = toolErr.Error()
		}
		tcRecord := &AgentRunToolCall{
			RunID:      run.ID,
			ToolName:   toolName,
			Input:      args,
			Output:     output,
			Status:     status,
			StepNumber: currentStep,
		}
		if persistErr := ae.repo.CreateToolCall(ctx, tcRecord); persistErr != nil {
			ae.log.Warn("failed to persist tool call",
				slog.String("run_id", run.ID),
				slog.String("tool", toolName),
				slog.String("error", persistErr.Error()),
			)
		}

		// Check for doom loop
		action := doomDetector.recordCall(toolName, args)
		switch action {
		case doomActionWarn:
			ae.log.Warn("doom loop warning: consecutive identical tool calls detected",
				slog.String("run_id", run.ID),
				slog.String("tool", toolName),
				slog.Int("consecutive", doomDetector.consecutiveCount),
			)
		case doomActionStop:
			ae.log.Error("doom loop detected, stopping agent",
				slog.String("run_id", run.ID),
				slog.String("tool", toolName),
				slog.Int("consecutive", doomDetector.consecutiveCount),
			)
			return map[string]any{
				"error":   "DOOM_LOOP_DETECTED",
				"message": fmt.Sprintf("Detected %d consecutive identical calls to %q. Agent is stuck in a loop and has been stopped.", doomDetector.consecutiveCount, toolName),
			}, nil
		}

		return result, toolErr
	}

	llmAgent, err := llmagent.New(llmagent.Config{
		Name:                  sanitizeAgentName(agentName),
		Description:           ae.resolveDescription(req),
		Instruction:           instruction,
		Model:                 llm,
		Tools:                 resolvedTools,
		GenerateContentConfig: genConfig,
		BeforeModelCallbacks:  []llmagent.BeforeModelCallback{beforeModelCb},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{afterToolCb},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	// Create in-memory session and runner
	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: "agents",
		UserID:  "system",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	sess := createResp.Session

	r, err := runner.New(runner.Config{
		Agent:          llmAgent,
		SessionService: sessionService,
		AppName:        "agents",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	// Build the user message
	userContent := genai.NewContentFromText(req.UserMessage, genai.RoleUser)

	// Persist the user message
	ae.persistMessage(ctx, run.ID, "user", req.UserMessage, initialSteps)

	// Run the agent
	var lastEvent *session.Event
	for event, eventErr := range r.Run(ctx, "system", sess.ID(), userContent, agent.RunConfig{}) {
		if eventErr != nil {
			steps := tracker.current()
			_ = ae.repo.FailRunWithSteps(ctx, run.ID, eventErr.Error(), steps)
			return &ExecuteResult{
				RunID:    run.ID,
				Status:   RunStatusError,
				Summary:  map[string]any{"error": eventErr.Error()},
				Steps:    steps,
				Duration: time.Since(startTime),
			}, nil
		}

		if event == nil {
			continue
		}

		// Persist assistant messages from events
		if event.Content != nil && !event.Partial {
			ae.persistEventContent(ctx, run.ID, event, tracker.current())
		}

		if event.IsFinalResponse() {
			lastEvent = event
		}
	}

	// Determine final status
	steps := tracker.current()
	duration := time.Since(startTime)
	durationMs := int(duration.Milliseconds())

	// Check if we were paused by the step limit callback
	currentRun, _ := ae.repo.FindRunByID(ctx, run.ID)
	if currentRun != nil && currentRun.Status == RunStatusPaused {
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusPaused,
			Summary:  map[string]any{"reason": "step_limit_reached", "steps": steps},
			Steps:    steps,
			Duration: duration,
		}, nil
	}

	// Build summary from the last event
	summary := ae.buildSummary(lastEvent, steps)

	// Mark run as complete
	if err := ae.repo.CompleteRunWithSteps(ctx, run.ID, summary, steps, durationMs); err != nil {
		ae.log.Warn("failed to complete run record",
			slog.String("run_id", run.ID),
			slog.String("error", err.Error()),
		)
	}

	// Update the agent's last run status
	if req.Agent != nil && req.Agent.ID != "" {
		_ = ae.repo.UpdateLastRun(ctx, req.Agent.ID, string(RunStatusSuccess))
	}

	ae.log.Info("agent execution completed",
		slog.String("run_id", run.ID),
		slog.Int("steps", steps),
		slog.Duration("duration", duration),
	)

	return &ExecuteResult{
		RunID:    run.ID,
		Status:   RunStatusSuccess,
		Summary:  summary,
		Steps:    steps,
		Duration: duration,
	}, nil
}

// buildCoordinationTools creates spawn_agents and list_available_agents tools
// if the agent is at a depth that allows coordination.
func (ae *AgentExecutor) buildCoordinationTools(req ExecuteRequest, runID string) ([]tool.Tool, error) {
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}

	// Sub-agents at max depth don't get coordination tools
	if req.Depth >= maxDepth {
		return nil, nil
	}

	// Sub-agents only get coordination tools if explicitly opted in
	if req.Depth > 0 && req.AgentDefinition != nil {
		hasCoordTool := false
		for _, t := range req.AgentDefinition.Tools {
			if coordinationTools[t] {
				hasCoordTool = true
				break
			}
		}
		if !hasCoordTool {
			return nil, nil
		}
	}

	deps := CoordinationToolDeps{
		Executor:    ae,
		Repo:        ae.repo,
		Logger:      ae.log,
		ProjectID:   req.ProjectID,
		ParentRunID: runID,
		Depth:       req.Depth,
		MaxDepth:    maxDepth,
	}

	var tools []tool.Tool

	listTool, err := BuildListAvailableAgentsTool(deps)
	if err != nil {
		return nil, fmt.Errorf("failed to build list_available_agents: %w", err)
	}
	tools = append(tools, listTool)

	spawnTool, err := BuildSpawnAgentsTool(deps)
	if err != nil {
		return nil, fmt.Errorf("failed to build spawn_agents: %w", err)
	}
	tools = append(tools, spawnTool)

	return tools, nil
}

// resolveAgentID returns the agent ID for the run record.
// If the Agent entity has an ID, use it. Otherwise, try the definition.
func (ae *AgentExecutor) resolveAgentID(req ExecuteRequest) string {
	if req.Agent != nil && req.Agent.ID != "" {
		return req.Agent.ID
	}
	// If there's no runtime Agent entity, we need a placeholder
	// This shouldn't happen in normal flow since triggers.go creates the Agent entity
	if req.AgentDefinition != nil {
		return req.AgentDefinition.ID
	}
	return "unknown"
}

// resolveAgentName returns a display name for the agent.
func (ae *AgentExecutor) resolveAgentName(req ExecuteRequest) string {
	if req.AgentDefinition != nil && req.AgentDefinition.Name != "" {
		return req.AgentDefinition.Name
	}
	if req.Agent != nil && req.Agent.Name != "" {
		return req.Agent.Name
	}
	return "agent"
}

// resolveDescription returns a description for the agent.
func (ae *AgentExecutor) resolveDescription(req ExecuteRequest) string {
	if req.AgentDefinition != nil && req.AgentDefinition.Description != nil {
		return *req.AgentDefinition.Description
	}
	if req.Agent != nil && req.Agent.Description != nil {
		return *req.Agent.Description
	}
	return ""
}

// resolveInstruction returns the system prompt for the agent.
func (ae *AgentExecutor) resolveInstruction(req ExecuteRequest) string {
	if req.AgentDefinition != nil && req.AgentDefinition.SystemPrompt != nil {
		return *req.AgentDefinition.SystemPrompt
	}
	if req.Agent != nil && req.Agent.Prompt != nil {
		return *req.Agent.Prompt
	}
	return "You are a helpful assistant."
}

// persistMessage persists a single message to the database.
func (ae *AgentExecutor) persistMessage(ctx context.Context, runID, role, text string, stepNumber int) {
	msg := &AgentRunMessage{
		RunID:      runID,
		Role:       role,
		Content:    map[string]any{"text": text},
		StepNumber: stepNumber,
	}
	if err := ae.repo.CreateMessage(ctx, msg); err != nil {
		ae.log.Warn("failed to persist message",
			slog.String("run_id", runID),
			slog.String("role", role),
			slog.String("error", err.Error()),
		)
	}
}

// persistEventContent persists assistant/tool response content from an ADK event.
func (ae *AgentExecutor) persistEventContent(ctx context.Context, runID string, event *session.Event, stepNumber int) {
	if event.Content == nil {
		return
	}

	role := "assistant"
	if event.Author != "" {
		role = event.Author
	}

	// Extract text content from parts
	contentMap := make(map[string]any)
	var textParts []string
	var functionCalls []map[string]any

	for _, part := range event.Content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
		if part.FunctionCall != nil {
			functionCalls = append(functionCalls, map[string]any{
				"name": part.FunctionCall.Name,
				"args": part.FunctionCall.Args,
			})
		}
	}

	if len(textParts) > 0 {
		contentMap["text"] = textParts
	}
	if len(functionCalls) > 0 {
		contentMap["function_calls"] = functionCalls
	}

	if len(contentMap) == 0 {
		return
	}

	msg := &AgentRunMessage{
		RunID:      runID,
		Role:       role,
		Content:    contentMap,
		StepNumber: stepNumber,
	}
	if err := ae.repo.CreateMessage(ctx, msg); err != nil {
		ae.log.Warn("failed to persist event content",
			slog.String("run_id", runID),
			slog.String("role", role),
			slog.String("error", err.Error()),
		)
	}
}

// buildSummary creates a summary map from the final event.
func (ae *AgentExecutor) buildSummary(lastEvent *session.Event, steps int) map[string]any {
	summary := map[string]any{
		"steps": steps,
	}

	if lastEvent != nil && lastEvent.Content != nil {
		var textParts []string
		for _, part := range lastEvent.Content.Parts {
			if part != nil && part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		}
		if len(textParts) > 0 {
			// Take the last text part as the final response
			summary["final_response"] = textParts[len(textParts)-1]
		}
	}

	return summary
}

// sanitizeAgentName ensures the agent name is valid for ADK (no spaces, etc.).
func sanitizeAgentName(name string) string {
	// Replace spaces and special chars with underscores
	result := make([]byte, 0, len(name))
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, byte(c))
		} else {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "agent"
	}
	return string(result)
}

// --- Step Tracker ---

// stepTracker is a thread-safe counter for tracking LLM invocation steps.
// The step count is CUMULATIVE across resumes, unlike systems that reset on each run.
type stepTracker struct {
	mu       sync.Mutex
	steps    int
	maxSteps int
}

func newStepTracker(maxSteps, initialSteps int) *stepTracker {
	return &stepTracker{
		steps:    initialSteps,
		maxSteps: maxSteps,
	}
}

func (st *stepTracker) increment() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.steps++
	return st.steps
}

func (st *stepTracker) current() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.steps
}

func (st *stepTracker) exceeded() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.steps >= st.maxSteps
}

// --- Doom Loop Detector ---

// doomAction defines what the doom loop detector recommends.
type doomAction int

const (
	doomActionNone doomAction = iota
	doomActionWarn
	doomActionStop

	// doomWarnThreshold is the number of consecutive identical calls before warning.
	doomWarnThreshold = 3
	// doomStopThreshold is the number of consecutive identical calls before stopping.
	doomStopThreshold = 5
)

// doomLoopDetector tracks consecutive identical tool calls to detect infinite loops.
type doomLoopDetector struct {
	log              *slog.Logger
	lastToolName     string
	lastArgsHash     string
	consecutiveCount int
}

func newDoomLoopDetector(log *slog.Logger) *doomLoopDetector {
	return &doomLoopDetector{log: log}
}

// recordCall records a tool call and returns an action recommendation.
func (d *doomLoopDetector) recordCall(toolName string, args map[string]any) doomAction {
	argsHash := fmt.Sprintf("%v", args) // Simple hash — good enough for consecutive comparison

	if toolName == d.lastToolName && argsHash == d.lastArgsHash {
		d.consecutiveCount++
	} else {
		d.lastToolName = toolName
		d.lastArgsHash = argsHash
		d.consecutiveCount = 1
	}

	if d.consecutiveCount >= doomStopThreshold {
		return doomActionStop
	}
	if d.consecutiveCount >= doomWarnThreshold {
		return doomActionWarn
	}
	return doomActionNone
}
