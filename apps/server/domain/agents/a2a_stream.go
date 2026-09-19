package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// A2A streaming + HITL emission lane (POST /message:stream).
//
// This is a stateless facade over the run engine: each `data:` line is a
// StreamResponse discriminated by member presence (`task`, `message`,
// `statusUpdate`, `artifactUpdate`), never a `kind` field. Internal stream
// events are translated per design.md's event map (Decision 3).

// ---------------------------------------------------------------------------
// Stream translator (pure, unit-testable)
// ---------------------------------------------------------------------------

// a2aStreamTranslator accumulates a turn's streaming state and translates
// internal StreamEvents into A2A StreamResponse payloads. It is a pure, non-I/O
// type so the event map can be tested without a database or executor.
type a2aStreamTranslator struct {
	taskID    string
	contextID string

	// textArtID is the stable artifactId for the growing streaming text
	// artifact; textBuf accumulates assistant text deltas so each artifactUpdate
	// carries the full (growing) text part.
	textArtID string
	textBuf   strings.Builder

	// thinking accumulates reasoning/planning deltas, which A2A has no member
	// for; they are folded into the final ROLE_AGENT message metadata.
	thinking []string

	// toolSeq/toolArtID give each tool-call trajectory a stable artifactId so a
	// call's start and end deltas accumulate onto the same artifact.
	toolSeq   int
	toolArtID string
}

// newA2aStreamTranslator builds a translator for a task/context pair.
func newA2aStreamTranslator(taskID, contextID string) *a2aStreamTranslator {
	return &a2aStreamTranslator{
		taskID:    taskID,
		contextID: contextID,
		textArtID: "artifact-" + taskID,
	}
}

// translate maps one internal stream event to zero or more A2A StreamResponse
// payloads, in emission order. Thinking and tool-approval events produce no
// payload: thinking folds into final-message metadata and the approval gate is
// reported as the terminal INPUT_REQUIRED statusUpdate after execution.
func (t *a2aStreamTranslator) translate(ev StreamEvent) []StreamResponse {
	switch ev.Type {
	case StreamEventTextDelta:
		t.textBuf.WriteString(ev.Text)
		return []StreamResponse{{
			ArtifactUpdate: &TaskArtifactUpdateEvent{
				TaskID:    t.taskID,
				ContextID: t.contextID,
				Artifact:  Artifact{ArtifactID: t.textArtID, Parts: []Part{TextPart(t.textBuf.String())}},
			},
		}}

	case StreamEventThinking:
		// Never its own stream member; folded into final-message metadata.
		t.thinking = append(t.thinking, ev.Text)
		return nil

	case StreamEventToolCallStart:
		t.toolSeq++
		t.toolArtID = fmt.Sprintf("tool-call-%d", t.toolSeq)
		return []StreamResponse{{
			ArtifactUpdate: &TaskArtifactUpdateEvent{
				TaskID:    t.taskID,
				ContextID: t.contextID,
				Artifact: Artifact{
					ArtifactID: t.toolArtID,
					Name:       ev.Tool,
					Parts:      []Part{{Data: map[string]any{"toolName": ev.Tool, "input": ev.Input}}},
				},
			},
		}}

	case StreamEventToolCallEnd:
		return []StreamResponse{{
			ArtifactUpdate: &TaskArtifactUpdateEvent{
				TaskID:    t.taskID,
				ContextID: t.contextID,
				Artifact: Artifact{
					ArtifactID: t.toolArtID,
					Name:       ev.Tool,
					Parts:      []Part{{Data: map[string]any{"toolName": ev.Tool, "output": ev.Output}}},
				},
				Append: true,
			},
		}}

	case StreamEventError:
		return []StreamResponse{a2aStatusUpdate(t.taskID, t.contextID, TaskStateFailed, ev.Error)}

	case StreamEventToolApproval:
		// The run pauses; the terminal INPUT_REQUIRED statusUpdate is emitted
		// after execution, not from this intermediate gate event.
		return nil

	default:
		return nil
	}
}

