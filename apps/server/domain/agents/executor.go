package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/tracing"
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

// ModelLimitsLookup is a narrow interface for querying the max output token limit
// for a given model name from the provider catalog. It is satisfied by
// *provider.Repository but declared here to avoid a direct import of the provider
// domain package into the agents package.
type ModelLimitsLookup interface {
	GetModelOutputLimit(ctx context.Context, modelName string) (int, error)
}

// BudgetExceededError is returned by Execute when a project's monthly spending
// limit has been reached and BUDGET_ENFORCEMENT_ENABLED=true.
type BudgetExceededError struct {
	ProjectID string
	Message   string
}

func (e *BudgetExceededError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("project %s has exceeded its monthly budget", e.ProjectID)
}

// QueueFullError is returned when an agent already has too many pending jobs in the queue.
// This prevents queue explosion by rejecting new runs when the queue is at capacity.
type QueueFullError struct {
	AgentID        string
	PendingJobs    int
	MaxPendingJobs int
}

func (e *QueueFullError) Error() string {
	return fmt.Sprintf("agent %s has %d pending jobs (max %d); run rejected to prevent queue explosion",
		e.AgentID, e.PendingJobs, e.MaxPendingJobs)
}

// callerRunIDKey is the context key used to propagate the calling agent's run ID
// through the execution pipeline so that tool calls (e.g. trigger_agent) can
// identify which run is making the request.
type callerRunIDKey struct{}

// contextWithCallerRunID stores the current run's ID in context so downstream
// tool handlers can read it without needing it in their function signatures.
func contextWithCallerRunID(ctx context.Context, runID string) context.Context {
	return context.WithValue(ctx, callerRunIDKey{}, runID)
}

// callerRunIDFromContext retrieves the calling run's ID from context.
// Returns empty string if not set.
func callerRunIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(callerRunIDKey{}).(string)
	return v
}

// acpSessionIDKey is the context key used to propagate the ACP session ID
// through the execution pipeline so that built-in tools (e.g. set_session_title)
// can update session metadata without needing it in their function signatures.
// NOTE: defined here as a bridge — mcp.ContextWithACPSessionID / mcp.ACPSessionIDFromContext
// use this same exported mechanism via the mcp package.

// ExecuteRequest defines the parameters for executing an agent.
type ExecuteRequest struct {
	Agent           *Agent
	AgentDefinition *AgentDefinition
	ProjectID       string
	OrgID           string
	UserMessage     string
	ParentRunID     *string
	RootRunID       *string // top-level orchestration run ID; propagated unchanged through all sub-agent spawns
	MaxSteps        *int
	Timeout         *time.Duration
	Depth           int
	MaxDepth        int
	TriggerSource   *string
	TriggerMetadata map[string]any
	StreamCallback  StreamCallback // Optional: enables streaming of text deltas and tool call events
	Model           string         // Optional per-run model override; takes precedence over AgentDefinition.Model

	// Ephemeral sandbox token — set by the chat handler before calling Execute.
	// AuthToken is the raw emt_* token value to inject into sandbox containers as MEMORY_API_KEY.
	// EphemeralTokenID is the DB id of the token; the executor revokes it on workspace teardown.
	AuthToken        string
	EphemeralTokenID string

	// EnvVars are runtime environment variables passed per-run by the caller.
	// They are merged into the sandbox container environment with lower priority
	// than system keys (MEMORY_API_KEY, MEMORY_PROJECT_ID, MEMORY_SERVER_URL).
	EnvVars map[string]string

	// ACPSessionID links this run to an ACP session so built-in tools like
	// set_session_title can update session metadata during execution.
	ACPSessionID string

	// UserID is the authenticated user who initiated this run (empty for system/background runs).
	// Passed to ask_user tool so notifications target the correct user.
	UserID string
}

// ExecuteResult is the outcome of an agent execution.
type ExecuteResult struct {
	RunID    string
	Status   AgentRunStatus
	Summary  map[string]any
	Steps    int
	Duration time.Duration

	// Cleanup tears down the workspace (container + ephemeral token) provisioned
	// for this run.  It is safe to call multiple times (idempotent via sync.Once).
	//
	// The executor always defers Cleanup as a safety net, but callers that want
	// lower latency (e.g. SSE streams) can call Cleanup *asynchronously* after
	// they have finished writing the response — the deferred call will then be a
	// no-op.
	Cleanup func()
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
	skillRepo      *skills.Repository
	embeddingsSvc  *embeddings.Service
	provisioner    *sandbox.AutoProvisioner // nil if workspaces are disabled
	wsEnabled      bool                     // cached feature flag
	sessionService session.Service
	modelLimits    ModelLimitsLookup // nil if provider module is not registered
	apiTokenSvc    *apitoken.Service // nil if not configured; used for ephemeral sandbox tokens
	usageService   *provider.UsageService
	eventsSvc      *events.Service // nil if events module not registered; used by ask_user SSE notification
	safeguards     config.AgentSafeguardsConfig
	log            *slog.Logger
}

// NewAgentExecutor creates a new AgentExecutor.
func NewAgentExecutor(
	modelFactory *adk.ModelFactory,
	toolPool *ToolPool,
	repo *Repository,
	skillRepo *skills.Repository,
	embeddingsSvc *embeddings.Service,
	provisioner *sandbox.AutoProvisioner,
	cfg *config.Config,
	sessionService session.Service,
	modelLimits ModelLimitsLookup,
	apiTokenSvc *apitoken.Service,
	usageService *provider.UsageService,
	eventsSvc *events.Service,
	log *slog.Logger,
) *AgentExecutor {
	wsEnabled := cfg.Sandbox.IsEnabled()
	if wsEnabled {
		log.Info("agent executor: workspace provisioning enabled")
	}
	return &AgentExecutor{
		modelFactory:   modelFactory,
		toolPool:       toolPool,
		repo:           repo,
		skillRepo:      skillRepo,
		embeddingsSvc:  embeddingsSvc,
		provisioner:    provisioner,
		wsEnabled:      wsEnabled,
		sessionService: sessionService,
		modelLimits:    modelLimits,
		apiTokenSvc:    apiTokenSvc,
		usageService:   usageService,
		eventsSvc:      eventsSvc,
		safeguards:     cfg.AgentSafeguards,
		log:            log.With(logger.Scope("agents.executor")),
	}
}

