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

	"github.com/emergent-company/emergent/domain/workspace"
	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/adk"
	"github.com/emergent-company/emergent/pkg/logger"
)

// StreamEventType identifies the kind of streaming event.
type StreamEventType int

const (
	// StreamEventTextDelta is emitted for each partial text token from the LLM.
	StreamEventTextDelta StreamEventType = iota
	// StreamEventToolCallStart is emitted before a tool is executed.
	StreamEventToolCallStart
	// StreamEventToolCallEnd is emitted after a tool finishes executing.
	StreamEventToolCallEnd
	// StreamEventError is emitted when an error occurs during execution.
	StreamEventError
)

// StreamEvent is a single event emitted during agent execution via StreamCallback.
type StreamEvent struct {
	Type   StreamEventType
	Text   string         // For TextDelta: the incremental text token
	Tool   string         // For ToolCallStart/End: the tool name
	Input  map[string]any // For ToolCallStart: the tool arguments
	Output map[string]any // For ToolCallEnd: the tool result
	Error  string         // For Error/ToolCallEnd: error message
}

// StreamCallback is an optional function invoked for each streaming event during execution.
// When set on ExecuteRequest, it enables real-time streaming of text tokens and tool calls.
type StreamCallback func(event StreamEvent)

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
	TriggerSource   *string
	TriggerMetadata map[string]any
	StreamCallback  StreamCallback // Optional: enables streaming of text deltas and tool call events
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
//
// When workspace provisioning is enabled and an agent definition has a
// workspace_config, the executor automatically provisions a sandboxed
// container before the run starts and tears it down after the run completes.
type AgentExecutor struct {
	modelFactory   *adk.ModelFactory
	toolPool       *ToolPool
	repo           *Repository
	provisioner    *workspace.AutoProvisioner // nil if workspaces are disabled
	wsEnabled      bool                       // cached feature flag
	sessionService session.Service
	log            *slog.Logger
}