// finalMessage builds the immutable ROLE_AGENT message emitted at turn end,
// preferring the persisted assistant text and falling back to the accumulated
// stream deltas. Thinking deltas are folded into metadata.
func (t *a2aStreamTranslator) finalMessage(messages []AgentRunMessage) *Message {
	text := finalAssistantText(messages)
	if text == "" {
		text = t.textBuf.String()
	}
	if text == "" {
		return nil
	}
	msg := &Message{Role: RoleAgent, Parts: []Part{TextPart(text)}}
	if len(t.thinking) > 0 {
		msg.Metadata = map[string]any{"thinking": strings.Join(t.thinking, "\n")}
	}
	return msg
}

// finalTextArtifactUpdate marks the streaming text artifact complete.
func (t *a2aStreamTranslator) finalTextArtifactUpdate() *StreamResponse {
	if t.textBuf.Len() == 0 {
		return nil
	}
	return &StreamResponse{ArtifactUpdate: &TaskArtifactUpdateEvent{
		TaskID:    t.taskID,
		ContextID: t.contextID,
		Artifact:  Artifact{ArtifactID: t.textArtID, Parts: []Part{TextPart(t.textBuf.String())}},
		LastChunk: true,
	}}
}

// failedEvents builds the terminal sequence for a failed run.
func (t *a2aStreamTranslator) failedEvents(errMsg string) []StreamResponse {
	return []StreamResponse{a2aStatusUpdate(t.taskID, t.contextID, TaskStateFailed, errMsg)}
}

// inputRequiredEvents builds the terminal sequence for a HITL pause: a single
// INPUT_REQUIRED statusUpdate carrying the prompt in status.message. The stream
// closes after it (documented milestone-1 deviation).
func (t *a2aStreamTranslator) inputRequiredEvents(prompt string) []StreamResponse {
	return []StreamResponse{a2aStatusUpdate(t.taskID, t.contextID, TaskStateInputRequired, prompt)}
}

// cancelledEvents builds the terminal sequence for a cancelled run.
func (t *a2aStreamTranslator) cancelledEvents() []StreamResponse {
	return []StreamResponse{a2aStatusUpdate(t.taskID, t.contextID, TaskStateCanceled, "")}
}

// completedEvents builds the terminal sequence for a successful run: final
// ROLE_AGENT message, final text artifact, then the COMPLETED task snapshot
// (last).
func (t *a2aStreamTranslator) completedEvents(messages []AgentRunMessage, latest *AgentRun, question *AgentQuestion) []StreamResponse {
	var out []StreamResponse

	if msg := t.finalMessage(messages); msg != nil {
		out = append(out, StreamResponse{Message: msg})
	}
	if au := t.finalTextArtifactUpdate(); au != nil {
		out = append(out, *au)
	}

	task := Task{ID: t.taskID, ContextID: t.contextID, Status: TaskStatus{State: TaskStateCompleted}}
	if latest != nil {
		task = buildA2ATask(latest, messages, question, t.taskID, -1)
	}
	out = append(out, StreamResponse{Task: &task})

	return out
}

// a2aStatusUpdate builds a statusUpdate StreamResponse for a task state, folding
// an optional message into status.message (agent role).
func a2aStatusUpdate(taskID, contextID string, state TaskState, msgText string) StreamResponse {
	su := &TaskStatusUpdateEvent{
		TaskID:    taskID,
		ContextID: contextID,
		Status:    TaskStatus{State: state},
	}
	if msgText != "" {
		su.Status.Message = textStatusMessage(msgText)
	}
	return StreamResponse{StatusUpdate: su}
}

// a2aDeltaEventType maps an internal stream event to the reused ACP run-event
// type persisted alongside emission. Returns false when the event has no
// persistence counterpart (tool-approval gate, unknown).
func a2aDeltaEventType(ev StreamEvent) (string, bool) {
	switch ev.Type {
	case StreamEventTextDelta, StreamEventThinking:
		return ACPEventMessagePart, true
	case StreamEventToolCallStart:
		return ACPEventToolCall, true
	case StreamEventToolCallEnd:
		return ACPEventToolResult, true
	case StreamEventError:
		return ACPEventError, true
	default:
		return "", false
	}
}

