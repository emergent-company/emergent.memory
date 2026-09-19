package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// A2A message-flow handlers (POST /message:send, GET /tasks/{id}, GET /tasks,
// POST /tasks/{id}:cancel, POST /tasks/{id}:subscribe).
//
// These are a stateless facade over the existing run engine: a Task maps to an
// AgentRun, a Task.contextId maps to kb.acp_sessions, and HITL resume reuses the
// existing question-claim pattern (repo.AnswerQuestion) + executor.Resume.

// ---------------------------------------------------------------------------
// Local error helpers (validation / invalid-state envelope). The A2A error
// envelope already covers protocol errors; these fill the "client supplied
// something invalid" gap without touching the committed a2a_errors.go.
// ---------------------------------------------------------------------------

const (
	a2aReasonInvalidArgument A2AErrorReason = "INVALID_ARGUMENT"
	a2aReasonInvalidState    A2AErrorReason = "INVALID_STATE"
	a2aReasonSkillNotFound   A2AErrorReason = "SKILL_NOT_FOUND"
)

// a2aSkillIDMetadataKey is the canonical message.metadata key under which a
// first-party client encodes the target AgentSkill id. Mirrors the SDK's
// pkg/sdk/a2a.SkillIDMetadataKey (kept local here: the SDK is a separate module
// and the agents package must not import it).
const a2aSkillIDMetadataKey = "skillId"

// errA2ASkillNotFound is returned by resolveA2AAgent when a message.metadata
// skill id matches no agent definition in the project. Distinct from a generic
// internal error so startA2ATask can map it to a 400 SKILL_NOT_FOUND envelope.
var errA2ASkillNotFound = errors.New("skill not found")

// a2aValidationError builds a 400 INVALID_ARGUMENT envelope for malformed
// requests (empty message, unknown context, context/task mismatch).
func a2aValidationError(message string) *A2AError {
	return &A2AError{Code: A2AErrorCode(http.StatusBadRequest), Reason: a2aReasonInvalidArgument, Message: message}
}

// a2aInvalidStateError builds a 400 INVALID_STATE envelope for resume-guard
// rejections (task not awaiting input, lost a concurrent question claim).
func a2aInvalidStateError(message string) *A2AError {
	return &A2AError{Code: A2AErrorCode(http.StatusBadRequest), Reason: a2aReasonInvalidState, Message: message}
}

