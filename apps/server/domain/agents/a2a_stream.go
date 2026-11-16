package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

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
//
// It is NOT safe for concurrent use: ADK emits tool callbacks from parallel
// goroutines. Drive it through a2aStreamSink, which serialises the callback
// body (translate + write + persist).
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

	// toolSeq/toolArtByCall give each tool-call trajectory a stable artifactId
	// so a call's start and end deltas accumulate onto the same artifact.
	// ADK runs the function calls of one model response concurrently, so starts
	// and ends interleave: the end must be matched to its own call by the ADK
	// function-call id (StreamEvent.ID), never to whichever call started last.
	// toolArtID is the most recent assignment, used only when the id is empty.
	toolSeq       int
	toolArtID     string
	toolArtByCall map[string]string
}

// newA2aStreamTranslator builds a translator for a task/context pair.
func newA2aStreamTranslator(taskID, contextID string) *a2aStreamTranslator {
	return &a2aStreamTranslator{
		taskID:    taskID,
		contextID: contextID,
		textArtID: "artifact-" + taskID,
	}
}

// a2aStreamSink serialises the streaming callback body. The translator is NOT
// safe for concurrent use, and ADK calls it concurrently: the function calls of
// one model response fan out through platform.RunTasks (one goroutine per call),
// so both tool callbacks reach StreamCallback at once. Without this lock the
// shared toolSeq++ can hand two starts the same "tool-call-N", the toolArtByCall
// map write can panic the process ("concurrent map writes" is a fatal error, not
// a recoverable race), and the SSE writes interleave.
//
// The mutex makes one event atomic: translate, write, and persist/fan-out happen
// in event order. Event order between goroutines is still whichever arrives
// first, which is all the wire needs.
type a2aStreamSink struct {
	mu      sync.Mutex
	tr      *a2aStreamTranslator
	write   func(...StreamResponse)
	onDelta func(ev StreamEvent, resps []StreamResponse) // optional; nil skips persistence
}

