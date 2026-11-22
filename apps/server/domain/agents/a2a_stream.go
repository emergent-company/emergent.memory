package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/sse"
)

// A2A streaming + HITL emission lane (POST /message:stream).
//
// This is a stateless facade over the run engine: each `data:` line is a
// StreamResponse discriminated by member presence (`task`, `message`,
// `statusUpdate`, `artifactUpdate`), never a `kind` field. Internal stream
// events are translated through the explicit event map in this file.

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

	// toolArtID is the artifactId of the most recent tool call, so a call's
	// start and end deltas accumulate onto the same artifact. The id is derived
	// from the ADK function-call id (StreamEvent.ID) when the event carries one:
	// a per-translator counter restarts on every stream, and a resumed stream
	// builds a fresh translator for the same task, so a counter would make the
	// resumed turn reuse the paused turn's toolCallIds (#904). toolSeq is only
	// the fallback for events that arrive without a function-call id.
	toolSeq   int
	toolArtID string
}

// toolCallArtifactID builds the A2A artifactId for a tool call from its ADK
// function-call id. The prefix keeps tool artifacts distinguishable from the
// text ("artifact-<task>") and A2UI ("a2ui-<surface>") artifact ids on the same
// task; the id part is what makes it unique per call rather than per stream.
func toolCallArtifactID(functionCallID string) string {
	return "tool-call-" + functionCallID
}