// NewAgentExecutor creates a new AgentExecutor.
func NewAgentExecutor(
	modelFactory *adk.ModelFactory,
	toolPool *ToolPool,
	repo *Repository,
	provisioner *workspace.AutoProvisioner,
	cfg *config.Config,
	sessionService session.Service,
	log *slog.Logger,
) *AgentExecutor {
	wsEnabled := cfg.Workspace.IsEnabled()
	if wsEnabled {
		log.Info("agent executor: workspace provisioning enabled")
	}
	return &AgentExecutor{
		modelFactory:   modelFactory,
		toolPool:       toolPool,
		repo:           repo,
		provisioner:    provisioner,
		wsEnabled:      wsEnabled,
		sessionService: sessionService,
		log:            log.With(logger.Scope("agents.executor")),
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
		AgentID:         ae.resolveAgentID(req),
		ParentRunID:     req.ParentRunID,
		MaxSteps:        &maxSteps,
		TriggerSource:   req.TriggerSource,
		TriggerMetadata: req.TriggerMetadata,
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

	// Provision workspace if configured
	hasWorkspaceConfig := ae.wsEnabled && ae.provisioner != nil &&
		req.AgentDefinition != nil && len(req.AgentDefinition.WorkspaceConfig) > 0
	if hasWorkspaceConfig {
		if err := ae.repo.UpdateSessionStatus(ctx, run.ID, SessionStatusProvisioning); err != nil {
			ae.log.Warn("failed to update session status to provisioning",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	wsResult := ae.provisionWorkspace(ctx, run.ID, req)
	if wsResult != nil && wsResult.Workspace != nil {
		defer ae.teardownWorkspace(ctx, wsResult)
	}

	// Workspace provisioning complete (or skipped) — mark session active
	if hasWorkspaceConfig {
		if err := ae.repo.UpdateSessionStatus(ctx, run.ID, SessionStatusActive); err != nil {
			ae.log.Warn("failed to update session status to active",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Build and run the pipeline
	result, err := ae.runPipeline(ctx, run, req, maxSteps, 0, startTime, wsResult, nil)
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

	// Provision workspace if configured
	hasWorkspaceConfig := ae.wsEnabled && ae.provisioner != nil &&
		req.AgentDefinition != nil && len(req.AgentDefinition.WorkspaceConfig) > 0
	if hasWorkspaceConfig {
		if err := ae.repo.UpdateSessionStatus(ctx, newRun.ID, SessionStatusProvisioning); err != nil {
			ae.log.Warn("failed to update session status to provisioning",
				slog.String("run_id", newRun.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	wsResult := ae.provisionWorkspace(ctx, newRun.ID, req)
	if wsResult != nil && wsResult.Workspace != nil {
		defer ae.teardownWorkspace(ctx, wsResult)
	}

	// Workspace provisioning complete (or skipped) — mark session active
	if hasWorkspaceConfig {
		if err := ae.repo.UpdateSessionStatus(ctx, newRun.ID, SessionStatusActive); err != nil {
			ae.log.Warn("failed to update session status to active",
				slog.String("run_id", newRun.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Build and run the pipeline with accumulated step count
	result, err := ae.runPipeline(ctx, newRun, req, maxSteps, priorRun.StepCount, startTime, wsResult, nil)
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

// provisionWorkspace provisions a workspace for the agent run if configured.
// Returns nil if workspace provisioning is disabled, not configured, or not needed.
// Provisioning failures are non-fatal — the agent runs in degraded mode without a workspace.
func (ae *AgentExecutor) provisionWorkspace(ctx context.Context, runID string, req ExecuteRequest) *workspace.ProvisioningResult {
	// Check preconditions: feature enabled, provisioner available, definition has workspace config
	if !ae.wsEnabled || ae.provisioner == nil {
		return nil
	}
	if req.AgentDefinition == nil || len(req.AgentDefinition.WorkspaceConfig) == 0 {
		return nil
	}

	ae.log.Info("provisioning workspace for agent run",
		slog.String("run_id", runID),
		slog.String("agent_definition_id", req.AgentDefinition.ID),
	)

	result, err := ae.provisioner.ProvisionForSession(ctx, req.AgentDefinition.ID, req.ProjectID, req.AgentDefinition.WorkspaceConfig, nil)
	if err != nil {
		ae.log.Error("workspace provisioning returned error, running without workspace",
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
		return nil
	}
	if result == nil {
		// Workspace not enabled in agent definition config
		return nil
	}

	// Link workspace to this run
	if result.Workspace != nil {
		if linkErr := ae.provisioner.LinkToRun(ctx, result.Workspace, runID); linkErr != nil {
			ae.log.Warn("failed to link workspace to run",
				slog.String("run_id", runID),
				slog.String("workspace_id", result.Workspace.ID),
				slog.String("error", linkErr.Error()),
			)
		}
	}

	if result.Degraded {
		ae.log.Warn("workspace provisioned in degraded mode",
			slog.String("run_id", runID),
			slog.String("error", result.Error.Error()),
		)
	} else if result.Workspace != nil {
		ae.log.Info("workspace provisioned successfully",
			slog.String("run_id", runID),
			slog.String("workspace_id", result.Workspace.ID),
		)
	}

	return result
}

// teardownWorkspace destroys the provisioned workspace after the agent run completes.
// Called via defer so it runs regardless of how the run exits.
func (ae *AgentExecutor) teardownWorkspace(ctx context.Context, result *workspace.ProvisioningResult) {
	if result == nil || result.Workspace == nil || ae.provisioner == nil {
		return
	}

	// Use a detached context for teardown since the run context may be cancelled
	teardownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ae.provisioner.TeardownWorkspace(teardownCtx, result.Workspace)
}

func (ae *AgentExecutor) getRootRunID(ctx context.Context, run *AgentRun) string {
	current := run
	for current.ResumedFrom != nil {
		prev, err := ae.repo.FindRunByID(ctx, *current.ResumedFrom)
		if err != nil || prev == nil {
			break
		}
		current = prev
	}
	return current.ID
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
	wsResult *workspace.ProvisioningResult,
	askPauseState *AskPauseState,
) (*ExecuteResult, error) {
	// Identify the root session ID
	sessionID := ae.getRootRunID(ctx, run)
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

	// Add workspace tools if a non-degraded workspace was provisioned
	if wsResult != nil && wsResult.Workspace != nil && !wsResult.Degraded {
		wsTools, wsToolErr := ae.resolveWorkspaceTools(wsResult, req)
		if wsToolErr != nil {
			ae.log.Warn("failed to build workspace tools, continuing without them",
				slog.String("run_id", run.ID),
				slog.String("error", wsToolErr.Error()),
			)
		} else if len(wsTools) > 0 {
			resolvedTools = append(resolvedTools, wsTools...)
			ae.log.Info("workspace tools added to agent pipeline",
				slog.String("run_id", run.ID),
				slog.Int("count", len(wsTools)),
			)
		}
	}

	// Add ask_user tool if opted in via agent definition
	if askPauseState == nil {
		askPauseState = &AskPauseState{}
	}
	askUserTool, askErr := ae.buildAskUserTool(req, run.ID, askPauseState)
	if askErr != nil {
		ae.log.Warn("failed to build ask_user tool, continuing without it",
			slog.String("error", askErr.Error()),
		)
	} else if askUserTool != nil {
		resolvedTools = append(resolvedTools, askUserTool)
		ae.log.Info("ask_user tool added to agent pipeline",
			slog.String("run_id", run.ID),
		)
	}

	// Build the LLM agent
	agentName := ae.resolveAgentName(req)
	instruction := ae.resolveInstruction(req)

	// Augment system instruction with workspace context if available
	if wsResult != nil && wsResult.Workspace != nil && !wsResult.Degraded {
		instruction = ae.augmentInstructionWithWorkspace(instruction, wsResult)
	}

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
		// Check if context was cancelled (timeout or manual cancellation)
		if ctx.Err() != nil {
			ae.log.Warn("context cancelled, stopping agent",
				slog.String("run_id", run.ID),
				slog.String("reason", ctx.Err().Error()),
			)
			return nil, fmt.Errorf("agent stopped: %w", ctx.Err())
		}

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

		// Check if ask_user requested a pause
		if askPauseState != nil && askPauseState.ShouldPause() {
			ae.log.Info("ask_user pause requested, pausing agent",
				slog.String("run_id", run.ID),
				slog.String("question_id", askPauseState.QuestionID()),
				slog.Int("step", currentStep),
			)
			_ = ae.repo.PauseRun(ctx, run.ID, currentStep)
			return &model.LLMResponse{
				Content: genai.NewContentFromText("Execution paused. Waiting for user response to your question.", genai.RoleModel),
			}, nil
		}

		// Periodically persist step count
		if currentStep%5 == 0 {
			_ = ae.repo.UpdateStepCount(ctx, run.ID, currentStep)
		}

		return nil, nil
	}

	// Set up before-tool callback for streaming ToolCallStart events
	beforeToolCb := func(tCtx tool.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		if req.StreamCallback != nil {
			req.StreamCallback(StreamEvent{
				Type:  StreamEventToolCallStart,
				Tool:  t.Name(),
				Input: args,
			})
		}
		return args, nil
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

		// Emit ToolCallEnd streaming event
		if req.StreamCallback != nil {
			evt := StreamEvent{
				Type:   StreamEventToolCallEnd,
				Tool:   toolName,
				Output: output,
			}
			if toolErr != nil {
				evt.Error = toolErr.Error()
			}
			req.StreamCallback(evt)
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
		BeforeToolCallbacks:   []llmagent.BeforeToolCallback{beforeToolCb},
		AfterToolCallbacks:    []llmagent.AfterToolCallback{afterToolCb},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM agent: %w", err)
	}

	// Retrieve or create the persistent session
	sessionService := ae.sessionService
	var sess session.Session

	if sessionID != run.ID {
		// It's a resumed run, attempt to load the existing session
		getResp, err := sessionService.Get(ctx, &session.GetRequest{
			AppName:   "agents",
			UserID:    "system",
			SessionID: sessionID,
		})
		if err == nil && getResp != nil && getResp.Session != nil {
			sess = getResp.Session
			ae.log.Info("resumed ADK session from database",
				slog.String("session_id", sessionID),
				slog.Int("history_events", sess.Events().Len()),
			)
		} else {
			ae.log.Warn("failed to load existing ADK session, falling back to new session",
				slog.String("session_id", sessionID),
				slog.String("error", err.Error()),
			)
		}
	}

	if sess == nil {
		// Create a new session
		createResp, err := sessionService.Create(ctx, &session.CreateRequest{
			AppName:   "agents",
			UserID:    "system",
			SessionID: sessionID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create session: %w", err)
		}
		sess = createResp.Session
	}

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
	runCfg := agent.RunConfig{}
	if req.StreamCallback != nil {
		runCfg.StreamingMode = agent.StreamingModeSSE
	}
	for event, eventErr := range r.Run(ctx, "system", sess.ID(), userContent, runCfg) {
		if eventErr != nil {
			steps := tracker.current()
			if req.StreamCallback != nil {
				req.StreamCallback(StreamEvent{
					Type:  StreamEventError,
					Error: eventErr.Error(),
				})
			}
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

		// Stream partial text deltas to the callback
		if event.Partial && event.Content != nil && req.StreamCallback != nil {
			for _, part := range event.Content.Parts {
				if part != nil && part.Text != "" {
					req.StreamCallback(StreamEvent{
						Type: StreamEventTextDelta,
						Text: part.Text,
					})
				}
			}
		}

		// Persist assistant messages from events
		if event.Content != nil && !event.Partial {
			ae.persistEventContent(ctx, run.ID, event, tracker.current())
		}

		if event.IsFinalResponse() {
			lastEvent = event
		}
	}

	// Check if we exited due to context cancellation (timeout or manual cancellation)
	if ctx.Err() != nil {
		steps := tracker.current()
		errMsg := "Run cancelled"
		reason := "unknown"

		if ctx.Err() == context.DeadlineExceeded {
			reason = "timeout"
			errMsg = "Run cancelled: timeout exceeded"
		} else if ctx.Err() == context.Canceled {
			reason = "cancelled"
			errMsg = "Run cancelled: context cancelled"
		}

		ae.log.Warn("run cancelled by context",
			slog.String("run_id", run.ID),
			slog.String("reason", reason),
			slog.Int("steps", steps),
		)

		_ = ae.repo.FailRunWithSteps(ctx, run.ID, errMsg, steps)
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": errMsg, "reason": reason},
			Steps:    steps,
			Duration: time.Since(startTime),
		}, nil
	}

	// Determine final status
	steps := tracker.current()
	duration := time.Since(startTime)
	durationMs := int(duration.Milliseconds())

	// Check if we were paused by the step limit callback or ask_user tool
	currentRun, _ := ae.repo.FindRunByID(ctx, run.ID)
	if currentRun != nil && currentRun.Status == RunStatusPaused {
		pauseReason := "step_limit_reached"
		pauseSummary := map[string]any{"reason": pauseReason, "steps": steps}
		if askPauseState != nil && askPauseState.ShouldPause() {
			pauseReason = "awaiting_user_input"
			pauseSummary["reason"] = pauseReason
			pauseSummary["question_id"] = askPauseState.QuestionID()
		}
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusPaused,
			Summary:  pauseSummary,
			Steps:    steps,
			Duration: duration,
		}, nil
	}

	// Build summary from the last event
	summary := ae.buildSummary(lastEvent, steps)

	// Include workspace info in summary if provisioned
	if wsResult != nil && wsResult.Workspace != nil {
		summary["workspace_id"] = wsResult.Workspace.ID
		if wsResult.Degraded {
			summary["workspace_degraded"] = true
		}
	}

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

// augmentInstructionWithWorkspace appends workspace context to the system instruction
// so the agent knows it has a sandboxed environment available.
func (ae *AgentExecutor) augmentInstructionWithWorkspace(instruction string, wsResult *workspace.ProvisioningResult) string {
	if wsResult == nil || wsResult.Workspace == nil {
		return instruction
	}

	wsContext := "\n\n## Workspace Environment\n" +
		"You have a sandboxed workspace container available for this run.\n" +
		fmt.Sprintf("- Workspace ID: %s\n", wsResult.Workspace.ID)

	if wsResult.RepoURL != "" {
		wsContext += fmt.Sprintf("- Repository: %s\n", wsResult.RepoURL)
		if wsResult.Branch != "" {
			wsContext += fmt.Sprintf("- Branch: %s\n", wsResult.Branch)
		}
		wsContext += "- Working directory: /workspace\n"
	}

	wsContext += "\nWorkspace tools are prefixed with workspace_ (e.g. workspace_bash, workspace_read, etc.).\n" +
		"Use these tools to interact with files and run commands in the sandboxed container.\n"

	return instruction + wsContext
}

// resolveWorkspaceTools builds ADK tools that let the agent interact with its
// provisioned workspace container (bash, read, write, edit, glob, grep, git).
// Returns nil if the provisioner can't provide a provider for the workspace.
func (ae *AgentExecutor) resolveWorkspaceTools(wsResult *workspace.ProvisioningResult, req ExecuteRequest) ([]tool.Tool, error) {
	if ae.provisioner == nil || wsResult == nil || wsResult.Workspace == nil {
		return nil, nil
	}

	// Get the provider for this workspace
	provider, err := ae.provisioner.GetProviderForWorkspace(wsResult.Workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for workspace %s: %w", wsResult.Workspace.ID, err)
	}

	// Parse workspace config for tool filtering
	var wsCfg *workspace.AgentWorkspaceConfig
	if req.AgentDefinition != nil && len(req.AgentDefinition.WorkspaceConfig) > 0 {
		wsCfg, _ = workspace.ParseAgentWorkspaceConfig(req.AgentDefinition.WorkspaceConfig)
	}

	return BuildWorkspaceTools(WorkspaceToolDeps{
		Provider:    provider,
		ProviderID:  wsResult.Workspace.ProviderWorkspaceID,
		WorkspaceID: wsResult.Workspace.ID,
		Config:      wsCfg,
		Logger:      ae.log,
	})
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

// buildAskUserTool creates the ask_user tool if the agent definition opts in.
// Returns nil if the agent doesn't have ask_user in its tools list.
func (ae *AgentExecutor) buildAskUserTool(req ExecuteRequest, runID string, pauseState *AskPauseState) (tool.Tool, error) {
	if req.AgentDefinition == nil {
		return nil, nil
	}

	// Check if ask_user is in the agent definition's tools list
	hasAskUser := false
	for _, t := range req.AgentDefinition.Tools {
		if t == ToolNameAskUser {
			hasAskUser = true
			break
		}
	}
	if !hasAskUser {
		return nil, nil
	}

	agentID := ae.resolveAgentID(req)

	deps := AskUserToolDeps{
		Repo:       ae.repo,
		Logger:     ae.log,
		ProjectID:  req.ProjectID,
		AgentID:    agentID,
		RunID:      runID,
		PauseState: pauseState,
	}

	return BuildAskUserTool(deps)
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