// handle translates and emits one stream event. Safe for concurrent use.
func (s *a2aStreamSink) handle(ev StreamEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resps := s.tr.translate(ev)
	if s.write != nil {
		s.write(resps...)
	}
	if s.onDelta != nil {
		s.onDelta(ev, resps)
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
		t.toolSeq++
		t.toolArtID = fmt.Sprintf("tool-call-%d", t.toolSeq)
		if ev.ID != "" {
			if t.toolArtByCall == nil {
				t.toolArtByCall = make(map[string]string)
			}
			t.toolArtByCall[ev.ID] = t.toolArtID
		}
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
		// Match the end to its own start by call id. Parallel calls finish out
		// of order, so the last-started artifact is usually the wrong one.
		artID := t.toolArtID
		if ev.ID != "" {
			if id, ok := t.toolArtByCall[ev.ID]; ok {
				artID = id
				delete(t.toolArtByCall, ev.ID)
			}
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

// abandonedStreamCancelReason is recorded on a run whose A2A stream consumer went
// away mid-turn: the SSE connection failed to write, or the request context was
// cancelled while the executor was still running. It is a cancel, not a failure —
// the work is not wrong, nobody is listening for it (issue #868).
const abandonedStreamCancelReason = "Run cancelled: the A2A stream consumer disconnected"

// a2aStreamTarget tracks the run that currently owns the streamed turn so a dead
// connection cancels the row that is actually executing. On a new task that is
// the run this handler created; on a resume the executor creates a child run and
// the paused checkpoint it resumes from is not what carries the work, so the id
// is updated from ExecuteRequest.OnRunCreated.
type a2aStreamTarget struct {
	mu      sync.Mutex
	id      string
	aborted bool
}

// newA2AStreamTarget tracks runID as the executing run.
func newA2AStreamTarget(runID string) *a2aStreamTarget {
	return &a2aStreamTarget{id: runID}
}

// track records the id of the run now executing this turn and reports whether the
// stream has ALREADY been abandoned, so the caller cancels this run instead of
// leaving it to run for a consumer that is gone. On the resume path the abort can
// land between the WORKING write and the child insert, when the tracked id is
// still the paused checkpoint: that cancel is refused (a paused row must never be
// flipped to cancelling), and without this second chance the child — created a
// moment later — would execute the whole turn for nobody. An empty id is ignored
// so a caller cannot erase a known target.
func (t *a2aStreamTarget) track(runID string) (aborted bool) {
	if runID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.id = runID
	return t.aborted
}

// markAborted reports that the stream is abandoned (exactly once, from the
// handler's abandon guard) and returns the id tracked at that moment. A track
// call that arrives afterwards sees aborted=true and cancels its own run.
func (t *a2aStreamTarget) markAborted() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.aborted = true
	return t.id
}

// a2aStreamWriter writes translated StreamResponses to the SSE connection and
// reports the first failure. A failed write means the consumer is gone, so the
// handler treats it as a cancel instead of writing into a dead connection for the
// rest of the turn — the discarded-error shape that let a run keep stepping for
// nine minutes in issue #868.
type a2aStreamWriter struct {
	writer  *sse.Writer
	onAbort func() // called at most once, on the first failed write
	aborted atomic.Bool
}

// newA2AStreamWriter wraps the SSE writer.
func newA2AStreamWriter(w *sse.Writer) *a2aStreamWriter {
	return &a2aStreamWriter{writer: w}
}

// write emits one StreamResponse and returns the write error so callers stop
// writing into a dead connection. The first failure also invokes onAbort.
func (s *a2aStreamWriter) write(sr StreamResponse) error {
	if err := s.writer.WriteData(sr); err != nil {
		if s.aborted.CompareAndSwap(false, true) && s.onAbort != nil {
			s.onAbort()
		}
		return err
	}
	return nil
}

// writeAll emits a translated sequence in order, stopping at the first failed
// write. The error is consumed here (not discarded): a dead connection has
// already triggered the cancel, and every later write would fail the same way.
func (s *a2aStreamWriter) writeAll(resps []StreamResponse) {
	for _, sr := range resps {
		if err := s.write(sr); err != nil {
			return
		}
	}
}

// trackStreamRun records runID as the run executing this stream and, when the
// stream has already been abandoned, cancels it immediately. This closes the
// resume window: the abort can land between the WORKING frame and the child run
// insert, when the tracked id is still the paused checkpoint — a cancel the
// repository refuses on purpose (issue #867) — so the child would otherwise
// execute a whole turn for a consumer that is gone (issue #868).
func (h *A2AHandler) trackStreamRun(ctx context.Context, target *a2aStreamTarget, runID string) {
	if target.track(runID) {
		h.cancelAbandonedStream(ctx, runID)
	}
}

// cancelAbandonedStream cancels the run behind a stream whose consumer went away.
// It records the durable cancel intent (issue #1166) and notifies the local
// executor — the same two steps an explicit A2A CancelTask takes — so the run
// stops at its next step boundary instead of running to completion for a client
// that is no longer reading (issue #868).
//
// The transition only applies while the run is still doing work: a paused run has
// already stopped and is waiting for a resume, and cancelling it would strand the
// task the user is about to continue (issue #867).
func (h *A2AHandler) cancelAbandonedStream(ctx context.Context, runID string) {
	if runID == "" {
		return
	}
	if h.repo != nil {
		accepted, err := h.repo.RequestRunCancellationIfActive(ctx, runID)
		if err != nil {
			if h.log != nil {
				h.log.Warn("failed to record a cancel for an abandoned A2A stream",
					slog.String("run_id", runID),
					slog.String("error", err.Error()),
				)
			}
			return
		}
		if !accepted {
			// Already terminal or paused: nothing is executing to cancel.
			return
		}
	}
	if h.executor != nil {
		h.executor.Cancel(runID, abandonedStreamCancelReason)
	}
}

// releaseResumeClaim releases a checkpoint that a resume consumed but never
// turned into a child run, so a failed follow-up is not silently dropped against
// a row that execution will never finalize. A plain claim is restored to the
// pause so the follow-up is retryable; one carrying a cancel landed in the claim
// window is finalized as cancelled instead. See
// Repository.ReleasePausedRunClaim for the guard; when the child does exist this
// is a no-op.
func (h *A2AHandler) releaseResumeClaim(ctx context.Context, run *AgentRun) {
	if h.repo == nil || run == nil || run.ID == "" {
		return
	}
	released, err := h.repo.ReleasePausedRunClaim(ctx, run.ID)
	if err != nil {
		h.log.Error("failed to release paused run claim",
			"run_id", run.ID, "error", err.Error())
		return
	}
	if released {
		h.log.Warn("resume failed before creating a child run; released the checkpoint claim",
			"run_id", run.ID)
	}
}

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
	}

	triggerSource := "a2a"
	run, err := h.repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:        agent.ID,
		TriggerSource:  &triggerSource,
		TriggerMessage: &userMessage,
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

	questions, err := h.repo.FindPendingQuestionsByRunID(ctx, latest.ID)
	if err != nil {
		h.log.Error("failed to load pending questions", "task_id", taskID, "error", err)
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to load pending questions"))
	}
	// A paused run with no pending question is a continuation, not a question:
	// a step-limit or watchdog pause leaves no question row, and the client has
	// already latched the task id from the INPUT_REQUIRED status, so rejecting
	// the follow-up would strand the task (issue #867).
	var question *AgentQuestion
	if len(questions) > 0 {
		question = questions[0]
	}

	// Kill switch: check BEFORE claiming the pending question, otherwise a
	// disabled resume consumes the question and permanently strands the task
	// (same guarantee as the JSON /message path).
	if h.executor != nil {
		if err := h.executor.EnsureExecutionEnabled(); err != nil {
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, err.Error()))
		}
	}

	if question != nil {
		claimed, err := h.repo.AnswerQuestion(ctx, question.ID, userMessage, userID)
		if err != nil {
			h.log.Error("failed to answer question", "task_id", taskID, "error", err)
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to answer question"))
		}
		if !claimed {
			return writeA2AError(c, a2aInvalidStateError("question is no longer pending"))
		}
	} else {
		// The questionless continuation has no question row to claim, so claim the
		// pause itself: two followers (a client retry, a double submit) would
		// otherwise both create a resumed child run and execute the turn twice.
		// The claim stays non-terminal ("working") until Resume's finalize, so a
		// cancel landing in the window still wins (the A2A lane's contract; other
		// resume entry points claim terminally instead — issue #887).
		claimed, err := h.repo.BeginA2AResumeClaim(ctx, latest.ID)
		if err != nil {
			h.log.Error("failed to claim paused run", "task_id", taskID, "error", err)
			return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to claim paused run"))
		}
		if !claimed {
			return writeA2AError(c, a2aInvalidStateError("task is no longer awaiting input (already resumed)"))
		}
	}

	resumeMsg := a2aResumeMessage(question, userMessage)

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
		// The questionless branch claimed the pause into "working"; Resume must
		// commit that claim (FinalizeResumedCheckpoint) instead of claiming
		// again, which a "working" row would reject (issue #887).
		PauseClaimed: question == nil,
		// TrustedInternal defaults false: an A2A surface must not reach internal agents.
		RenderA2UI: true, // A2A stream renders A2UI surfaces (artifactUpdate)
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

	writer := newA2AStreamWriter(sse.NewWriter(c.Response().Writer))
	if err := writer.writer.Start(); err != nil {
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "SSE streaming not supported"))
	}
	defer writer.writer.Close()

	// A consumer that disconnects is a cancel, not a slow client: the connection
	// closing cancels the request context, and a failed write means the
	// connection is already dead. Either way the run must stop, so both paths
	// funnel through cancelAbandonedStream exactly once.
	target := newA2AStreamTarget(run.ID)
	var abandonOnce sync.Once
	abandon := func() {
		abandonOnce.Do(func() { h.cancelAbandonedStream(bgCtx, target.markAborted()) })
	}
	writer.onAbort = abandon

	streamDone := make(chan struct{})
	defer close(streamDone)
	go func() {
		select {
		case <-ctx.Done():
			abandon()
		case <-streamDone:
		}
	}()

	tr := newA2aStreamTranslator(taskID, contextID)

	// write emits translated responses to the wire. lifecycle persists + fans out
	// to the bus separately so the terminal multi-event sequence persists once.
	write := func(resps ...StreamResponse) {
		writer.writeAll(resps)
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

	// Track the run that actually executes this turn: on a resume the executor
	// creates a child run, so a cancel aimed at run.ID would hit the paused
	// checkpoint instead of the work in flight. If the stream was already
	// abandoned before that child existed, track reports it and the child is
	// cancelled here rather than executing a whole turn for a dead consumer.
	execReq.OnRunCreated = func(runID string) { h.trackStreamRun(bgCtx, target, runID) }

	// Translate + emit streaming deltas as they arrive, persisting each delta's
	// translated payload (and fanning it out to the bus) so a subscribe client
	// can replay the same artifactUpdate/message events. The sink serialises the
	// whole body: ADK emits tool callbacks from parallel goroutines.
	sink := &a2aStreamSink{
		tr:    tr,
		write: write,
		onDelta: func(ev StreamEvent, resps []StreamResponse) {
			et, ok := a2aDeltaEventType(ev)
			if !ok {
				return
			}
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
		},
	}
	execReq.StreamCallback = sink.handle

	var result *ExecuteResult
	var execErr error
	if resume {
		if h.resumeRun != nil {
			result, execErr = h.resumeRun(ctx, run, execReq)
		} else {
			result, execErr = h.executor.Resume(ctx, run, execReq)
		}
	} else {
		result, execErr = h.executor.ExecuteWithRun(ctx, run, execReq)
	}
	if result != nil && result.Cleanup != nil {
		defer result.Cleanup()
	}
	if execErr != nil {
		h.log.Error("a2a stream run failed", "task_id", taskID, "error", execErr.Error())
		if resume {
			// The continuation claims the pause before Resume runs. If Resume
			// failed before creating a child run, the claim owns a row that no
			// execution will ever finalize; restore the pause so the follow-up
			// is retryable and GetTask stops reporting live work. Once the child
			// exists this is a no-op and the child carries the failure.
			h.releaseResumeClaim(bgCtx, run)
		} else {
			// Defence in depth: if the switch was flipped after the pre-check, the
			// executor refused before touching the row, so fail it terminal rather
			// than leaving it `running` until the stale-run reaper.
			failRunIfExecutionDisabled(bgCtx, h.repo, run, execErr, h.log)
		}
	}

	h.streamTerminal(bgCtx, writer, tr, projectID, taskID, result, execErr)
	return nil
}