// nextToolArtifactID assigns the artifactId for a newly started tool call,
// preferring the function-call id and falling back to the per-stream counter
// when the event carries none.
func (t *a2aStreamTranslator) nextToolArtifactID(ev StreamEvent) string {
	if ev.ID != "" {
		return toolCallArtifactID(ev.ID)
	}
	t.toolSeq++
	return fmt.Sprintf("tool-call-%d", t.toolSeq)
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
		// Skip the segment-closing event (Done:true, empty text) — it only
		// marks the end of an open segment and carries no content.
		if ev.Done {
			return nil
		}
		t.thinking = append(t.thinking, ev.Text)
		return nil

	case StreamEventToolCallStart:
		t.toolArtID = t.nextToolArtifactID(ev)
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
		// The end carries the same function-call id as its start, so deriving
		// the id here keeps the pair correlated even when another call started
		// in between; the last assignment is the fallback for an id-less event.
		artID := t.toolArtID
		if ev.ID != "" {
			artID = toolCallArtifactID(ev.ID)
		}
		return []StreamResponse{{
			ArtifactUpdate: &TaskArtifactUpdateEvent{
				TaskID:    t.taskID,
				ContextID: t.contextID,
				Artifact: Artifact{
					ArtifactID: artID,
					Name:       ev.Tool,
					Parts:      []Part{{Data: map[string]any{"toolName": ev.Tool, "output": ev.Output}}},
				},
				Append: true,
			},
		}}

	case StreamEventA2UI:
		return []StreamResponse{{
			ArtifactUpdate: &TaskArtifactUpdateEvent{
				TaskID:    t.taskID,
				ContextID: t.contextID,
				Artifact:  Artifact{ArtifactID: "a2ui-" + ev.SurfaceID, Parts: []Part{a2uiDataPart(ev.A2UI)}},
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

// a2uiDataPart builds the A2A data part carrying an ordered array of A2UI
// surface messages, marked with the application/a2ui+json MIME type so A2UI-
// capable clients render it and others ignore it.
func a2uiDataPart(msgs []a2ui.Message) Part {
	return Part{
		Data:     msgs,
		Metadata: map[string]any{"mimeType": A2UIMimeType},
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
	msg := &Message{MessageID: newA2AMessageID(), Role: RoleAgent, Parts: []Part{TextPart(text)}}
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
// INPUT_REQUIRED statusUpdate carrying the prompt in status.message plus the
// pause-source discriminator (ask_user vs tool_approval) in status-level
// metadata so a resuming client can tell the two apart. The stream closes after
// it (documented milestone-1 deviation).
func (t *a2aStreamTranslator) inputRequiredEvents(prompt string, run *AgentRun) []StreamResponse {
	su := &TaskStatusUpdateEvent{
		TaskID:    t.taskID,
		ContextID: t.contextID,
		Status:    TaskStatus{State: TaskStateInputRequired},
	}
	if prompt != "" {
		su.Status.Message = textStatusMessage(prompt)
	}
	if md := hitlMetadata(run); md != nil {
		su.Metadata = md
	}
	return []StreamResponse{{StatusUpdate: su}}
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

// a2aResumeConflictResponse reports, inside a stream that has already started,
// that a concurrent follow-up consumed the pause and owns the task. It carries no
// terminal state: the winner is still executing the task.
func a2aResumeConflictResponse(taskID, contextID string) StreamResponse {
	msg := textStatusMessage("This task is already being resumed by another follow-up. Follow it with tasks/" + taskID + ":subscribe.")
	msg.TaskID = taskID
	msg.ContextID = contextID
	return StreamResponse{Message: msg}
}

// a2aDeltaEventType maps an internal stream event to the reused run-event
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

// persistA2AEvent inserts a run event into the reused kb.run_events table so
// GetTask history and SubscribeTask replay stay consistent.
func (h *A2AHandler) persistA2AEvent(ctx context.Context, runID, eventType string, data map[string]any) {
	if h.repo == nil {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	event := &RunEvent{RunID: runID, EventType: eventType, Data: data}
	if err := h.repo.InsertRunEvent(ctx, event); err != nil {
		h.log.Warn("failed to persist A2A run event",
			slog.String("run_id", runID),
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}

// a2aStreamPayload wraps a translated StreamResponse as the payload stored on a
// persisted run event (and carried on the bus) so SubscribeTask can replay the
// full artifact/message/status delta rather than just the lifecycle type. The
// key "stream" is what a2aStreamResponseFromEventData decodes on the other end.
func a2aStreamPayload(sr StreamResponse) map[string]any {
	return map[string]any{"stream": sr}
}

// emitA2ABus publishes an A2A event onto the events.Service SSE bus so
// SubscribeTask receives live status transitions and translated deltas for
// streamed tasks. payload (when non-nil) carries the translated StreamResponse
// under the "stream" key; lifecycle-only events pass nil.
func (h *A2AHandler) emitA2ABus(ctx context.Context, projectID, runID, eventType string, payload map[string]any) {
	if h.eventsSvc == nil || projectID == "" {
		return
	}
	data := map[string]any{"type": eventType, "run_id": runID}
	for k, v := range payload {
		data[k] = v
	}
	h.eventsSvc.EmitCreated(ctx, events.EntityAgentRun, runID, projectID, &events.EmitOptions{Data: data})
}

// a2aInitialTask builds the SUBMITTED snapshot emitted as the first event of a
// new-task stream. CreateRunWithOptions creates the run with the internal
// RunStatusRunning, so a plain RunToA2ATask would serialize WORKING; the A2A
// wire contract is SUBMITTED-first, so the state is forced here.
func a2aInitialTask(run *AgentRun, taskID, contextID string) Task {
	task := RunToA2ATask(run, nil, nil, nil)
	task.ID = taskID
	task.ContextID = contextID
	task.Status.State = TaskStateSubmitted
	return task
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

	projectID, err := h.addressedProject(c)
	if err != nil {
		return err
	}

	var req SendMessageRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return writeA2AError(c, a2aValidationError("invalid request body"))
	}

	// Validate required A2A Message fields before routing or execution, matching
	// SendMessage's gate.
	if req.Message.MessageID == "" {
		return writeA2AError(c, a2aValidationError("message.messageId is required"))
	}
	if !isValidA2ARole(req.Message.Role) {
		return writeA2AError(c, a2aValidationError("message.role is required and must be ROLE_USER or ROLE_AGENT"))
	}

	userMessage := a2aUserMessageFromParts(req.Message.Parts)
	surfaceAction, isSurfaceAction := a2uiActionFromMetadata(req.Message.Metadata)
	if userMessage == "" && !isSurfaceAction {
		return writeA2AError(c, a2aValidationError("message must contain at least one text part"))
	}

	userID := ""
	if u := auth.GetUser(c); u != nil {
		userID = u.ID
	}

	if isSurfaceAction {
		contextID, a2aErr := h.a2uiSurfaceActionContext(c.Request().Context(), req)
		if a2aErr != nil {
			return writeA2AError(c, a2aErr)
		}
		return h.streamNewTask(c, projectID, userID, A2UIActionMessage(surfaceAction, userMessage), contextID, a2aSkillIDFromMetadata(req.Message.Metadata))
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
		h.log.Error("failed to resolve agent", "project_id", projectID, "error", err)
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to resolve agent"))
	}

	// Resolve or lazily create the context (kb.sessions).
	session, err := h.repo.EnsureSessionForContext(ctx, projectID, contextID, strPtr(def.Name))
	if err != nil {
		h.log.Error("failed to resolve context", "context_id", contextID, "project_id", projectID, "error", err)
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to resolve context"))
	}
	if session == nil {
		return writeA2AError(c, a2aValidationError("unknown contextId"))
	}
	contextID = session.ID

	// Kill switch: refuse before creating the run row. Creating it first would
	// leave a `running` row until the stale-run reaper (same guarantee as the
	// JSON /message path).
	if h.executor != nil {
		if err := h.executor.EnsureExecutionEnabled(); err != nil {
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, err.Error()))
		}
		// Budget pre-flight before creating the run row: a capped project must
		// not leave an A2A run that can never execute.
		if err := h.executor.ensureBudgetAvailable(ctx, projectID, agent.ID, "a2a"); err != nil {
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, err.Error()))
		}
	}

	triggerSource := "a2a"
	run, err := h.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:        agent.ID,
		TriggerSource:  &triggerSource,
		TriggerMessage: &userMessage,
		// An authenticated A2A caller ordered this run; an anonymous one did not.
		OrderedByUserID: OrderedByUser(userID),
	})
	if err != nil {
		h.log.Error("failed to create run", "agent_id", agent.ID, "error", err)
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to create run"))
	}

	if err := h.repo.UpdateRunSessionID(ctx, run.ID, contextID); err != nil {
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
		// TrustedInternal defaults false: an A2A surface must not reach internal agents.
		RenderA2UI: true, // A2A stream renders A2UI surfaces (artifactUpdate)
	}

	return h.streamRun(c, projectID, run.ID, contextID, run, execReq, false)
}

// streamResumeTask resumes an INPUT_REQUIRED task with a follow-up message and
// streams the continuation. Mirrors resumeA2ATask's claim/resume setup.
func (h *A2AHandler) streamResumeTask(c echo.Context, projectID, userID, userMessage, taskID, contextID string) error {
	ctx := c.Request().Context()

	original, err := h.repo.FindRunByID(ctx, taskID)
	if err != nil {
		h.log.Error("failed to load task", "task_id", taskID, "error", err)
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load task"))
	}
	if original == nil || original.Agent == nil || original.Agent.ProjectID != projectID {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}
	if contextID != "" && derefString(original.SessionID) != contextID {
		return writeA2AError(c, a2aValidationError("contextId does not match taskId"))
	}

	latest, err := h.repo.FindLatestRunInChain(ctx, taskID)
	if err != nil || latest == nil {
		return writeA2AError(c, NewA2AError(A2ACodeTaskNotFound, A2AReasonTaskNotFound, "task not found"))
	}
	if latest.Status != RunStatusPaused {
		return writeA2AError(c, a2aInvalidStateError(fmt.Sprintf("task is not awaiting input (state %s)", MapRunStatusToTaskState(latest.Status))))
	}

	resumeMsg, pauseClaimed, resumeErr := h.prepareA2AResume(ctx, latest, userMessage, userID)
	if resumeErr != nil {
		return writeA2AError(c, resumeErr)
	}

	def, err := h.repo.ResolveDefinitionForAgent(ctx, latest.Agent)
	if err != nil {
		def = nil
	}
	if def == nil && latest.Agent != nil {
		def, _ = h.repo.FindDefinitionByName(ctx, projectID, latest.Agent.Name)
	}

	streamCtxID := derefString(original.SessionID)
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
		PauseClaimed:    pauseClaimed,
		// TrustedInternal defaults false: an A2A surface must not reach internal agents.
		RenderA2UI: true, // A2A stream renders A2UI surfaces (artifactUpdate)
	}

	return h.streamRun(c, projectID, taskID, streamCtxID, latest, execReq, true)
}

// a2aAbandonTimeout bounds the durable-cancel write issued when an SSE consumer
// disappears. The run is already being stopped locally, so a slow database must
// not keep the handler or its goroutine alive.
const a2aAbandonTimeout = 5 * time.Second

// a2aStreamGuard stops the run behind an SSE stream when the client consuming
// it goes away (issue #868). A vanished consumer used to be invisible: the
// handler discarded every WriteData error, so a closed connection did not fail
// the request and the run kept stepping in its sandbox with nobody reading the
// output — tokens and workspace mutations spent on a turn the user had already
// cancelled.
//
// It fires at most once, on either signal: a failed SSE write or a cancelled
// request context (client disconnect). It is disarmed when the handler
// completes normally, so a run that produced its terminal frame is never
// cancelled after the fact.
//
// The run id is tracked instead of fixed at construction because a resume
// creates a fresh child run: cancelling the paused checkpoint would strand the
// resume instead of stopping the work. An abandonment that lands before the
// child id is known is remembered and applied as soon as the id is tracked.
type a2aStreamGuard struct {
	mu        sync.Mutex
	runID     string
	abandoned bool
	lateFired bool
	stopped   bool
	stopCh    chan struct{}
	onAbandon func(runID string)
}

// newA2AStreamGuard builds a guard for a stream executing initialRunID (empty
// when the executing run does not exist yet, as on the resume path).
func newA2AStreamGuard(initialRunID string, onAbandon func(runID string)) *a2aStreamGuard {
	return &a2aStreamGuard{
		runID:     initialRunID,
		stopCh:    make(chan struct{}),
		onAbandon: onAbandon,
	}
}

// watch fires the guard when ctx is cancelled, unless the handler finished
// first. It runs on its own goroutine.
func (g *a2aStreamGuard) watch(ctx context.Context) {
	select {
	case <-ctx.Done():
		g.abandon()
	case <-g.stopCh:
	}
}

// trackRun records the run that is executing now. On the resume path this is
// called from ExecuteRequest.OnRunCreated once the child run row exists, which
// is why it may arrive after an abandonment already fired.
func (g *a2aStreamGuard) trackRun(runID string) {
	if runID == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	g.runID = runID
	if g.abandoned && !g.lateFired {
		g.lateFired = true
		g.onAbandon(runID)
	}
}

// abandon stops the run once. An empty run id means the consumer vanished
// before the executing run existed (the resume window): execution is stopped
// now and the durable cancel is issued when the id is tracked.
func (g *a2aStreamGuard) abandon() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped || g.abandoned {
		return
	}
	g.abandoned = true
	if g.runID != "" {
		g.lateFired = true
	}
	g.onAbandon(g.runID)
}

// stop disarms the guard and waits for any in-flight abandonment, so no cancel
// write can outlive the handler.
func (g *a2aStreamGuard) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return
	}
	g.stopped = true
	close(g.stopCh)
}