// a2aSkillNotFoundError builds a 400 SKILL_NOT_FOUND envelope for an unknown
// message.metadata skill id.
func a2aSkillNotFoundError(skillID string) *A2AError {
	return &A2AError{Code: A2AErrorCode(http.StatusBadRequest), Reason: a2aReasonSkillNotFound, Message: "unknown skill: " + skillID}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// a2aUserMessageFromParts concatenates the text members of a message's parts.
// A2A Part is a member-presence union; only the `text` member contributes.
func a2aUserMessageFromParts(parts []Part) string {
	var text string
	for _, p := range parts {
		if p.Text != nil && *p.Text != "" {
			text += *p.Text
		}
	}
	return text
}

// a2aSkillIDFromMetadata extracts the target skill id from message metadata.
// The canonical key is "skillId"; "skill_id" is tolerated. Empty/nil returns "".
func a2aSkillIDFromMetadata(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	if v, ok := metadata[a2aSkillIDMetadataKey].(string); ok && v != "" {
		return v
	}
	if v, ok := metadata["skill_id"].(string); ok && v != "" {
		return v
	}
	return ""
}

// isTerminalRunStatus reports whether an internal run status is terminal.
func isTerminalRunStatus(s AgentRunStatus) bool {
	switch s {
	case RunStatusSuccess, RunStatusSkipped, RunStatusError, RunStatusCancelled:
		return true
	default:
		return false
	}
}

// hitlMetadata builds task metadata distinguishing the pause source for a
// paused run so A2A callers can tell an ask_user question from a tool-approval
// gate. Returns nil for non-paused runs.
func hitlMetadata(run *AgentRun) map[string]any {
	if run == nil || run.Status != RunStatusPaused {
		return nil
	}
	source := "unknown"
	if sc := SuspendSignalFromMap(run.SuspendContext); sc != nil {
		switch sc.Reason {
		case SuspendReasonAwaitingHuman:
			source = "ask_user"
		case SuspendReasonAwaitingToolConfirm:
			source = "tool_approval"
		default:
			source = string(sc.Reason)
		}
	}
	return map[string]any{
		"pauseSource":   source,
		"inputRequired": true,
	}
}

// internalStatusesForTaskState maps an A2A TaskState filter to the set of
// internal statuses it represents. Returns nil when the state is unknown.
func internalStatusesForTaskState(state TaskState) []AgentRunStatus {
	switch state {
	case TaskStateSubmitted:
		return []AgentRunStatus{RunStatusQueued}
	case TaskStateWorking:
		return []AgentRunStatus{RunStatusRunning, RunStatusCancelling}
	case TaskStateCompleted:
		return []AgentRunStatus{RunStatusSuccess, RunStatusSkipped}
	case TaskStateFailed:
		return []AgentRunStatus{RunStatusError}
	case TaskStateInputRequired:
		return []AgentRunStatus{RunStatusPaused}
	case TaskStateCanceled:
		return []AgentRunStatus{RunStatusCancelled}
	default:
		return nil
	}
}

// a2aPageSize normalises a caller-supplied page size to the [1,100] range with
// a default of 50.
func a2aPageSize(n int) int {
	if n <= 0 {
		return 50
	}
	if n > 100 {
		return 100
	}
	return n
}

// a2aEncodePageToken encodes an offset into an opaque page token.
func a2aEncodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

// a2aDecodePageToken decodes a page token back into an offset. Invalid or empty
// tokens resolve to offset 0.
func a2aDecodePageToken(token string) int {
	if token == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// a2aQueryInt parses an integer query parameter with a default fallback.
func a2aQueryInt(c echo.Context, key string, def int) int {
	s := c.QueryParam(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// a2aEventTypeToTaskState maps an ACP lifecycle event type to an A2A TaskState.
func a2aEventTypeToTaskState(eventType string) (TaskState, bool) {
	switch eventType {
	case ACPEventRunCreated:
		return TaskStateSubmitted, true
	case ACPEventRunInProgress:
		return TaskStateWorking, true
	case ACPEventRunAwaiting:
		return TaskStateInputRequired, true
	case ACPEventRunCompleted:
		return TaskStateCompleted, true
	case ACPEventRunFailed:
		return TaskStateFailed, true
	case ACPEventRunCancelled:
		return TaskStateCanceled, true
	default:
		return TaskStateUnspecified, false
	}
}

// a2aStreamResponseFromEventType builds a statusUpdate StreamResponse for a
// lifecycle event type, or false when the event has no A2A mapping.
func a2aStreamResponseFromEventType(eventType, taskID, contextID string) (StreamResponse, bool) {
	state, ok := a2aEventTypeToTaskState(eventType)
	if !ok {
		return StreamResponse{}, false
	}
	return StreamResponse{
		StatusUpdate: &TaskStatusUpdateEvent{
			TaskID:    taskID,
			ContextID: contextID,
			Status:    TaskStatus{State: state},
		},
	}, true
}

// buildA2ATask assembles an A2A Task from the latest run in a resume chain,
// forcing Task.id to the caller-supplied (stable) task ID rather than the
// internal run/resume-run ID. historyLength > 0 truncates history to the last
// N messages.
func buildA2ATask(latest *AgentRun, messages []AgentRunMessage, question *AgentQuestion, taskID string, historyLength int) Task {
	taskRun := *latest
	taskRun.ID = taskID
	task := RunToA2ATask(&taskRun, messages, question, nil)
	if historyLength > 0 && len(task.History) > historyLength {
		task.History = task.History[len(task.History)-historyLength:]
	}
	if md := hitlMetadata(latest); md != nil {
		task.Metadata = md
	}
	return task
}

// ---------------------------------------------------------------------------
// Handler helpers
// ---------------------------------------------------------------------------

// a2aOrgID resolves the org ID for the project, mirroring ACP's acpOrgID.
func (h *A2AHandler) a2aOrgID(ctx context.Context, c echo.Context, projectID string) string {
	user := auth.GetUser(c)
	if user != nil && user.OrgID != "" {
		return user.OrgID
	}
	orgID, _ := h.repo.GetOrgIDByProjectID(ctx, projectID)
	return orgID
}

// resolveA2AAgent resolves the runtime agent + definition for a new A2A task.
//
// Routing contract: the A2A SendMessageRequest has no skill selector, so a
// first-party client targets a specific skill by encoding the AgentSkill id in
// message.metadata["skillId"] (the SDK's pkg/sdk/a2a.SkillIDMetadataKey). When
// that key is present the definition is resolved by slug — external-visibility
// first, then any-visibility (mirroring ACPHandler.resolveAgentDefinitionBySlug)
// — and an unknown skill id is a hard 400 (SKILL_NOT_FOUND), never a fallback.
// When the key is absent, the project's general-purpose CLI assistant is used,
// mirroring the platform /api/ask behaviour.
func (h *A2AHandler) resolveA2AAgent(ctx context.Context, projectID, skillID string) (*AgentDefinition, *Agent, error) {
	var def *AgentDefinition
	var err error

	if skillID != "" {
		def, err = h.resolveA2AAgentBySkillID(ctx, projectID, skillID)
		if err != nil {
			return nil, nil, err
		}
		if def == nil {
			return nil, nil, errA2ASkillNotFound
		}
	} else {
		def, err = h.repo.EnsureCliAssistantAgent(ctx, projectID, "")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to ensure platform assistant: %w", err)
		}
	}

	agent, err := h.resolveRuntimeAgent(ctx, projectID, def)
	if err != nil {
		return nil, nil, err
	}
	return def, agent, nil
}

// resolveA2AAgentBySkillID resolves an agent definition by skill id (slug),
// preferring external visibility and falling back to any visibility. Returns
// (nil, nil) when no definition matches; a non-nil error only on DB failure.
func (h *A2AHandler) resolveA2AAgentBySkillID(ctx context.Context, projectID, skillID string) (*AgentDefinition, error) {
	def, err := h.repo.FindExternalAgentBySlug(ctx, projectID, skillID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up skill: %w", err)
	}
	if def == nil {
		def, err = h.repo.FindAgentDefinitionBySlug(ctx, projectID, skillID)
		if err != nil {
			return nil, fmt.Errorf("failed to look up skill: %w", err)
		}
	}
	return def, nil
}

// resolveRuntimeAgent finds the runtime Agent for a definition by name, creating
// a lightweight runtime agent (StrategyType "agent-def:<id>") when absent.
func (h *A2AHandler) resolveRuntimeAgent(ctx context.Context, projectID string, def *AgentDefinition) (*Agent, error) {
	agent, err := h.repo.FindByName(ctx, projectID, def.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to look up runtime agent: %w", err)
	}
	if agent == nil {
		agent = &Agent{
			ProjectID:    projectID,
			Name:         def.Name,
			StrategyType: "agent-def:" + def.ID,
			CronSchedule: "0 0 * * *",
			TriggerType:  TriggerTypeManual,
		}
		if err := h.repo.Create(ctx, agent); err != nil {
			return nil, fmt.Errorf("failed to create runtime agent: %w", err)
		}
	}
	return agent, nil
}

// loadA2ATask loads the original run (project-scoped), resolves the latest run
// in its resume chain, and returns the latest run + its messages + pending
// question. The returned run's status reflects the whole chain (so a chain that
// paused again reports INPUT_REQUIRED), while callers keep Task.id stable via
// buildA2ATask.
func (h *A2AHandler) loadA2ATask(ctx context.Context, projectID, taskID string) (*AgentRun, []AgentRunMessage, *AgentQuestion, *A2AError) {
	original, err := h.repo.FindRunByID(ctx, taskID)
	if err != nil {
		return nil, nil, nil, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load task: "+err.Error())
	}
	if original == nil || original.Agent == nil || original.Agent.ProjectID != projectID {
		return nil, nil, nil, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found")
	}

	latest, err := h.repo.FindLatestRunInChain(ctx, taskID)
	if err != nil {
		return nil, nil, nil, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to resolve task chain: "+err.Error())
	}
	if latest == nil {
		return nil, nil, nil, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found")
	}

	msgs, _ := h.repo.FindMessagesByRunID(ctx, latest.ID)
	msgValues := make([]AgentRunMessage, len(msgs))
	for i, m := range msgs {
		msgValues[i] = *m
	}

	var question *AgentQuestion
	if latest.Status == RunStatusPaused {
		questions, _ := h.repo.FindPendingQuestionsByRunID(ctx, latest.ID)
		if len(questions) > 0 {
			question = questions[0]
		}
	}

	return latest, msgValues, question, nil
}

// respondWithA2ATask builds the SendMessageResponse carrying the task (and a
// final agent message when the run completed with text).
func (h *A2AHandler) respondWithA2ATask(c echo.Context, projectID, taskID string) error {
	ctx := c.Request().Context()
	latest, messages, question, a2aErr := h.loadA2ATask(ctx, projectID, taskID)
	if a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	task := buildA2ATask(latest, messages, question, taskID, -1)
	resp := SendMessageResponse{Task: &task}
	if task.Status.State == TaskStateCompleted {
		if final := finalAssistantText(messages); final != "" {
			resp.Message = &Message{Role: RoleAgent, Parts: []Part{TextPart(final)}}
		}
	}
	return writeA2AJSON(c, resp)
}

// ---------------------------------------------------------------------------
// POST /message:send
// ---------------------------------------------------------------------------

// SendMessage handles POST /message:send. With no message.taskId it starts a new
// task; with a taskId it resumes an INPUT_REQUIRED task.
func (h *A2AHandler) SendMessage(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	var req SendMessageRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return writeA2AError(c, a2aValidationError("invalid request body"))
	}

	userMessage := a2aUserMessageFromParts(req.Message.Parts)
	if userMessage == "" {
		return writeA2AError(c, a2aValidationError("message must contain at least one text part"))
	}

	userID := ""
	if u := auth.GetUser(c); u != nil {
		userID = u.ID
	}

	if req.Message.TaskID == "" {
		skillID := a2aSkillIDFromMetadata(req.Message.Metadata)
		return h.startA2ATask(c, projectID, userID, userMessage, req.Message.ContextID, skillID, req.Configuration)
	}
	return h.resumeA2ATask(c, projectID, userID, userMessage, req.Message.TaskID, req.Message.ContextID, req.Configuration)
}

// startA2ATask creates a new task: resolve/create context, create a run with
// TriggerSource "a2a", then execute (sync block or async fire-and-forget).
func (h *A2AHandler) startA2ATask(c echo.Context, projectID, userID, userMessage, contextID, skillID string, cfg *SendMessageConfiguration) error {
	ctx := c.Request().Context()

	def, agent, err := h.resolveA2AAgent(ctx, projectID, skillID)
	if err != nil {
		if errors.Is(err, errA2ASkillNotFound) {
			return writeA2AError(c, a2aSkillNotFoundError(skillID))
		}
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, err.Error()))
	}

	// Resolve or lazily create the context (kb.acp_sessions).
	if contextID != "" {
		session, err := h.repo.GetACPSession(ctx, projectID, contextID)
		if err != nil {
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load context: "+err.Error()))
		}
		if session == nil {
			return writeA2AError(c, a2aValidationError("unknown contextId"))
		}
	} else {
		session := &ACPSession{ProjectID: projectID, AgentName: strPtr(def.Name)}
		if err := h.repo.CreateACPSession(ctx, session); err != nil {
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to create context: "+err.Error()))
		}
		contextID = session.ID
	}

	triggerSource := "a2a"
	run, err := h.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:        agent.ID,
		TriggerSource:  &triggerSource,
		TriggerMessage: &userMessage,
	})
	if err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to create run: "+err.Error()))
	}

	if err := h.repo.UpdateRunACPSessionID(ctx, run.ID, contextID); err != nil {
		h.log.Warn("failed to link run to context",
			"run_id", run.ID, "context_id", contextID, "error", err.Error(),
		)
	}

	orgID := h.a2aOrgID(ctx, c, projectID)
	execReq := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserID:          userID,
		UserMessage:     userMessage,
	}

	if cfg != nil && cfg.ReturnImmediately {
		go func() {
			bgCtx := context.Background()
			result, execErr := h.executor.ExecuteWithRun(bgCtx, run, execReq)
			if result != nil && result.Cleanup != nil {
				result.Cleanup()
			}
			if execErr != nil {
				h.log.Error("a2a async run failed", "run_id", run.ID, "error", execErr.Error())
			}
		}()
		task := RunToA2ATask(run, nil, nil, nil)
		return writeA2AJSON(c, SendMessageResponse{Task: &task})
	}

	result, execErr := h.executor.ExecuteWithRun(ctx, run, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if execErr != nil {
		h.log.Error("a2a sync run failed", "run_id", run.ID, "error", execErr.Error())
	}

	return h.respondWithA2ATask(c, projectID, run.ID)
}