// Execute runs an agent from scratch using the provided request.
func (ae *AgentExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	startTime := time.Now()
	dbCtx := context.Background()

	// Emergency kill switch — blocks all agent execution when disabled.
	if !ae.safeguards.ExecutionEnabled {
		ae.log.Error("agent execution blocked by kill switch",
			slog.String("project_id", req.ProjectID),
			slog.String("agent_id", ae.resolveAgentID(req)),
		)
		return nil, fmt.Errorf("agent execution is disabled system-wide")
	}

	// Budget pre-flight check — hard stop when project has exceeded its monthly budget.
	if ae.usageService != nil && req.ProjectID != "" {
		exceeded, err := ae.usageService.CheckBudgetExceeded(ctx, req.ProjectID)
		if err != nil {
			// Fail-open: log warning but proceed so a broken budget query never halts agents.
			ae.log.Warn("budget pre-flight check failed, proceeding",
				slog.String("project_id", req.ProjectID),
				slog.String("error", err.Error()),
			)
		} else if exceeded && ae.safeguards.BudgetEnforcementEnabled {
			ae.log.Warn("agent execution blocked: project budget exceeded",
				slog.String("project_id", req.ProjectID),
				slog.String("agent_id", ae.resolveAgentID(req)),
			)
			return nil, &BudgetExceededError{
				ProjectID: req.ProjectID,
				Message:   fmt.Sprintf("project %s has exceeded its monthly spending budget", req.ProjectID),
			}
		}
	}

	// Validate depth
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if req.Depth > maxDepth {
		return nil, fmt.Errorf("max agent depth %d exceeded (current depth: %d)", maxDepth, req.Depth)
	}

	// Determine max steps for this run: per-request override > definition > default
	maxSteps := DefaultMaxStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 {
		maxSteps = *req.MaxSteps
	} else if req.AgentDefinition != nil && req.AgentDefinition.MaxSteps != nil && *req.AgentDefinition.MaxSteps > 0 {
		maxSteps = *req.AgentDefinition.MaxSteps
	}

	// Create the run record
	createOpts := CreateRunOptions{
		AgentID:         ae.resolveAgentID(req),
		ParentRunID:     req.ParentRunID,
		MaxSteps:        &maxSteps,
		TriggerSource:   req.TriggerSource,
		TriggerMetadata: req.TriggerMetadata,
	}
	if req.AgentDefinition != nil {
		createOpts.AgentDefinitionID = &req.AgentDefinition.ID
	}

	run, err := ae.repo.CreateRunWithOptions(dbCtx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent run: %w", err)
	}

	// Link to ACP session if provided via request (e.g. trigger_agent sync path).
	if req.ACPSessionID != "" {
		run.ACPSessionID = &req.ACPSessionID
		if updateErr := ae.repo.UpdateRunACPSessionID(dbCtx, run.ID, req.ACPSessionID); updateErr != nil {
			ae.log.Warn("failed to persist acp_session_id on agent run",
				slog.String("run_id", run.ID),
				slog.String("error", updateErr.Error()),
			)
		}
	}

	// Establish root_run_id: top-level runs own it; sub-agents receive it from the parent.
	if req.RootRunID == nil {
		req.RootRunID = &run.ID
	}

	// Start OTel span now that we have the run ID
	agentName := ae.resolveAgentName(req)
	modelName := req.Model
	if modelName == "" && req.AgentDefinition != nil && req.AgentDefinition.Model != nil {
		modelName = req.AgentDefinition.Model.Name
	}
	ctx, span := tracing.Start(ctx, "agent.run",
		attribute.String("memory.agent.id", ae.resolveAgentID(req)),
		attribute.String("memory.agent.name", agentName),
		attribute.String("memory.agent.run_id", run.ID),
		attribute.String("memory.agent.root_run_id", *req.RootRunID),
		attribute.String("memory.project.id", req.ProjectID),
		attribute.String("memory.agent.model", modelName),
	)
	defer span.End()

	// Persist trace_id and root_run_id back to the run row so the reverse link
	// (run → trace, run → orchestration root) is queryable without OTEL.
	if sc := span.SpanContext(); sc.IsValid() {
		if err := ae.repo.UpdateTraceAndRootRun(dbCtx, run.ID, sc.TraceID().String(), *req.RootRunID); err != nil {
			ae.log.Warn("failed to persist trace_id/root_run_id on agent run",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	ae.log.Info("executing agent",
		slog.String("run_id", run.ID),
		slog.String("project_id", req.ProjectID),
		slog.String("agent_name", agentName),
		slog.Int("depth", req.Depth),
		slog.Int("max_steps", maxSteps),
	)

	// Inject auth token into context so internal loopback calls (e.g. search-knowledge → /query)
	// can authenticate. For HTTP-triggered runs the raw token is already in ctx via auth middleware;
	// for background/scheduled runs there is none, so mint a short-lived ephemeral token.
	{
		effectiveToken := req.AuthToken
		if effectiveToken == "" {
			effectiveToken = auth.RawTokenFromContext(ctx)
		}
		if effectiveToken == "" && ae.apiTokenSvc != nil && req.ProjectID != "" && req.OrgID != "" {
			if ephID, ephToken, mintErr := ae.apiTokenSvc.CreateEphemeral(ctx, req.ProjectID, req.OrgID, "", 2*time.Hour); mintErr == nil {
				effectiveToken = ephToken
				req.EphemeralTokenID = ephID
			} else {
				ae.log.Warn("failed to mint ephemeral token for background agent run",
					slog.String("project_id", req.ProjectID),
					slog.String("error", mintErr.Error()),
				)
			}
		}
		if effectiveToken != "" {
			ctx = auth.ContextWithRawToken(ctx, effectiveToken)
		}
	}

	// Provision workspace if configured
	hasSandboxConfig := ae.wsEnabled && ae.provisioner != nil &&
		req.AgentDefinition != nil && len(req.AgentDefinition.SandboxConfig) > 0
	if hasSandboxConfig {
		if err := ae.repo.UpdateSessionStatus(dbCtx, run.ID, SessionStatusProvisioning); err != nil {
			ae.log.Warn("failed to update session status to provisioning",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	wsResult, wsErr := ae.provisionWorkspace(ctx, run.ID, req)
	if wsErr != nil {
		// Fatal provisioning failure (e.g. image not ready) — fail the run
		errMsg := wsErr.Error()
		_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, errMsg, 0)
		span.RecordError(wsErr)
		span.SetStatus(codes.Error, errMsg)
		span.SetAttributes(
			attribute.Int("memory.agent.step_count", 0),
			attribute.String("memory.agent.run_status", string(RunStatusError)),
		)
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": errMsg},
			Steps:    0,
			Duration: time.Since(startTime),
		}, nil
	}

	// Build an idempotent cleanup function so teardown runs exactly once.
	// Callers are responsible for invoking Cleanup on the returned ExecuteResult.
	// SSE callers can invoke it asynchronously after flushing the response;
	// non-SSE callers should defer it or call it synchronously.
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			ae.teardownWorkspace(ctx, wsResult, req.EphemeralTokenID)
		})
	}

	// Workspace provisioning complete (or skipped) — mark session active
	if hasSandboxConfig {
		if err := ae.repo.UpdateSessionStatus(dbCtx, run.ID, SessionStatusActive); err != nil {
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
		_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, err.Error(), 0)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(
			attribute.Int("memory.agent.step_count", 0),
			attribute.String("memory.agent.run_status", string(RunStatusError)),
		)
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": err.Error()},
			Steps:    0,
			Duration: time.Since(startTime),
			Cleanup:  cleanup,
		}, nil
	}

	// Record final run outcome on the span
	span.SetAttributes(
		attribute.Int("memory.agent.step_count", result.Steps),
		attribute.String("memory.agent.run_status", string(result.Status)),
	)
	switch result.Status {
	case RunStatusPaused:
		span.AddEvent("agent.max_steps_reached", trace.WithAttributes(
			attribute.Int("memory.agent.step_count", result.Steps),
		))
		span.SetStatus(codes.Ok, "")
	case RunStatusError:
		errMsg := ""
		if e, ok := result.Summary["error"].(string); ok {
			errMsg = e
		}
		span.SetStatus(codes.Error, errMsg)
	default:
		span.SetStatus(codes.Ok, "")
	}

	result.Cleanup = cleanup
	return result, nil
}