// abandonedRunID reports whether the consumer went away and, if so, the run id
// that was executing when it did (empty on the resume window).
func (g *a2aStreamGuard) abandonedRunID() (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runID, g.abandoned
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

	// The executor runs under runCtx so an abandoned stream can stop it. The
	// request context alone was not enough: a run whose consumer vanished kept
	// executing to completion (issue #868).
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	writer := sse.NewWriter(c.Response().Writer)
	if err := writer.Start(); err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "SSE streaming not supported"))
	}
	defer writer.Close()

	tr := newA2aStreamTranslator(taskID, contextID)

	// guard cancels the run when its consumer goes away. A resume creates a
	// fresh child run, so no id is tracked until ExecuteRequest.OnRunCreated
	// (wired below) reports it: cancelling the paused checkpoint instead would
	// strand the resume. On the new-task path run.ID is already the executing
	// run.
	trackedRunID := run.ID
	if resume {
		trackedRunID = ""
	}
	guard := newA2AStreamGuard(trackedRunID, func(runID string) {
		// Record the durable cancel intent first: the executor observes it at
		// its next step boundary and finalizes the row as cancelled. Only then
		// cancel the local run context, so the terminal write cannot race the
		// intent and misreport an abandoned run as failed. The write is bounded
		// and detached from the request context, which may already be gone.
		if runID != "" {
			cancelCtx, stopCancel := context.WithTimeout(bgCtx, a2aAbandonTimeout)
			if _, err := h.repo.RequestRunCancellation(cancelCtx, runID); err != nil {
				h.log.Warn("failed to request cancel for abandoned a2a stream run",
					slog.String("run_id", runID),
					slog.String("task_id", taskID),
					slog.String("error", err.Error()),
				)
			}
			stopCancel()
		}
		h.log.Info("a2a stream consumer gone, stopping run",
			slog.String("task_id", taskID),
			slog.String("run_id", runID),
		)
		cancelRun()
	})
	// Registered after cancelRun so it runs before it: on a clean return the
	// guard is disarmed before the request context is cancelled.
	defer guard.stop()

	if ctx.Err() != nil {
		// The consumer was already gone before execution started: do not spend
		// a single step on it.
		guard.abandon()
	}
	go guard.watch(ctx)

	prevOnRunCreated := execReq.OnRunCreated
	execReq.OnRunCreated = func(runID string) {
		guard.trackRun(runID)
		if prevOnRunCreated != nil {
			prevOnRunCreated(runID)
		}
	}

	// write emits a StreamResponse to the wire. A failed write means the
	// consumer is gone, which is treated as a cancel. lifecycle persists + fans
	// out to the bus separately so the terminal multi-event sequence persists
	// once.
	write := func(sr StreamResponse) {
		if err := writer.WriteData(sr); err != nil {
			guard.abandon()
		}
	}
	lifecycle := func(eventType string) {
		h.persistA2AEvent(bgCtx, taskID, eventType, nil)
		h.emitA2ABus(ctx, projectID, taskID, eventType, nil)
	}

	// Initial task (SUBMITTED) on the new-task path only.
	if !resume {
		task := a2aInitialTask(run, taskID, contextID)
		write(StreamResponse{Task: &task})
		lifecycle(ACPEventRunCreated)
	}

	// Working transition.
	write(a2aStatusUpdate(taskID, contextID, TaskStateWorking, ""))
	lifecycle(ACPEventRunInProgress)

	// Translate + emit streaming deltas as they arrive, persisting each delta's
	// translated payload (and fanning it out to the bus) so a subscribe client
	// can replay the same artifactUpdate/message events.
	execReq.StreamCallback = func(ev StreamEvent) {
		resps := tr.translate(ev)
		for _, sr := range resps {
			write(sr)
		}
		if et, ok := a2aDeltaEventType(ev); ok {
			for _, sr := range resps {
				h.persistA2AEvent(bgCtx, taskID, et, a2aStreamPayload(sr))
				h.emitA2ABus(ctx, projectID, taskID, et, a2aStreamPayload(sr))
			}
			if len(resps) == 0 {
				// Thinking folds into final-message metadata and has no wire
				// member; persist it as a payload-less message.part for replay
				// consistency but do not fan it out to subscribers.
				h.persistA2AEvent(bgCtx, taskID, et, nil)
			}
		}
	}

	var result *ExecuteResult
	var execErr error
	if resume {
		result, execErr = h.executor.Resume(runCtx, run, execReq)
	} else {
		result, execErr = h.executor.ExecuteWithRun(runCtx, run, execReq)
	}
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if errors.Is(execErr, ErrRunAlreadyResumed) {
		// Another follow-up consumed the pause first and owns the task. The
		// stream has already started, so report it as a message and emit no
		// terminal lifecycle event: the concurrent resume is still working, so
		// marking this task failed (or completed) would be a lie (issue #887).
		//
		// ErrRunPauseNotResumable is deliberately NOT folded in here: no turn is
		// running in that case, so the stream must report the failure and close
		// the task out rather than telling the client to wait on a dead run
		// (issue #912). It falls through to the failure path below.
		write(a2aResumeConflictResponse(taskID, contextID))
		return nil
	}
	if execErr != nil {
		h.log.Error("a2a stream run failed", "task_id", taskID, "error", execErr.Error())
		// Defence in depth: if the switch was flipped after the pre-check, the
		// executor refused before touching the row, so fail it terminal rather
		// than leaving it `running` until the stale-run reaper.
		failRunIfExecutionDisabled(bgCtx, h.repo, run, execErr, h.log)
	}
	if abandonedRunID, abandoned := guard.abandonedRunID(); abandoned && abandonedRunID != "" {
		// The consumer went away. The durable cancel intent normally makes the
		// executor finalize the row as cancelled, but a run that stopped before
		// reaching the pipeline (or whose executor never observed the intent)
		// must not stay non-terminal. CancelRun is guarded, so a run that
		// already reached a terminal state is left untouched.
		if _, err := h.repo.CancelRun(bgCtx, abandonedRunID); err != nil {
			h.log.Warn("failed to finalize abandoned a2a stream run",
				slog.String("run_id", abandonedRunID),
				slog.String("task_id", taskID),
				slog.String("error", err.Error()),
			)
		}
	}

	h.streamTerminal(bgCtx, write, tr, projectID, taskID, result, execErr)
	return nil
}

// streamTerminal emits the terminal StreamResponse(s) for a finished run and
// lets streamRun's deferred Close end the stream.
func (h *A2AHandler) streamTerminal(
	ctx context.Context,
	write func(StreamResponse),
	tr *a2aStreamTranslator,
	projectID, taskID string,
	result *ExecuteResult,
	execErr error,
) {
	lifecycle := func(eventType string) {
		h.persistA2AEvent(ctx, taskID, eventType, nil)
		h.emitA2ABus(ctx, projectID, taskID, eventType, nil)
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
		// prompt in status.message and the pause-source discriminator in
		// metadata, then close the stream. The client resumes with a new
		// message:stream/message:send carrying the same taskId.
		prompt := ""
		var pausedRun *AgentRun
		questions, _ := h.repo.FindPendingQuestionsByRunID(ctx, runID)
		if len(questions) > 0 {
			prompt = questions[0].Question
		}
		if r, _ := h.repo.FindRunByID(ctx, runID); r != nil {
			pausedRun = r
		}
		for _, sr := range tr.inputRequiredEvents(prompt, pausedRun) {
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