// streamTerminal emits the terminal StreamResponse(s) for a finished run and
// lets streamRun's deferred Close end the stream.
func (h *A2AHandler) streamTerminal(
	ctx context.Context,
	writer *a2aStreamWriter,
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
		writer.writeAll(tr.failedEvents(execErr.Error()))
		lifecycle(ACPEventRunFailed)
		return
	}
	if result == nil {
		writer.writeAll(tr.failedEvents("run produced no result"))
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
		writer.writeAll(tr.inputRequiredEvents(prompt, pausedRun))
		lifecycle(ACPEventRunAwaiting)

	case RunStatusError:
		errMsg := "unknown error"
		if errRun, _ := h.repo.FindRunByID(ctx, runID); errRun != nil && errRun.ErrorMessage != nil {
			errMsg = *errRun.ErrorMessage
		}
		writer.writeAll(tr.failedEvents(errMsg))
		lifecycle(ACPEventRunFailed)

	case RunStatusCancelled:
		writer.writeAll(tr.cancelledEvents())
		lifecycle(ACPEventRunCancelled)

	default: // Success / Skipped → COMPLETED
		latest, messages, question, a2aErr := h.loadA2ATask(ctx, projectID, taskID)
		if a2aErr != nil {
			latest = nil
		}
		writer.writeAll(tr.completedEvents(messages, latest, question))
		lifecycle(ACPEventRunCompleted)
	}
}