// ExecuteWithRun executes an agent using a pre-created run record.
// This is used by the HTTP trigger endpoint to decouple run creation
// (synchronous, returns immediately) from actual execution (async).
func (ae *AgentExecutor) ExecuteWithRun(ctx context.Context, run *AgentRun, req ExecuteRequest) (*ExecuteResult, error) {
	startTime := time.Now()
	dbCtx := context.Background()

	// Validate depth
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if req.Depth > maxDepth {
		return nil, fmt.Errorf("max agent depth %d exceeded (current depth: %d)", maxDepth, req.Depth)
	}

	// Determine max steps for this run: per-request override > definition > default
	maxSteps := DefaultMaxStepsPerRun
	if req.MaxSteps != nil && *req.MaxSteps > 0 {
		maxSteps = *req.MaxSteps
	} else if req.AgentDefinition != nil && req.AgentDefinition.MaxSteps != nil && *req.AgentDefinition.MaxSteps > 0 {
		maxSteps = *req.AgentDefinition.MaxSteps
	}

	// Establish root_run_id: top-level runs own it; sub-agents receive it from the parent.
	if req.RootRunID == nil {
		req.RootRunID = &run.ID
	}

	// Start OTel span
	agentName := ae.resolveAgentName(req)
	modelName := req.Model
	if modelName == "" && req.AgentDefinition != nil && req.AgentDefinition.Model != nil {
		modelName = req.AgentDefinition.Model.Name
	}
	ctx, span := tracing.Start(ctx, "agent.run",
		attribute.String("memory.agent.id", ae.resolveAgentID(req)),
		attribute.String("memory.agent.name", agentName),
		attribute.String("memory.agent.run_id", run.ID),
		attribute.String("memory.agent.root_run_id", *req.RootRunID),
		attribute.String("memory.project.id", req.ProjectID),
		attribute.String("memory.agent.model", modelName),
	)
	defer span.End()

	// Persist trace_id and root_run_id back to the run row so the reverse link
	// (run → trace, run → orchestration root) is queryable without OTEL.
	if sc := span.SpanContext(); sc.IsValid() {
		if err := ae.repo.UpdateTraceAndRootRun(ctx, run.ID, sc.TraceID().String(), *req.RootRunID); err != nil {
			ae.log.Warn("failed to persist trace_id/root_run_id on agent run",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Persist the resolved model name on the run record for observability (#141)
	if modelName != "" {
		if err := ae.repo.UpdateRunModel(dbCtx, run.ID, modelName); err != nil {
			ae.log.Warn("failed to persist model on agent run",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	ae.log.Info("executing agent (async)",
		slog.String("run_id", run.ID),
		slog.String("project_id", req.ProjectID),
		slog.String("agent_name", agentName),
		slog.Int("depth", req.Depth),
		slog.Int("max_steps", maxSteps),
	)

	// Provision workspace if configured
	hasSandboxConfig := ae.wsEnabled && ae.provisioner != nil &&
		req.AgentDefinition != nil && len(req.AgentDefinition.SandboxConfig) > 0
	if hasSandboxConfig {
		if err := ae.repo.UpdateSessionStatus(dbCtx, run.ID, SessionStatusProvisioning); err != nil {
			ae.log.Warn("failed to update session status to provisioning",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	wsResult, wsErr := ae.provisionWorkspace(ctx, run.ID, req)
	if wsErr != nil {
		errMsg := wsErr.Error()
		_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, errMsg, 0)
		span.RecordError(wsErr)
		span.SetStatus(codes.Error, errMsg)
		span.SetAttributes(
			attribute.Int("memory.agent.step_count", 0),
			attribute.String("memory.agent.run_status", string(RunStatusError)),
		)
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": errMsg},
			Steps:    0,
			Duration: time.Since(startTime),
		}, nil
	}

	// Build an idempotent cleanup function so teardown runs exactly once.
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			ae.teardownWorkspace(ctx, wsResult, req.EphemeralTokenID)
		})
	}

	if hasSandboxConfig {
		if err := ae.repo.UpdateSessionStatus(ctx, run.ID, SessionStatusActive); err != nil {
			ae.log.Warn("failed to update session status to active",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	result, err := ae.runPipeline(ctx, run, req, maxSteps, 0, startTime, wsResult, nil)
	if err != nil {
		_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, err.Error(), 0)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(
			attribute.Int("memory.agent.step_count", 0),
			attribute.String("memory.agent.run_status", string(RunStatusError)),
		)
		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": err.Error()},
			Steps:    0,
			Duration: time.Since(startTime),
			Cleanup:  cleanup,
		}, nil
	}

	span.SetAttributes(
		attribute.Int("memory.agent.step_count", result.Steps),
		attribute.String("memory.agent.run_status", string(result.Status)),
	)
	switch result.Status {
	case RunStatusPaused:
		span.AddEvent("agent.max_steps_reached", trace.WithAttributes(
			attribute.Int("memory.agent.step_count", result.Steps),
		))
		span.SetStatus(codes.Ok, "")
	case RunStatusError:
		errMsg := ""
		if e, ok := result.Summary["error"].(string); ok {
			errMsg = e
		}
		span.SetStatus(codes.Error, errMsg)
	default:
		span.SetStatus(codes.Ok, "")
	}

	result.Cleanup = cleanup
	return result, nil
}
func (ae *AgentExecutor) Resume(ctx context.Context, priorRun *AgentRun, req ExecuteRequest) (*ExecuteResult, error) {
	startTime := time.Now()
	dbCtx := context.Background()

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
	newRun, err := ae.repo.CreateRunWithOptions(dbCtx, CreateRunOptions{
		AgentID:          priorRun.AgentID,
		ParentRunID:      req.ParentRunID,
		MaxSteps:         &maxSteps,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: priorRun.StepCount,
		TriggerMetadata:  priorRun.TriggerMetadata,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create resumed run: %w", err)
	}

	// Establish root_run_id for resumed runs: inherit from caller or default to own ID.
	if req.RootRunID == nil {
		req.RootRunID = &newRun.ID
	}

	// Persist root_run_id on the resumed run row. trace_id is omitted here since
	// Resume has no dedicated agent.run span — the caller's HTTP span context is
	// not meaningful to store as the run's trace.
	if err := ae.repo.UpdateTraceAndRootRun(dbCtx, newRun.ID, "", *req.RootRunID); err != nil {
		ae.log.Warn("failed to persist root_run_id on resumed agent run",
			slog.String("run_id", newRun.ID),
			slog.String("error", err.Error()),
		)
	}

	ae.log.Info("resuming agent",
		slog.String("run_id", newRun.ID),
		slog.String("resumed_from", priorRun.ID),
		slog.Int("prior_steps", priorRun.StepCount),
		slog.Int("max_steps", maxSteps),
	)

	// Inject auth token into context (same logic as Execute — handles background/scheduled resumes).
	{
		effectiveToken := req.AuthToken
		if effectiveToken == "" {
			effectiveToken = auth.RawTokenFromContext(ctx)
		}
		if effectiveToken == "" && ae.apiTokenSvc != nil && req.ProjectID != "" && req.OrgID != "" {
			if ephID, ephToken, mintErr := ae.apiTokenSvc.CreateEphemeral(ctx, req.ProjectID, req.OrgID, "", 2*time.Hour); mintErr == nil {
				effectiveToken = ephToken
				req.EphemeralTokenID = ephID
			} else {
				ae.log.Warn("failed to mint ephemeral token for background agent resume",
					slog.String("project_id", req.ProjectID),
					slog.String("error", mintErr.Error()),
				)
			}
		}
		if effectiveToken != "" {
			ctx = auth.ContextWithRawToken(ctx, effectiveToken)
		}
	}

	// Provision workspace if configured
	hasSandboxConfig := ae.wsEnabled && ae.provisioner != nil &&
		req.AgentDefinition != nil && len(req.AgentDefinition.SandboxConfig) > 0
	if hasSandboxConfig {
		if err := ae.repo.UpdateSessionStatus(dbCtx, newRun.ID, SessionStatusProvisioning); err != nil {
			ae.log.Warn("failed to update session status to provisioning",
				slog.String("run_id", newRun.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	wsResult, wsErr := ae.provisionWorkspace(ctx, newRun.ID, req)
	if wsErr != nil {
		errMsg := wsErr.Error()
		_ = ae.repo.FailRunWithSteps(dbCtx, newRun.ID, errMsg, priorRun.StepCount)
		return &ExecuteResult{
			RunID:    newRun.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": errMsg},
			Steps:    priorRun.StepCount,
			Duration: time.Since(startTime),
		}, nil
	}

	// Build an idempotent cleanup function so teardown runs exactly once.
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			ae.teardownWorkspace(ctx, wsResult, req.EphemeralTokenID)
		})
	}

	// Workspace provisioning complete (or skipped) — mark session active
	if hasSandboxConfig {
		if err := ae.repo.UpdateSessionStatus(dbCtx, newRun.ID, SessionStatusActive); err != nil {
			ae.log.Warn("failed to update session status to active",
				slog.String("run_id", newRun.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Build and run the pipeline with accumulated step count
	result, err := ae.runPipeline(ctx, newRun, req, maxSteps, priorRun.StepCount, startTime, wsResult, nil)
	if err != nil {
		_ = ae.repo.FailRunWithSteps(dbCtx, newRun.ID, err.Error(), priorRun.StepCount)
		return &ExecuteResult{
			RunID:    newRun.ID,
			Status:   RunStatusError,
			Summary:  map[string]any{"error": err.Error()},
			Steps:    priorRun.StepCount,
			Duration: time.Since(startTime),
			Cleanup:  cleanup,
		}, nil
	}

	result.Cleanup = cleanup
	return result, nil
}

// provisionWorkspace provisions a workspace for the agent run if configured.
// Returns (nil, nil) if workspace provisioning is disabled, not configured, or not needed.
// Returns (result, nil) on success or degraded provisioning.
// Returns (nil, error) for fatal pre-provisioning failures (e.g. image not ready) —
// callers must fail the run with RunStatusError.
func (ae *AgentExecutor) provisionWorkspace(ctx context.Context, runID string, req ExecuteRequest) (*sandbox.ProvisioningResult, error) {
	// Check preconditions: feature enabled, provisioner available, definition has workspace config
	if !ae.wsEnabled || ae.provisioner == nil {
		return nil, nil
	}
	if req.AgentDefinition == nil || len(req.AgentDefinition.SandboxConfig) == 0 {
		return nil, nil
	}

	ae.log.Info("provisioning workspace for agent run",
		slog.String("run_id", runID),
		slog.String("agent_definition_id", req.AgentDefinition.ID),
	)

	// Check image readiness before attempting provisioning.
	// If the image is still pulling or in error state, fail the run immediately
	// rather than silently degrading to stub tools.
	if err := ae.provisioner.WaitForImageReady(ctx, req.ProjectID, req.AgentDefinition.SandboxConfig); err != nil {
		ae.log.Error("sandbox image not ready, failing run",
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("sandbox image not ready: %w", err)
	}

	// Convert TriggerMetadata (map[string]any) to pass as taskMetadata
	taskMeta := make(map[string]any, len(req.TriggerMetadata))
	for k, v := range req.TriggerMetadata {
		taskMeta[k] = v
	}
	result, err := ae.provisioner.ProvisionForSession(ctx, req.AgentDefinition.ID, req.ProjectID, req.AgentDefinition.SandboxConfig, taskMeta, req.AuthToken, req.EnvVars)
	if err != nil {
		ae.log.Error("workspace provisioning failed, failing run",
			slog.String("run_id", runID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("workspace provisioning failed: %w", err)
	}
	if result == nil {
		// Workspace not enabled in agent definition config
		return nil, nil
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
		ae.log.Error("workspace provisioning degraded, failing run",
			slog.String("run_id", runID),
			slog.String("error", result.Error.Error()),
		)
		return nil, fmt.Errorf("workspace provisioning failed: %w", result.Error)
	} else if result.Workspace != nil {
		ae.log.Info("workspace provisioned successfully",
			slog.String("run_id", runID),
			slog.String("workspace_id", result.Workspace.ID),
		)
	}

	return result, nil
}

// teardownWorkspace destroys the provisioned workspace after the agent run completes.
// Called via defer so it runs regardless of how the run exits.
func (ae *AgentExecutor) teardownWorkspace(ctx context.Context, result *sandbox.ProvisioningResult, tokenID string) {
	if result == nil || result.Workspace == nil || ae.provisioner == nil {
		return
	}

	// Use a detached context for teardown since the run context may be cancelled
	teardownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ae.provisioner.TeardownWorkspace(teardownCtx, result.Workspace)

	// Revoke the ephemeral token if one was minted for this run
	if tokenID != "" && ae.apiTokenSvc != nil {
		ae.apiTokenSvc.RevokeEphemeral(teardownCtx, tokenID)
	}
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
	wsResult *sandbox.ProvisioningResult,
	askPauseState *AskPauseState,
) (*ExecuteResult, error) {
	dbCtx := context.Background()

	// Identify the root session ID
	sessionID := ae.getRootRunID(ctx, run)

	// Inject the current run ID into context so downstream tools (e.g. trigger_agent)
	// can propagate it as the parent_run_id when spawning child runs.
	ctx = contextWithCallerRunID(ctx, run.ID)
	// Inject ACP session ID into context so built-in tools (e.g. set_session_title)
	// can update session metadata. If the run was created without the session ID set
	// in-memory (e.g. via trigger_agent async path), fetch it from DB as a fallback.
	if run.ACPSessionID != nil && *run.ACPSessionID != "" {
		ctx = mcp.ContextWithACPSessionID(ctx, *run.ACPSessionID)
	} else if run.ID != "" {
		if freshRun, fetchErr := ae.repo.FindRunByID(dbCtx, run.ID); fetchErr == nil && freshRun != nil && freshRun.ACPSessionID != nil && *freshRun.ACPSessionID != "" {
			run.ACPSessionID = freshRun.ACPSessionID
			ctx = mcp.ContextWithACPSessionID(ctx, *freshRun.ACPSessionID)
		}
	}
	// Also inject into the provider context so the tracking model can attribute
	// LLM usage events to this run.
	ctx = provider.ContextWithRunID(ctx, run.ID)
	// Inject the root orchestration run ID so the tracking model can attribute
	// cost to the full orchestration tree, not just the immediate run.
	if req.RootRunID != nil {
		ctx = provider.ContextWithRootRunID(ctx, *req.RootRunID)
	}

	// Inject project and org IDs into context so the credential resolver can look up
	// the org-level provider config via the DB hierarchy (project → org), and so
	// the tracking model can attribute LLM usage events to the correct tenant.
	if req.ProjectID != "" {
		ctx = auth.ContextWithProjectID(ctx, req.ProjectID)
	}
	// Prefer explicitly supplied token; fall back to one already in context (e.g.
	// when the executor is called from a trigger, worker pool, or spawned agent
	// where the caller doesn't set AuthToken but the context carries a token from
	// the auth middleware or a parent executor call).
	effectiveToken := req.AuthToken
	if effectiveToken == "" {
		effectiveToken = auth.RawTokenFromContext(ctx)
	}
	// For background/scheduled runs there is no HTTP request context, so no token
	// is stored yet. Mint a short-lived ephemeral token so internal loopback calls
	// (e.g. search-knowledge → /query) can authenticate. The token is revoked at
	// teardown via req.EphemeralTokenID.
	if effectiveToken == "" && ae.apiTokenSvc != nil && req.ProjectID != "" && req.OrgID != "" {
		if ephID, ephToken, mintErr := ae.apiTokenSvc.CreateEphemeral(ctx, req.ProjectID, req.OrgID, "", 2*time.Hour); mintErr == nil {
			effectiveToken = ephToken
			// Store on req so teardownWorkspace can revoke it.
			req.EphemeralTokenID = ephID
		} else {
			ae.log.Warn("failed to mint ephemeral token for background agent run",
				slog.String("project_id", req.ProjectID),
				slog.String("error", mintErr.Error()),
			)
		}
	}
	if effectiveToken != "" {
		ctx = auth.ContextWithRawToken(ctx, effectiveToken)
	}
	if req.OrgID != "" {
		ctx = auth.ContextWithOrgID(ctx, req.OrgID)
	}

	// Apply timeout if specified
	if req.Timeout != nil && *req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *req.Timeout)
		defer cancel()
	}

	// Create a cancellable context so the doom loop detector can hard-stop the run.
	var cancelRun context.CancelFunc
	ctx, cancelRun = context.WithCancel(ctx)
	defer cancelRun()
	var cancelReason string // set before calling cancelRun() to give a meaningful error message

	// Create the LLM model — per-run override takes precedence
	modelName := req.Model
	if modelName == "" && req.AgentDefinition != nil && req.AgentDefinition.Model != nil && req.AgentDefinition.Model.Name != "" {
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

	// Persist the resolved model name (and provider when available) on the run record.
	// llm.Name() reflects the actual model used (including factory/credential defaults),
	// which may differ from the modelName variable when a credential-level default applies.
	if resolvedModelName := llm.Name(); resolvedModelName != "" {
		var providerName string
		if tm, ok := llm.(*provider.TrackingModel); ok {
			providerName = tm.ProviderName()
		}
		if err := ae.repo.UpdateRunModel(dbCtx, run.ID, resolvedModelName, providerName); err != nil {
			ae.log.Warn("failed to persist model on agent run",
				slog.String("run_id", run.ID),
				slog.String("model", resolvedModelName),
				slog.String("provider", providerName),
				slog.String("error", err.Error()),
			)
		}
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

	// Filter out banned tools
	if req.AgentDefinition != nil && len(req.AgentDefinition.BannedTools) > 0 {
		banned := make(map[string]bool)
		for _, name := range req.AgentDefinition.BannedTools {
			banned[name] = true
		}
		filtered := make([]tool.Tool, 0, len(resolvedTools))
		for _, t := range resolvedTools {
			if !banned[t.Name()] {
				filtered = append(filtered, t)
			}
		}
		resolvedTools = filtered
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

	// Add workspace tools if a non-degraded workspace was provisioned.
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

	// Add skill tool if opted in via agent definition
	if skillTool, skillErr := ae.buildSkillTool(ctx, run, req); skillErr != nil {
		ae.log.Warn("failed to build skill tool, continuing without it",
			slog.String("run_id", run.ID),
			slog.String("error", skillErr.Error()),
		)
	} else if skillTool != nil {
		resolvedTools = append(resolvedTools, skillTool)
		ae.log.Info("skill tool added to agent pipeline",
			slog.String("run_id", run.ID),
		)
	}

	// Persist the final tool list on the run record for observability
	if len(resolvedTools) > 0 {
		toolNames := make([]string, len(resolvedTools))
		for i, t := range resolvedTools {
			toolNames[i] = t.Name()
		}
		if err := ae.repo.UpdateRunTools(dbCtx, run.ID, toolNames); err != nil {
			ae.log.Warn("failed to persist tools on agent run",
				slog.String("run_id", run.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	// Build the LLM agent
	agentName := ae.resolveAgentName(req)
	instruction := ae.resolveInstruction(req)

	// Augment system instruction with workspace context if available.
	// If workspace was requested but provisioning failed or is degraded, inject a clear
	// unavailability notice so the model doesn't attempt to call workspace tools that
	// have no registered handler (which would cause silent tool-call drops).
	// Skip injection when contextInjection is set to "never" — the agent owns its prompt.
	if wsResult != nil && wsResult.Workspace != nil && !wsResult.Degraded &&
		contextInjectionMode(req.AgentDefinition) != "never" {
		instruction = ae.augmentInstructionWithWorkspace(instruction, wsResult)
	}

	// TriggerMetadata is per-run dynamic context (e.g. title, source, caller-supplied keys).
	// It is injected as a prefix on the first user-turn message rather than the system
	// instruction, so the system instruction remains byte-identical across runs and can
	// be served from Gemini's implicit prompt cache (requires a stable prefix).
	var triggerContextPrefix string
	if len(run.TriggerMetadata) > 0 {
		if ctxJSON, err := json.Marshal(run.TriggerMetadata); err == nil {
			triggerContextPrefix = "<context>\n" + string(ctxJSON) + "\n</context>\n\n"
		}
	}

	genConfig := ae.modelFactory.DefaultGenerateConfig()
	if req.AgentDefinition != nil && req.AgentDefinition.Model != nil {
		if req.AgentDefinition.Model.Temperature != nil {
			genConfig.Temperature = req.AgentDefinition.Model.Temperature
		}
		if req.AgentDefinition.Model.MaxTokens != nil {
			// Explicit per-agent override takes highest priority.
			genConfig.MaxOutputTokens = int32(*req.AgentDefinition.Model.MaxTokens)
		} else if ae.modelLimits != nil && modelName != "" {
			// Fall back to the models.dev catalog limit for this model.
			if limit, err := ae.modelLimits.GetModelOutputLimit(ctx, modelName); err == nil && limit > 0 {
				genConfig.MaxOutputTokens = int32(limit)
			}
		}
		// Inject Google-native tools (google_search, url_context, code_execution).
		// Only tools that are both requested by the agent definition AND supported
		// by the resolved model are activated — unsupported combinations are skipped.
		if len(req.AgentDefinition.Model.NativeTools) > 0 {
			supported := adk.SupportedNativeTools(modelName)
			enabled := adk.IntersectNativeTools(req.AgentDefinition.Model.NativeTools, supported)
			if len(enabled) > 0 {
				nativeGenaiTools := adk.BuildNativeGenaiTools(enabled)
				genConfig.Tools = append(genConfig.Tools, nativeGenaiTools...)
				ae.log.Info("google native tools enabled for agent",
					slog.String("run_id", run.ID),
					slog.String("model", modelName),
					slog.Any("tools", enabled),
				)
			}
		}
	}

	// Create the step tracker
	tracker := newStepTracker(maxSteps, initialSteps)

	// Create the doom loop detector
	doomDetector := newDoomLoopDetector(ae.log)

	// Accumulate cached token counts across all LLM steps in this run.
	// Used to surface cache hit visibility in the run summary.
	var cachedTokensMu sync.Mutex
	var totalCachedTokens int64

	// Set up before-model callback for step tracking
	beforeModelCb := func(cbCtx agent.CallbackContext, llmReq *model.LLMRequest) (*model.LLMResponse, error) {
		// Check if context was cancelled (timeout or manual cancellation)
		if ctx.Err() != nil {
			msg := cancelReason
			if msg == "" {
				if ctx.Err() == context.DeadlineExceeded {
					msg = "agent stopped: timeout exceeded"
				} else {
					msg = "agent stopped: context canceled"
				}
			}
			ae.log.Warn("context cancelled, stopping agent",
				slog.String("run_id", run.ID),
				slog.String("reason", msg),
			)
			return nil, fmt.Errorf("%s", msg)
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
			_ = ae.repo.PauseRun(dbCtx, run.ID, currentStep)
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
			_ = ae.repo.PauseRun(dbCtx, run.ID, currentStep)
			return &model.LLMResponse{
				Content: genai.NewContentFromText("Execution paused. Waiting for user response to your question.", genai.RoleModel),
			}, nil
		}

		// Periodically persist step count
		if currentStep%5 == 0 {
			_ = ae.repo.UpdateStepCount(dbCtx, run.ID, currentStep)
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
		// Return nil to let the ADK framework proceed with actual tool execution.
		// Returning a non-nil result tells the framework the callback already handled
		// the tool call and skips tool.Run() entirely (Bug 6 fix).
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
		if persistErr := ae.repo.CreateToolCall(dbCtx, tcRecord); persistErr != nil {
			ae.log.Warn("failed to persist tool call",
				slog.String("run_id", run.ID),
				slog.String("tool", toolName),
				slog.String("error", persistErr.Error()),
			)
		}

		// If ask_user was just called, pause the run immediately so the LLM
		// cannot produce a final response that would mark the run as success
		// before beforeModelCb fires (race condition fix).
		if toolName == ToolNameAskUser && askPauseState != nil && askPauseState.ShouldPause() {
			ae.log.Info("ask_user afterToolCb: pausing run immediately",
				slog.String("run_id", run.ID),
				slog.String("question_id", askPauseState.QuestionID()),
			)
			_ = ae.repo.PauseRun(dbCtx, run.ID, currentStep)
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
			cancelReason = fmt.Sprintf("agent stopped: doom loop detected — %d consecutive identical calls to %q", doomDetector.consecutiveCount, toolName)
			cancelRun()
			return nil, fmt.Errorf("DOOM_LOOP_DETECTED: %d consecutive identical calls to %q — agent is stuck in a loop and has been stopped", doomDetector.consecutiveCount, toolName)
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

	// Log final resolved tool names for diagnostics
	if len(resolvedTools) > 0 {
		toolNames := make([]string, len(resolvedTools))
		for i, t := range resolvedTools {
			toolNames[i] = t.Name()
		}
		ae.log.Info("final resolved tools for agent",
			slog.String("run_id", run.ID),
			slog.Int("count", len(resolvedTools)),
			slog.Any("tools", toolNames),
		)
	}

	// After-model callback: accumulate cached token counts for summary visibility.
	afterModelCb := func(cbCtx agent.CallbackContext, llmResp *model.LLMResponse, llmErr error) (*model.LLMResponse, error) {
		if llmResp != nil && llmResp.UsageMetadata != nil && llmResp.UsageMetadata.CachedContentTokenCount > 0 {
			cachedTokensMu.Lock()
			totalCachedTokens += int64(llmResp.UsageMetadata.CachedContentTokenCount)
			cachedTokensMu.Unlock()
		}
		return nil, nil
	}

	llmAgent, err := llmagent.New(llmagent.Config{
		Name:                  sanitizeAgentName(agentName),
		Description:           ae.resolveDescription(req),
		Instruction:           instruction,
		Model:                 llm,
		Tools:                 resolvedTools,
		GenerateContentConfig: genConfig,
		BeforeModelCallbacks:  []llmagent.BeforeModelCallback{beforeModelCb},
		AfterModelCallbacks:   []llmagent.AfterModelCallback{afterModelCb},
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
			ae.log.Warn("failed to load existing ADK session, deleting stale session before creating fresh one",
				slog.String("session_id", sessionID),
				slog.String("error", err.Error()),
			)
			// Delete the stale session so the Create below can succeed.
			_ = sessionService.Delete(ctx, &session.DeleteRequest{
				AppName:   "agents",
				UserID:    "system",
				SessionID: sessionID,
			})
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

	// Build the user message. Prepend TriggerMetadata context if present so the
	// system instruction stays static (enabling Gemini implicit prompt caching).
	userContent := genai.NewContentFromText(triggerContextPrefix+req.UserMessage, genai.RoleUser)

	// Persist the user message
	ae.persistMessage(dbCtx, run.ID, "user", req.UserMessage, initialSteps)

	// Run the agent with MALFORMED_FUNCTION_CALL retry logic.
	// When the LLM emits a malformed function call we inject a recovery turn
	// ("Your previous response was malformed — please try again") and restart
	// the iterator on the same ADK session, up to maxMalformedRetries times.
	// Similarly, when the LLM calls an unknown/hallucinated tool name we inject
	// a correction turn listing the available tools.
	const maxMalformedRetries = 3
	const maxTransientRetries = 5
	var lastEvent *session.Event
	var lastTextEvent *session.Event // fallback: last non-empty text event
	runCfg := agent.RunConfig{}
	if req.StreamCallback != nil {
		runCfg.StreamingMode = agent.StreamingModeSSE
	}
	malformedCount := 0
	unknownToolCount := 0
	transientErrCount := 0
	currentContent := userContent
	for {
		malformed := false
		unknownTool := false
		transientErr := false
		for event, eventErr := range r.Run(ctx, "system", sess.ID(), currentContent, runCfg) {
			if eventErr != nil {
				steps := tracker.current()
				errStr := eventErr.Error()

				// RESOURCE_EXHAUSTED / spending cap: treat as permanent after 1 retry.
				// On first occurrence, log and retry once (5s sleep). On second, disable
				// the agent to prevent infinite billing loops.
				isSpendingCapErr := strings.Contains(errStr, "RESOURCE_EXHAUSTED") ||
					strings.Contains(errStr, "spending cap")
				if isSpendingCapErr {
					agentID := ae.resolveAgentID(req)
					if transientErrCount == 0 {
						transientErrCount++
						ae.log.Warn("agent run received RESOURCE_EXHAUSTED, retrying once then disabling",
							slog.String("run_id", run.ID),
							slog.String("agent_id", agentID),
							slog.String("error", errStr),
						)
						transientErr = true
						time.Sleep(5 * time.Second)
						break
					}
					// Second occurrence: disable the agent permanently.
					ae.log.Error("agent run received RESOURCE_EXHAUSTED on retry, disabling agent",
						slog.String("run_id", run.ID),
						slog.String("agent_id", agentID),
						slog.String("error", errStr),
					)
					if agentID != "" {
						if disableErr := ae.repo.DisableAgent(ctx, agentID, "spending cap exceeded"); disableErr != nil {
							ae.log.Error("failed to disable agent after RESOURCE_EXHAUSTED",
								slog.String("agent_id", agentID),
								slog.String("error", disableErr.Error()),
							)
						}
					}
					_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, "spending cap exceeded, agent disabled", steps)
					return &ExecuteResult{
						RunID:  run.ID,
						Status: RunStatusError,
					}, fmt.Errorf("spending cap exceeded, agent disabled")
				}

				// Retry on transient Google AI API errors (503 UNAVAILABLE, 429 rate-limit)
				// without injecting a new message — simply re-run from current content.
				isTransient := strings.Contains(errStr, "503") ||
					strings.Contains(errStr, "UNAVAILABLE") ||
					strings.Contains(errStr, "429")
				if isTransient && transientErrCount < maxTransientRetries {
					transientErrCount++
					// Fixed 5s delay — rate limit errors need a brief pause, not
					// an escalating backoff that burns through test timeouts.
					const transientDelay = 5 * time.Second
					ae.log.Warn("agent run received transient API error, retrying",
						slog.String("run_id", run.ID),
						slog.String("error", errStr),
						slog.Int("attempt", transientErrCount),
						slog.Int("max_retries", maxTransientRetries),
						slog.Duration("backoff", transientDelay),
					)
					transientErr = true
					time.Sleep(transientDelay)
					break // break inner loop to restart r.Run() with same content
				}
				// Retry when the LLM calls a hallucinated/unknown tool name.
				// Extract the bad name, suggest the closest real tool, and inject
				// a helpful correction message.
				if strings.Contains(errStr, "unknown tool:") && unknownToolCount < maxMalformedRetries {
					unknownToolCount++
					var toolNames []string
					for _, t := range resolvedTools {
						toolNames = append(toolNames, t.Name())
					}
					// Extract the called name from the error string (format: `unknown tool: "foo"`)
					calledName := errStr
					if idx := strings.Index(errStr, "unknown tool:"); idx >= 0 {
						calledName = strings.TrimSpace(errStr[idx+len("unknown tool:"):])
						calledName = strings.Trim(calledName, `"`)
					}
					// Find the closest real tool name by edit distance.
					suggestion := closestToolName(calledName, toolNames)
					var suggestionMsg string
					if suggestion != "" {
						suggestionMsg = fmt.Sprintf(
							" Did you mean %q? That tool can help you accomplish the same goal.",
							suggestion,
						)
					}
					correction := fmt.Sprintf(
						"You called a tool named %q which does not exist.%s "+
							"The tools available to you are: %s. "+
							"Please try again using only those tools.",
						calledName,
						suggestionMsg,
						strings.Join(toolNames, ", "),
					)
					ae.log.Warn("agent called unknown tool, injecting correction",
						slog.String("run_id", run.ID),
						slog.String("called_tool", calledName),
						slog.String("suggestion", suggestion),
						slog.Int("attempt", unknownToolCount),
						slog.Int("max_retries", maxMalformedRetries),
					)
					currentContent = genai.NewContentFromText(correction, genai.RoleUser)
					unknownTool = true
					time.Sleep(time.Duration(unknownToolCount) * 2 * time.Second)
					break
				}
				if req.StreamCallback != nil {
					req.StreamCallback(StreamEvent{
						Type:  StreamEventError,
						Error: errStr,
					})
				}
				_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, errStr, steps)
				return &ExecuteResult{
					RunID:    run.ID,
					Status:   RunStatusError,
					Summary:  map[string]any{"error": errStr},
					Steps:    steps,
					Duration: time.Since(startTime),
				}, nil
			}

			if event == nil {
				continue
			}

			// Treat non-empty ErrorCode that indicates a genuine LLM malfunction as
			// a fatal run error, so the result is properly marked RunStatusError and
			// the parent is re-enqueued with status:error rather than status:success.
			//
			// "STOP" and "FINISH_REASON_UNSPECIFIED" are normal terminations: the
			// model finished cleanly but produced no content (e.g. it called tools
			// and then stopped). All other codes represent abnormal terminations —
			// safety blocks, malformed function calls, recitation filters, etc.
			//
			// MALFORMED_FUNCTION_CALL is special: we retry by injecting a recovery
			// turn, up to maxMalformedRetries times.
			if event.ErrorCode != "" && event.ErrorCode != "STOP" && event.ErrorCode != "FINISH_REASON_UNSPECIFIED" {
				steps := tracker.current()
				errMsg := fmt.Sprintf("LLM returned error: %s", event.ErrorCode)
				if event.ErrorMessage != "" {
					errMsg = fmt.Sprintf("LLM returned error: %s: %s", event.ErrorCode, event.ErrorMessage)
				}
				if event.ErrorCode == "MALFORMED_FUNCTION_CALL" && malformedCount < maxMalformedRetries {
					malformedCount++
					ae.log.Warn("agent run received MALFORMED_FUNCTION_CALL, retrying",
						slog.String("run_id", run.ID),
						slog.Int("attempt", malformedCount),
						slog.Int("max_retries", maxMalformedRetries),
						slog.Int("steps", steps),
					)
					// Inject a recovery turn so the LLM can correct its call.
					currentContent = genai.NewContentFromText(
						"Your previous response contained a malformed function call. "+
							"Please review what you were trying to do and try again with "+
							"properly formatted arguments.",
						genai.RoleUser,
					)
					malformed = true
					break // break inner loop to restart r.Run() with recovery message
				}
				ae.log.Warn("agent run aborted due to LLM error code",
					slog.String("run_id", run.ID),
					slog.String("error_code", event.ErrorCode),
					slog.String("error_message", event.ErrorMessage),
					slog.Int("steps", steps),
				)
				if req.StreamCallback != nil {
					req.StreamCallback(StreamEvent{
						Type:  StreamEventError,
						Error: errMsg,
					})
				}
				_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, errMsg, steps)
				return &ExecuteResult{
					RunID:    run.ID,
					Status:   RunStatusError,
					Summary:  map[string]any{"error": errMsg, "error_code": event.ErrorCode},
					Steps:    steps,
					Duration: time.Since(startTime),
				}, nil
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
				ae.persistEventContent(dbCtx, run.ID, event, tracker.current())
			}

			if event.IsFinalResponse() {
				lastEvent = event
			}

			// Track the last non-partial event that carries text — used as fallback
			// when IsFinalResponse() never fires (e.g. agent ends with a tool call).
			if !event.Partial && event.Content != nil {
				stepHasText := false
				for _, part := range event.Content.Parts {
					if part != nil && part.Text != "" {
						lastTextEvent = event
						stepHasText = true
						break
					}
				}
				// Check for consecutive tool-only steps (issue #146).
				// recordStep resets its counter when text is produced; if the step
				// contained only tool calls it increments and may abort the run.
				toolOnlyAction := doomDetector.recordStep(stepHasText)
				switch toolOnlyAction {
				case doomActionWarn:
					ae.log.Warn("tool-only loop warning: consecutive steps with no assistant text",
						slog.String("run_id", run.ID),
						slog.Int("consecutive_tool_only_steps", doomDetector.toolOnlySteps),
					)
				case doomActionStop:
					ae.log.Error("tool-only loop detected, stopping agent",
						slog.String("run_id", run.ID),
						slog.Int("consecutive_tool_only_steps", doomDetector.toolOnlySteps),
					)
					cancelReason = fmt.Sprintf("agent stopped: tool-only loop detected — %d consecutive steps with no assistant text", doomDetector.toolOnlySteps)
					cancelRun()
				}
			}
		} // end inner for-range r.Run(...)
		if !malformed && !transientErr && !unknownTool {
			break // normal completion — exit outer retry loop
		}
		// MALFORMED_FUNCTION_CALL retry: brief pause then re-enter inner loop
		// (transient errors already slept their backoff above before breaking;
		// unknown tool errors already slept before breaking too)
		if malformed {
			time.Sleep(time.Duration(malformedCount) * 2 * time.Second)
		}
	} // end outer retry loop

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

		_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, errMsg, steps)
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
	currentRun, _ := ae.repo.FindRunByID(dbCtx, run.ID)
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
	summary := ae.buildSummary(lastEvent, lastTextEvent, steps)

	// Include cache hit visibility — non-zero means Gemini implicit cache fired.
	if totalCachedTokens > 0 {
		summary["cached_tokens"] = totalCachedTokens
	}

	// Include workspace info in summary if provisioned
	if wsResult != nil && wsResult.Workspace != nil {
		summary["workspace_id"] = wsResult.Workspace.ID
		if wsResult.Degraded {
			summary["workspace_degraded"] = true
		}
	}

	// If workspace was required but ended up degraded, mark as error.
	// The agent completed but could not actually use its sandbox — this is not success.
	if wsResult != nil && wsResult.Degraded {
		summary["error"] = "workspace provisioning failed: " + wsResult.Error.Error()
		_ = ae.repo.FailRunWithSteps(dbCtx, run.ID, summary["error"].(string), steps)

		if req.Agent != nil && req.Agent.ID != "" {
			_ = ae.repo.UpdateLastRun(dbCtx, req.Agent.ID, string(RunStatusError))
		}

		ae.log.Warn("agent run completed with degraded workspace, marking as error",
			slog.String("run_id", run.ID),
			slog.Int("steps", steps),
		)

		return &ExecuteResult{
			RunID:    run.ID,
			Status:   RunStatusError,
			Summary:  summary,
			Steps:    steps,
			Duration: duration,
		}, nil
	}

	// Mark run as complete
	if err := ae.repo.CompleteRunWithSteps(dbCtx, run.ID, summary, steps, durationMs); err != nil {
		ae.log.Warn("failed to complete run record",
			slog.String("run_id", run.ID),
			slog.String("error", err.Error()),
		)
	}

	// Update the agent's last run status
	if req.Agent != nil && req.Agent.ID != "" {
		_ = ae.repo.UpdateLastRun(dbCtx, req.Agent.ID, string(RunStatusSuccess))
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
func (ae *AgentExecutor) augmentInstructionWithWorkspace(instruction string, wsResult *sandbox.ProvisioningResult) string {
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

	wsContext += `
Workspace tools are prefixed with workspace_ and run inside the sandboxed container.

### Tool preference rules
- Reading files:    workspace_read   (NOT workspace_bash with cat/head/tail)
- Writing files:    workspace_write  (NOT workspace_bash with echo > or cat <<EOF)
- Editing files:    workspace_edit   (NOT workspace_bash with sed/awk)
- Finding files:    workspace_glob   (NOT workspace_bash with find or ls)
- Searching code:   workspace_grep   (NOT workspace_bash with grep)
- Terminal ops:     workspace_bash   (builds, tests, package managers, git via workspace_git)
- Git operations:   workspace_git    (structured git: status, diff, commit, checkout, clone)

### workspace_bash guidance
- Use the workdir parameter instead of "cd <dir> && <command>".
- Chain dependent commands with &&; independent commands can be parallel tool calls.
- Default timeout is 120 seconds. Increase timeout_ms for long builds or test suites.
- Avoid echo/printf for file creation — use workspace_write instead.
`

	return instruction + wsContext
}

// contextInjectionMode returns the context_injection setting from the agent
// definition's Config map. Valid values: "full" (default) and "never".
// When set to "never", automatic workspace/bootstrap context is not appended
// to the system instruction — the agent fully owns its prompt lifecycle.
func contextInjectionMode(def *AgentDefinition) string {
	if def == nil {
		return "full"
	}
	if v, ok := def.Config["contextInjection"].(string); ok && v != "" {
		return v
	}
	return "full"
}

// resolveWorkspaceTools builds ADK tools that let the agent interact with its
// provisioned workspace container (bash, read, write, edit, glob, grep, git).
// Returns nil if the provisioner can't provide a provider for the workspace.
func (ae *AgentExecutor) resolveWorkspaceTools(wsResult *sandbox.ProvisioningResult, req ExecuteRequest) ([]tool.Tool, error) {
	if ae.provisioner == nil || wsResult == nil || wsResult.Workspace == nil {
		return nil, nil
	}

	// Get the provider for this workspace
	provider, err := ae.provisioner.GetProviderForWorkspace(wsResult.Workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for workspace %s: %w", wsResult.Workspace.ID, err)
	}

	// Parse workspace config for tool filtering
	var wsCfg *sandbox.AgentSandboxConfig
	if req.AgentDefinition != nil && len(req.AgentDefinition.SandboxConfig) > 0 {
		wsCfg, _ = sandbox.ParseAgentSandboxConfig(req.AgentDefinition.SandboxConfig)
	}

	// Build per-session env vars so that tools executing inside the container
	// (run_python, bash) have credentials even when a warm-pool container was
	// used (warm containers are pre-booted without session-specific env vars).
	//
	// Merge priority (later wins):
	//   1. Static env vars from agent definition (cfg.EnvVars)
	//   2. Runtime env vars from trigger request (req.EnvVars)
	//   3. System keys (MEMORY_API_KEY, etc.) — always win
	sessionEnv := map[string]string{}

	// 1. Static env vars from agent sandbox config
	if wsCfg != nil {
		for k, v := range wsCfg.EnvVars {
			sessionEnv[k] = v
		}
	}

	// 2. Runtime env vars from trigger request
	for k, v := range req.EnvVars {
		sessionEnv[k] = v
	}

	// 3. System keys — always win
	if req.AuthToken != "" {
		sessionEnv["MEMORY_API_KEY"] = req.AuthToken
		sessionEnv["MEMORY_PROJECT_ID"] = req.ProjectID
		sessionEnv["MEMORY_SERVER_URL"] = "http://host.docker.internal:3002"
	}

	return BuildWorkspaceTools(WorkspaceToolDeps{
		Provider:        provider,
		ProviderID:      wsResult.Workspace.ProviderWorkspaceID,
		WorkspaceID:     wsResult.Workspace.ID,
		Config:          wsCfg,
		Logger:          ae.log,
		CheckoutService: ae.provisioner.CheckoutService(),
		SessionEnv:      sessionEnv,
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

	// Agents with an explicit non-empty tools whitelist only get coordination tools
	// if they explicitly opted in (i.e. whitelist contains spawn_agents or list_available_agents).
	// This applies at all depths — top-level agents are not exempt.
	// Agents with no definition (legacy) or empty whitelist fall through to the default behaviour:
	//   - no definition → all tools (legacy), coordination tools included
	//   - empty whitelist → no tools at all (filterToolDefs returns nil), but coordination
	//     tools were still being injected here — now blocked too.
	if req.AgentDefinition != nil && len(req.AgentDefinition.Tools) > 0 {
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
		Executor:       ae,
		Repo:           ae.repo,
		Logger:         ae.log,
		ProjectID:      req.ProjectID,
		ParentRunID:    runID,
		RootRunID:      derefString(req.RootRunID),
		Depth:          req.Depth,
		MaxDepth:       maxDepth,
		SpawnPolicy:    extractSpawnPolicy(req.AgentDefinition),
		ParentMetadata: req.TriggerMetadata,
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
		UserID:     req.UserID,
		EventsSvc:  ae.eventsSvc,
	}

	return BuildAskUserTool(deps)
}

// buildSkillTool constructs the skill tool for an agent run if "skill" is
// listed in the agent definition's Tools whitelist. Returns (nil, nil) when
// skill tool is not opted in. Non-fatal: callers should log + continue on error.
func (ae *AgentExecutor) buildSkillTool(ctx context.Context, run *AgentRun, req ExecuteRequest) (tool.Tool, error) {
	if req.AgentDefinition == nil {
		return nil, nil
	}

	// Opt-in check: inject skill tool when skills field is non-empty, autoLoadSkills is set, OR legacy "skill" in tools list.
	hasSkillsField := len(req.AgentDefinition.Skills) > 0 || req.AgentDefinition.AutoLoadSkills
	hasSkillInTools := false
	for _, t := range req.AgentDefinition.Tools {
		if t == "skill" {
			hasSkillInTools = true
			break
		}
	}
	if !hasSkillsField && !hasSkillInTools {
		return nil, nil
	}

	// Extract trigger message for semantic retrieval query
	triggerMsg := ""
	if run.TriggerMessage != nil {
		triggerMsg = *run.TriggerMessage
	}

	agentName := ""
	agentDesc := ""
	if req.AgentDefinition != nil {
		agentName = req.AgentDefinition.Name
		if req.AgentDefinition.Description != nil {
			agentDesc = *req.AgentDefinition.Description
		}
	}

	// Use the skills field when present; fall back to wildcard for legacy "skill" in tools.
	skillNames := req.AgentDefinition.Skills
	if len(skillNames) == 0 {
		skillNames = []string{"*"}
	}

	deps := skills.SkillToolDeps{
		Repo:             ae.skillRepo,
		EmbeddingsSvc:    ae.embeddingsSvc,
		Logger:           ae.log,
		ProjectID:        req.ProjectID,
		OrgID:            req.OrgID,
		TriggerMessage:   triggerMsg,
		AgentName:        agentName,
		AgentDescription: agentDesc,
		Skills:           skillNames,
	}

	return skills.BuildSkillTool(ctx, deps)
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
// If the agent definition declares skills, an <available_skills> index is
// appended so the agent sees all bound skills before it starts reasoning
// (zero-turn discovery) rather than only through the skill tool description.
func (ae *AgentExecutor) resolveInstruction(req ExecuteRequest) string {
	inst := ""
	if req.AgentDefinition != nil && req.AgentDefinition.SystemPrompt != nil {
		inst = *req.AgentDefinition.SystemPrompt
	} else if req.Agent != nil && req.Agent.Prompt != nil {
		inst = *req.Agent.Prompt
	} else {
		inst = "You are a helpful assistant."
	}

	if appendix := ae.buildSkillsSystemPrompt(req); appendix != "" {
		inst += "\n\n" + appendix
	}

	return inst
}

// buildSkillsSystemPrompt builds the <available_skills> block that is appended
// to the system instruction when the agent definition declares bound skills.
// Returns an empty string when there are no skills to inject.
func (ae *AgentExecutor) buildSkillsSystemPrompt(req ExecuteRequest) string {
	if req.AgentDefinition == nil {
		return ""
	}
	hasExplicit := len(req.AgentDefinition.Skills) > 0
	hasAutoLoad := req.AgentDefinition.AutoLoadSkills
	if !hasExplicit && !hasAutoLoad {
		return ""
	}

	ctx := context.Background()
	all, err := ae.skillRepo.FindForAgent(ctx, req.ProjectID, req.OrgID)
	if err != nil || len(all) == 0 {
		return ""
	}

	// Build the set of matched skills.
	// Explicit "*" means all; otherwise merge explicit names + auto-prefix matches.
	declared := req.AgentDefinition.Skills
	isWildcard := len(declared) == 1 && declared[0] == "*"
	var matched []*skills.Skill
	if isWildcard {
		matched = all
	} else {
		seen := make(map[string]struct{})
		// Explicit names first (they take precedence for deduplication).
		nameSet := make(map[string]struct{}, len(declared))
		for _, n := range declared {
			nameSet[n] = struct{}{}
		}
		for _, s := range all {
			if _, ok := nameSet[s.Name]; ok {
				matched = append(matched, s)
				seen[s.Name] = struct{}{}
			}
		}
		// Auto-load: prefix match — any skill named "{agentName}.{anything}".
		if hasAutoLoad && req.AgentDefinition.Name != "" {
			prefix := req.AgentDefinition.Name + "."
			for _, s := range all {
				if _, already := seen[s.Name]; !already && strings.HasPrefix(s.Name, prefix) {
					matched = append(matched, s)
					seen[s.Name] = struct{}{}
				}
			}
		}
	}
	if len(matched) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Skills (mandatory)\n")
	b.WriteString("Before replying, scan the skills below. If a skill matches or is even partially ")
	b.WriteString("relevant to your task, you MUST load it with the skill tool and follow its ")
	b.WriteString("instructions. Err on the side of loading.\n\n")
	b.WriteString("<available_skills>\n")
	for _, s := range matched {
		fmt.Fprintf(&b, "  - %s: %s\n", s.Name, s.Description)
	}
	b.WriteString("</available_skills>\n\n")
	b.WriteString("Only proceed without loading a skill if genuinely none are relevant to the task.")

	return b.String()
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
// If lastEvent has no text content (e.g. agent ended with a tool call),
// falls back to lastTextEvent — the last non-partial event that carried text.
func (ae *AgentExecutor) buildSummary(lastEvent *session.Event, lastTextEvent *session.Event, steps int) map[string]any {
	summary := map[string]any{
		"steps": steps,
	}

	// Helper to extract the last text part from an event.
	extractText := func(ev *session.Event) string {
		if ev == nil || ev.Content == nil {
			return ""
		}
		var last string
		for _, part := range ev.Content.Parts {
			if part != nil && part.Text != "" {
				last = part.Text
			}
		}
		return last
	}

	// Prefer the canonical final-response event; fall back to last text event.
	text := extractText(lastEvent)
	if text == "" {
		text = extractText(lastTextEvent)
	}
	if text != "" {
		summary["final_response"] = text
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
	doomWarnThreshold = 5
	// doomStopThreshold is the number of consecutive identical calls before stopping.
	doomStopThreshold = 10

	// toolOnlyWarnThreshold is the number of consecutive tool-only steps (no
	// assistant text produced) before issuing a warning.
	toolOnlyWarnThreshold = 10
	// toolOnlyStopThreshold is the number of consecutive tool-only steps before
	// aborting the run. This catches loops where different tools cycle without
	// ever producing meaningful output — the failure mode from issue #146.
	// Raised from 10→20 to accommodate legitimate multi-file write sequences
	// (issue #172); the identical-call doom detector handles true stuck loops.
	toolOnlyStopThreshold = 20
)

// doomLoopDetector tracks two loop patterns:
//  1. Consecutive identical tool calls (same tool + same args).
//  2. Consecutive tool-only steps where the LLM produces no assistant text.
type doomLoopDetector struct {
	log              *slog.Logger
	lastToolName     string
	lastArgsHash     string
	consecutiveCount int
	// toolOnlySteps counts LLM steps that produced only tool calls, no text.
	toolOnlySteps int
}

func newDoomLoopDetector(log *slog.Logger) *doomLoopDetector {
	return &doomLoopDetector{log: log}
}

// recordStep is called once per completed LLM step with whether the step
// produced any assistant text. It resets the tool-only counter on text output
// and returns an action recommendation when the counter exceeds thresholds.
func (d *doomLoopDetector) recordStep(hasText bool) doomAction {
	if hasText {
		d.toolOnlySteps = 0
		return doomActionNone
	}
	d.toolOnlySteps++
	if d.toolOnlySteps >= toolOnlyStopThreshold {
		return doomActionStop
	}
	if d.toolOnlySteps >= toolOnlyWarnThreshold {
		return doomActionWarn
	}
	return doomActionNone
}

// closestToolName returns the tool name from candidates that is most similar
// to called (by Levenshtein edit distance). Returns "" if candidates is empty.
func closestToolName(called string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	bestDist := levenshtein(called, best)
	for _, c := range candidates[1:] {
		if d := levenshtein(called, c); d < bestDist {
			bestDist = d
			best = c
		}
	}
	// Only suggest if reasonably close — threshold: half the longer string's length.
	maxLen := len(called)
	if len(best) > maxLen {
		maxLen = len(best)
	}
	if maxLen == 0 || bestDist > maxLen/2 {
		return ""
	}
	return best
}

// derefString safely dereferences a *string pointer.
// Returns the pointed-to value or "" if the pointer is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
		// A different tool call (different name or args) is evidence of real
		// progress — reset the tool-only step counter so legitimate multi-file
		// write sequences (workspace_write with different paths) are not killed
		// by the tool-only loop detector (issue #172).
		d.toolOnlySteps = 0
	}

	if d.consecutiveCount >= doomStopThreshold {
		return doomActionStop
	}
	if d.consecutiveCount >= doomWarnThreshold {
		return doomActionWarn
	}
	return doomActionNone
}