// ---------------------------------------------------------------------------
// Persistence / bus helpers
// ---------------------------------------------------------------------------

// persistA2AEvent inserts a run event into the reused kb.acp_run_events table so
// GetTask history and SubscribeTask replay stay consistent.
func (h *A2AHandler) persistA2AEvent(ctx context.Context, runID, eventType string, data map[string]any) {
	if h.repo == nil {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	event := &ACPRunEvent{RunID: runID, EventType: eventType, Data: data}
	if err := h.repo.InsertACPRunEvent(ctx, event); err != nil {
		h.log.Warn("failed to persist A2A run event",
			slog.String("run_id", runID),
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}

// emitA2ABus publishes an A2A lifecycle event onto the events.Service SSE bus so
// SubscribeTask receives live status transitions for streamed tasks.
func (h *A2AHandler) emitA2ABus(projectID, runID, eventType string) {
	if h.eventsSvc == nil || projectID == "" {
		return
	}
	h.eventsSvc.EmitCreated(events.EntityAgentRun, runID, projectID, &events.EmitOptions{
		Data: map[string]any{"type": eventType, "run_id": runID},
	})
}

// ---------------------------------------------------------------------------
// POST /message:stream
// ---------------------------------------------------------------------------

// StreamMessage handles POST /message:stream (SSE). Each `data:` line is a
// StreamResponse discriminated by member presence. With no message.taskId it
// streams a new task; with a taskId it resumes an INPUT_REQUIRED task.
func (h *A2AHandler) StreamMessage(c echo.Context) error {
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
		return h.streamNewTask(c, projectID, userID, userMessage, req.Message.ContextID, skillID)
	}
	return h.streamResumeTask(c, projectID, userID, userMessage, req.Message.TaskID, req.Message.ContextID)
}

// streamNewTask resolves the agent, lazily links/creates the context, creates
// the run, and streams it. Mirrors startA2ATask's setup but over SSE.
func (h *A2AHandler) streamNewTask(c echo.Context, projectID, userID, userMessage, contextID, skillID string) error {
	ctx := c.Request().Context()

	def, agent, err := h.resolveA2AAgent(ctx, projectID, skillID)
	if err != nil {
		if errors.Is(err, errA2ASkillNotFound) {
			return writeA2AError(c, a2aSkillNotFoundError(skillID))
		}
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, err.Error()))
	}

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

	execReq := ExecuteRequest{
		Agent:           agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           h.a2aOrgID(ctx, c, projectID),
		UserID:          userID,
		UserMessage:     userMessage,
	}

	return h.streamRun(c, projectID, run.ID, contextID, run, execReq, false)
}

// streamResumeTask resumes an INPUT_REQUIRED task with a follow-up message and
// streams the continuation. Mirrors resumeA2ATask's claim/resume setup.
func (h *A2AHandler) streamResumeTask(c echo.Context, projectID, userID, userMessage, taskID, contextID string) error {
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

	streamCtxID := derefString(original.ACPSessionID)
	if streamCtxID == "" {
		streamCtxID = contextID
	}

	execReq := ExecuteRequest{
		Agent:           latest.Agent,
		AgentDefinition: def,
		ProjectID:       projectID,
		OrgID:           h.a2aOrgID(ctx, c, projectID),
		UserID:          userID,
		UserMessage:     resumeMsg,
	}

	return h.streamRun(c, projectID, taskID, streamCtxID, latest, execReq, true)
}