// resumeA2ATask resumes an INPUT_REQUIRED task with a follow-up message. The
// returned Task.id is the original task id (never the internal resume run id).
func (h *A2AHandler) resumeA2ATask(c echo.Context, projectID, userID, userMessage, taskID, contextID string, _ *SendMessageConfiguration) error {
	ctx := c.Request().Context()

	original, err := h.repo.FindRunByID(ctx, taskID)
	if err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load task: "+err.Error()))
	}
	if original == nil || original.Agent == nil || original.Agent.ProjectID != projectID {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}
	if contextID != "" && derefString(original.ACPSessionID) != contextID {
		return writeA2AError(c, a2aValidationError("contextId does not match taskId"))
	}

	// Resume the latest run in the chain (handles a chain that paused again).
	latest, err := h.repo.FindLatestRunInChain(ctx, taskID)
	if err != nil || latest == nil {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}
	if latest.Status != RunStatusPaused {
		return writeA2AError(c, a2aInvalidStateError(fmt.Sprintf("task is not awaiting input (state %s)", MapRunStatusToTaskState(latest.Status))))
	}

	questions, err := h.repo.FindPendingQuestionsByRunID(ctx, latest.ID)
	if err != nil || len(questions) == 0 {
		return writeA2AError(c, a2aInvalidStateError("no pending question for task"))
	}

	// Atomically claim the pending question (mirror ACP AnswerQuestion path); a
	// concurrent responder wins and this call loses.
	claimed, err := h.repo.AnswerQuestion(ctx, questions[0].ID, userMessage, userID)
	if err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to answer question: "+err.Error()))
	}
	if !claimed {
		return writeA2AError(c, a2aInvalidStateError("question is no longer pending"))
	}

	resumeMsg := fmt.Sprintf(
		"Previously you asked: \"%s\"\nThe user responded: \"%s\"\nContinue from where you left off.",
		questions[0].Question, userMessage,
	)

	def, err := h.repo.ResolveDefinitionForAgent(ctx, latest.Agent)
	if err != nil {
		def = nil
	}
	if def == nil && latest.Agent != nil {
		def, _ = h.repo.FindDefinitionByName(ctx, projectID, latest.Agent.Name)
	}

	orgID := h.a2aOrgID(ctx, c, projectID)
	execReq := ExecuteRequest{
		Agent:           latest.Agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           orgID,
		UserID:          userID,
		UserMessage:     resumeMsg,
	}

	result, execErr := h.executor.Resume(ctx, latest, execReq)
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if execErr != nil {
		h.log.Error("a2a resume failed", "task_id", taskID, "error", execErr.Error())
	}

	return h.respondWithA2ATask(c, projectID, taskID)
}