// streamRun executes (or resumes) a run and streams A2A StreamResponse events
// over SSE. taskID is the stable A2A task id; run is the run passed to the
// executor (the fresh run on new-task, the latest paused run on resume).
func (h *A2AHandler) streamRun(
	c echo.Context,
	projectID, taskID, contextID string,
	run *AgentRun,
	execReq ExecuteRequest,
	resume bool,
) error {
	ctx := c.Request().Context()
	bgCtx := context.Background()

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "SSE streaming not supported"))
	}
	defer writer.Close()

	tr := newA2aStreamTranslator(taskID, contextID)

	// write emits a StreamResponse to the wire. lifecycle persists + fans out to
	// the bus separately so the terminal multi-event sequence persists once.
	write := func(sr StreamResponse) {
		_ = writer.WriteData(sr)
	}
	lifecycle := func(eventType string) {
		h.persistA2AEvent(bgCtx, taskID, eventType, nil)
		h.emitA2ABus(projectID, taskID, eventType)
	}

	// Initial task (SUBMITTED) on the new-task path only.
	if !resume {
		task := RunToA2ATask(run, nil, nil, nil)
		task.ID = taskID
		task.ContextID = contextID
		write(StreamResponse{Task: &task})
		lifecycle(ACPEventRunCreated)
	}

	// Working transition.
	write(a2aStatusUpdate(taskID, contextID, TaskStateWorking, ""))
	lifecycle(ACPEventRunInProgress)

	// Translate + emit streaming deltas as they arrive, persisting each delta
	// alongside emission (mirror makeEventPersistingCallback).
	execReq.StreamCallback = func(ev StreamEvent) {
		for _, sr := range tr.translate(ev) {
			_ = writer.WriteData(sr)
		}
		if et, ok := a2aDeltaEventType(ev); ok {
			h.persistA2AEvent(bgCtx, taskID, et, nil)
		}
	}

	var result *ExecuteResult
	var execErr error
	if resume {
		result, execErr = h.executor.Resume(ctx, run, execReq)
	} else {
		result, execErr = h.executor.ExecuteWithRun(ctx, run, execReq)
	}
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if execErr != nil {
		h.log.Error("a2a stream run failed", "task_id", taskID, "error", execErr.Error())
	}

	h.streamTerminal(bgCtx, writer, tr, projectID, taskID, result, execErr)
	return nil
}

// streamTerminal emits the terminal StreamResponse(s) for a finished run and
// lets streamRun's deferred Close end the stream.
func (h *A2AHandler) streamTerminal(
	ctx context.Context,
	writer *sse.Writer,
	tr *a2aStreamTranslator,
	projectID, taskID string,
	result *ExecuteResult,
	execErr error,
) {
	write := func(sr StreamResponse) {
		_ = writer.WriteData(sr)
	}
	lifecycle := func(eventType string) {
		h.persistA2AEvent(ctx, taskID, eventType, nil)
		h.emitA2ABus(projectID, taskID, eventType)
	}

	if execErr != nil {
		for _, sr := range tr.failedEvents(execErr.Error()) {
			write(sr)
		}
		lifecycle(ACPEventRunFailed)
		return
	}
	if result == nil {
		for _, sr := range tr.failedEvents("run produced no result") {
			write(sr)
		}
		lifecycle(ACPEventRunFailed)
		return
	}

	runID := result.RunID
	if runID == "" {
		runID = taskID
	}

	switch result.Status {
	case RunStatusPaused:
		// HITL (documented milestone-1 deviation): emit INPUT_REQUIRED with the
		// prompt in status.message and close the stream. The client resumes with
		// a new message:stream/message:send carrying the same taskId.
		prompt := ""
		questions, _ := h.repo.FindPendingQuestionsByRunID(ctx, runID)
		if len(questions) > 0 {
			prompt = questions[0].Question
		}
		for _, sr := range tr.inputRequiredEvents(prompt) {
			write(sr)
		}
		lifecycle(ACPEventRunAwaiting)

	case RunStatusError:
		errMsg := "unknown error"
		if errRun, _ := h.repo.FindRunByID(ctx, runID); errRun != nil && errRun.ErrorMessage != nil {
			errMsg = *errRun.ErrorMessage
		}
		for _, sr := range tr.failedEvents(errMsg) {
			write(sr)
		}
		lifecycle(ACPEventRunFailed)

	case RunStatusCancelled:
		for _, sr := range tr.cancelledEvents() {
			write(sr)
		}
		lifecycle(ACPEventRunCancelled)

	default: // Success / Skipped → COMPLETED
		latest, messages, question, a2aErr := h.loadA2ATask(ctx, projectID, taskID)
		if a2aErr != nil {
			latest = nil
		}
		for _, sr := range tr.completedEvents(messages, latest, question) {
			write(sr)
		}
		lifecycle(ACPEventRunCompleted)
	}
}