// ---------------------------------------------------------------------------
// GET /tasks/{id}
// ---------------------------------------------------------------------------

// GetTask handles GET /tasks/{id}.
func (h *A2AHandler) GetTask(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}

	taskID := c.Param("id")
	if taskID == "" {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}

	historyLength := a2aQueryInt(c, "historyLength", -1)

	latest, messages, question, a2aErr := h.loadA2ATask(c.Request().Context(), projectID, taskID)
	if a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	task := buildA2ATask(latest, messages, question, taskID, historyLength)
	return writeA2AJSON(c, task)
}

// ---------------------------------------------------------------------------
// GET /tasks
// ---------------------------------------------------------------------------

// ListTasks handles GET /tasks (project-scoped, filtered by contextId/status).
func (h *A2AHandler) ListTasks(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	pageSize := a2aPageSize(a2aQueryInt(c, "pageSize", 50))
	offset := a2aDecodePageToken(c.QueryParam("pageToken"))
	contextID := c.QueryParam("contextId")
	statusParam := c.QueryParam("status")

	var statuses []AgentRunStatus
	if statusParam != "" {
		statuses = internalStatusesForTaskState(TaskState(statusParam))
		if statuses == nil {
			return writeA2AError(c, a2aValidationError("unknown status filter: "+statusParam))
		}
	}

	runs, total, err := h.repo.ListA2ARuns(ctx, projectID, contextID, statuses, pageSize, offset)
	if err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to list tasks: "+err.Error()))
	}

	tasks := make([]Task, 0, len(runs))
	for _, r := range runs {
		t := RunToA2ATask(r, nil, nil, nil)
		if md := hitlMetadata(r); md != nil {
			t.Metadata = md
		}
		tasks = append(tasks, t)
	}

	next := ""
	if offset+pageSize < total {
		next = a2aEncodePageToken(offset + pageSize)
	}

	return writeA2AJSON(c, ListTasksResponse{
		Tasks:         tasks,
		NextPageToken: next,
		PageSize:      pageSize,
		TotalSize:     total,
	})
}

// ---------------------------------------------------------------------------
// POST /tasks/{id}:cancel
// ---------------------------------------------------------------------------

// CancelTask handles POST /tasks/{id}:cancel.
func (h *A2AHandler) CancelTask(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	taskID := c.Param("id")
	if taskID == "" {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}

	original, err := h.repo.FindRunByID(ctx, taskID)
	if err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load task: "+err.Error()))
	}
	if original == nil || original.Agent == nil || original.Agent.ProjectID != projectID {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}

	latest, err := h.repo.FindLatestRunInChain(ctx, taskID)
	if err != nil || latest == nil {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}

	if isTerminalRunStatus(latest.Status) {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotCancelable, A2AReasonTaskNotCancelable, "task is in terminal state "+string(latest.Status)))
	}

	if latest.Status == RunStatusQueued {
		if err := h.repo.CancelRun(ctx, latest.ID); err != nil {
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to cancel task: "+err.Error()))
		}
	} else if err := h.repo.SetRunCancelling(ctx, latest.ID); err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to cancel task: "+err.Error()))
	}

	return h.respondWithA2ATask(c, projectID, taskID)
}

// ---------------------------------------------------------------------------
// POST /tasks/{id}:subscribe
// ---------------------------------------------------------------------------

// SubscribeTask handles POST /tasks/{id}:subscribe, streaming the task's events
// as SSE StreamResponse payloads.
//
// DESIGN NOTE: message:send executes synchronously in this milestone and does
// not emit to the events.Service SSE bus (that is the streaming lane's
// concern), so a subscribe on an A2A-originated task currently replays
// persisted events (empty for A2A tasks) plus the current snapshot, then holds
// for any live agent_run bus events keyed to the task. This is a partial
// implementation: live fan-out becomes fully live once the streaming lane wires
// executor StreamCallback → bus emission.
func (h *A2AHandler) SubscribeTask(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	projectID, err := acpProjectID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	taskID := c.Param("id")
	if taskID == "" {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}

	original, err := h.repo.FindRunByID(ctx, taskID)
	if err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load task: "+err.Error()))
	}
	if original == nil || original.Agent == nil || original.Agent.ProjectID != projectID {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}

	latest, err := h.repo.FindLatestRunInChain(ctx, taskID)
	if err != nil || latest == nil {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}
	contextID := derefString(latest.ACPSessionID)

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "SSE streaming not supported"))
	}
	defer writer.Close()

	// Replay persisted events first (best-effort).
	persisted, _ := h.repo.GetACPRunEvents(ctx, taskID)
	for _, ev := range persisted {
		if sr, ok := a2aStreamResponseFromEventType(ev.EventType, taskID, contextID); ok {
			_ = writer.WriteData(sr)
		}
	}

	// Emit the current task snapshot.
	msgs, _ := h.repo.FindMessagesByRunID(ctx, latest.ID)
	msgValues := make([]AgentRunMessage, len(msgs))
	for i, m := range msgs {
		msgValues[i] = *m
	}
	var question *AgentQuestion
	if latest.Status == RunStatusPaused {
		questions, _ := h.repo.FindPendingQuestionsByRunID(ctx, latest.ID)
		if len(questions) > 0 {
			question = questions[0]
		}
	}
	task := buildA2ATask(latest, msgValues, question, taskID, -1)
	_ = writer.WriteData(StreamResponse{Task: &task})

	// Terminal task: nothing more to stream.
	if isTerminalRunStatus(latest.Status) {
		return nil
	}
	if h.eventsSvc == nil {
		return nil
	}

	done := c.Request().Context().Done()
	unsubscribe := h.eventsSvc.Subscribe(projectID, func(ev events.EntityEvent) {
		if ev.Entity != events.EntityAgentRun || ev.ID == nil || *ev.ID != taskID {
			return
		}
		typ, _ := ev.Data["type"].(string)
		state, ok := a2aEventTypeToTaskState(typ)
		if !ok {
			return
		}
		_ = writer.WriteData(StreamResponse{
			StatusUpdate: &TaskStatusUpdateEvent{
				TaskID:    taskID,
				ContextID: contextID,
				Status:    TaskStatus{State: state},
			},
		})
	})
	defer unsubscribe()

	<-done
	return nil
}
