package acp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/cli/internal/idgen"
	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
)

// defaultMaxSessions is the strict upper bound on the number of sessions tracked
// in memory. A long-lived agent fronting many sessions (e.g. an IDE reusing one
// ACP process) would otherwise grow its session map without bound; once the cap
// is reached the least-recently-used idle session is evicted. An in-flight
// session is never evicted; if every session is in-flight, adding another is
// refused rather than growing the map (see newSession), so the bound holds
// exactly at this value.
const defaultMaxSessions = 256

// cancelTaskTimeout bounds the follow-up CancelTask call a session/cancel makes.
// That call is a best-effort notification of the server, so it must never delay
// the cancelled prompt reply: it runs on its own goroutine and is abandoned
// (logged) when it exceeds this budget.
const cancelTaskTimeout = 5 * time.Second

// Agent bridges ACP method calls to the Memory A2A surface. Each ACP session
// maps to a Memory agent (skill) turn; consecutive prompts in the same session
// thread conversation via the A2A contextId, and a paused (human-in-the-loop)
// task is resumed by threading its taskId on the next prompt.
type Agent struct {
	client  *a2a.Client
	skill   string // default Memory agent skill id (RFC1123 slug)
	version string // CLI version advertised on initialize
	modes   []Mode // selectable external agents; empty degrades to the default skill

	mu          sync.Mutex
	sessions    map[string]*session
	seq         uint64 // monotonic tick updated on each session use, for LRU
	maxSessions int    // bounded session map size; <= 0 disables eviction

	// errLog receives best-effort diagnostics (a failed follow-up CancelTask,
	// for instance) that must never reach the ACP wire on stdout. Run wires it
	// to the process stderr; a nil value discards them.
	errLog io.Writer

	// testHookAfterLookup, when non-nil, runs after the session lookup and
	// before the turn registration critical section. Test-only seam for
	// deterministically exercising the lookup/registration race.
	testHookAfterLookup func()
}

// promptTurn is one in-flight prompt turn. It carries the turn's cancel
// function plus the A2A task id observed on the stream, so a session/cancel can
// stop the Memory run itself and not just the local request (issue #868). The
// task id is written by the streaming goroutine and read by cancel, so both
// sides hold Agent.mu.
type promptTurn struct {
	cancel context.CancelFunc
	taskID string
}

// session is the per-session state tracked by the agent.
type session struct {
	skill         string // selected skill slug; defaults to the agent's default skill
	contextID     string
	pendingTaskID string // set when the last turn paused for HITL input
	// cancels holds one entry per in-flight prompt turn, keyed by the turn
	// generation. Tracking per turn (rather than a single slot) keeps every
	// concurrent turn cancellable: a cancel notification cancels them all and
	// each turn removes only its own entry when it ends, so none is orphaned.
	cancels map[uint64]*promptTurn
	// cancelledThrough is the highest turn generation for which a session/cancel
	// has been requested. A turn whose generation is <= cancelledThrough
	// resolves "cancelled" at registration instead of starting a backend turn.
	// The watermark is monotonic and generation-scoped: every new turn gets a
	// strictly greater generation, so a cancel can only ever affect turns that
	// already existed when it arrived and can never leak onto a later turn (the
	// sticky-flag bug fixed in #780). It needs no explicit reset — a later
	// generation simply exceeds it.
	cancelledThrough uint64
	turn             uint64 // monotonic turn generation; keys cancels
	lastUsed         uint64 // agent.seq value at the last use, for LRU eviction
	dropped          bool   // set once the session is deleted/closed/evicted; never cleared
}

// inFlight reports the number of prompt turns currently running for the session.
func (s *session) inFlight() int { return len(s.cancels) }

// NewAgent creates an ACP agent that fronts the given A2A client and targets
// the Memory agent identified by skill (its RFC1123 slug). modes is the list of
// selectable (external) agents advertised to clients; when empty it degrades to
// the default skill (see ensureDefaultMode).
func NewAgent(client *a2a.Client, skill, version string, modes []Mode) *Agent {
	return &Agent{
		client:      client,
		skill:       skill,
		version:     version,
		modes:       ensureDefaultMode(modes, skill),
		sessions:    make(map[string]*session),
		maxSessions: defaultMaxSessions,
	}
}

// ensureDefaultMode returns the advertised mode list. When the list is empty
// (card fetch failed or returned no external agents) it degrades to a single
// fallback mode for the configured default skill, so session/new always has a
// valid current mode. A non-empty list is trusted as-is: it comes from the
// external-only AgentCard, so the default skill appears only when it is
// genuinely external-reachable — an internal/project/stale default is never
// prepended (the server refuses non-external agents at the first prompt).
func ensureDefaultMode(modes []Mode, skill string) []Mode {
	if len(modes) == 0 {
		return []Mode{{ID: skill, Name: skill}}
	}
	return modes
}

// hasMode reports whether id is one of the advertised modes.
func (a *Agent) hasMode(id string) bool {
	for _, m := range a.modes {
		if m.ID == id {
			return true
		}
	}
	return false
}

// modeStateFor builds the session-modes payload for the given current skill.
func (a *Agent) modeStateFor(skill string) *SessionModeState {
	return &SessionModeState{CurrentModeID: skill, AvailableModes: a.modes}
}

// configOptionsFor builds the session config-options payload for the given
// current skill.
func (a *Agent) configOptionsFor(skill string) []ConfigOption {
	values := make([]ConfigOptionValue, 0, len(a.modes))
	for _, m := range a.modes {
		values = append(values, ConfigOptionValue{Value: m.ID, Name: m.Name, Description: m.Description})
	}
	return []ConfigOption{{
		ID:           agentConfigOptionID,
		Name:         "Agent",
		Description:  "Memory agent to converse with",
		Category:     "model",
		Type:         "select",
		CurrentValue: skill,
		Options:      values,
	}}
}

// agentConfigOptionID is the id of the single select config option the bridge
// advertises for agent selection.
const agentConfigOptionID = "agent"

// initialize answers the ACP initialize method.
func (a *Agent) initialize() any {
	return InitializeResponse{
		ProtocolVersion: ProtocolVersion,
		AgentCapabilities: AgentCapabilities{
			LoadSession: false,
			PromptCapabilities: PromptCapabilities{
				Image:           false,
				Audio:           false,
				EmbeddedContext: false,
			},
			MCPCapabilities: MCPCapabilities{
				HTTP: false,
				SSE:  false,
			},
			SessionCapabilities: SessionCapabilities{
				Delete: &struct{}{},
				Close:  &struct{}{},
			},
		},
		AgentInfo:   &AgentInfo{Name: "memory", Title: "Memory", Version: a.version},
		AuthMethods: []any{},
	}
}

// newSession answers the ACP session/new method by allocating a fresh session.
// It fails when the map is already at its cap and no idle session can be
// evicted, so the bound on tracked sessions is strict rather than best-effort.
func (a *Agent) newSession() (SessionNewResponse, error) {
	id := newID("sess")
	a.mu.Lock()
	defer a.mu.Unlock()
	a.evictLocked()
	if a.maxSessions > 0 && len(a.sessions) >= a.maxSessions {
		return SessionNewResponse{}, fmt.Errorf(
			"session limit reached: %d sessions tracked, every one with an in-flight turn", len(a.sessions))
	}
	a.seq++
	a.sessions[id] = &session{lastUsed: a.seq, skill: a.skill}
	return SessionNewResponse{
		SessionID:     id,
		Modes:         a.modeStateFor(a.skill),
		ConfigOptions: a.configOptionsFor(a.skill),
	}, nil
}

// evictLocked drops least-recently-used idle sessions until the map has room for
// one more entry. Sessions with an in-flight turn are never evicted. If every
// session is in-flight nothing is pruned and the caller refuses to add another,
// which is what keeps the cap strict (see newSession).
//
// A victim is chosen only among sessions with no in-flight turn, so by
// construction it holds no cancel functions to invoke; cancelling on drop is
// handled by dropSession (delete/close) and by the dropped barrier below, which
// makes a prompt that is between lookup and registration abort and cancel its own
// turn. A victim is marked dropped before removal so that prompt aborts instead
// of running a turn on a detached session.
//
// The caller must hold a.mu.
func (a *Agent) evictLocked() {
	if a.maxSessions <= 0 {
		return
	}
	for len(a.sessions) >= a.maxSessions {
		var victimID string
		var victim *session
		for id, s := range a.sessions {
			if s.inFlight() > 0 {
				continue
			}
			if victim == nil || s.lastUsed < victim.lastUsed {
				victim, victimID = s, id
			}
		}
		if victim == nil {
			// Every session has an in-flight turn; nothing can be pruned safely.
			return
		}
		delete(a.sessions, victimID)
		// Sticky barrier against the prompt lookup/registration window: a
		// prompt that already holds this pointer must never admit a turn.
		victim.dropped = true
	}
}

// dropSession removes a session from the map, marks it dropped (so a prompt in
// the lookup/registration window aborts) and cancels every in-flight turn.
// Removing an unknown session is a silent no-op, matching ACP semantics.
func (a *Agent) dropSession(sessionID string) {
	a.mu.Lock()
	sess := a.sessions[sessionID]
	if sess == nil {
		a.mu.Unlock()
		return
	}
	delete(a.sessions, sessionID)
	// Sticky barrier against the prompt lookup/registration window: a prompt
	// that has already looked this session up must not run a turn for it.
	sess.dropped = true
	turns := make([]turnCancel, 0, len(sess.cancels))
	for _, t := range sess.cancels {
		turns = append(turns, turnCancel{cancel: t.cancel, taskID: t.taskID})
	}
	a.mu.Unlock()
	// Invoke the cancel functions outside the lock, as cancel does.
	for _, t := range turns {
		a.stopTurn(t)
	}
}

// deleteSession handles the ACP session/delete method: drop the session's state
// and cancel every in-flight turn for it.
func (a *Agent) deleteSession(sessionID string) { a.dropSession(sessionID) }

// closeSession handles the ACP session/close method: cancel ongoing work for the
// session and free its resources. This agent is in-memory, persists nothing, and
// advertises loadSession=false, so closing and deleting are equivalent — both
// drop the tracked session — but close is kept as its own ACP method to match the
// capability-gated lifecycle.
func (a *Agent) closeSession(sessionID string) { a.dropSession(sessionID) }

// setMode changes the session's target agent to the given mode. It validates the
// mode against the advertised list; the updated mode state is delivered to the
// client via a current_mode_update notification emitted by the caller (the ACP
// session/set_mode response itself is an empty object).
func (a *Agent) setMode(p SetModeParams) error {
	if !a.hasMode(p.ModeID) {
		return fmt.Errorf("unknown mode %q", p.ModeID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	sess := a.sessions[p.SessionID]
	if sess == nil {
		return fmt.Errorf("session %q not found", p.SessionID)
	}
	sess.skill = p.ModeID
	return nil
}

// setConfigOption changes the session's target agent via the config-options
// surface. Only the single "agent" option is supported.
func (a *Agent) setConfigOption(p SetConfigOptionParams) ([]ConfigOption, error) {
	if p.ConfigID != agentConfigOptionID {
		return nil, fmt.Errorf("unknown config option %q", p.ConfigID)
	}
	if !a.hasMode(p.Value) {
		return nil, fmt.Errorf("unknown value %q for config option %q", p.Value, p.ConfigID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	sess := a.sessions[p.SessionID]
	if sess == nil {
		return nil, fmt.Errorf("session %q not found", p.SessionID)
	}
	sess.skill = p.Value
	return a.configOptionsFor(p.Value), nil
}

// prompt answers the ACP session/prompt method by running one Memory agent turn
// over A2A message:stream. It streams reply text via session/update
// agent_message_chunk notifications and returns stopReason "end_turn" (or
// "cancelled" when a session/cancel notification interrupted the turn).
func (a *Agent) prompt(ctx context.Context, p PromptParams, send func(any) error) (any, error) {
	text := extractText(p.Prompt)

	a.mu.Lock()
	sess := a.sessions[p.SessionID]
	if sess == nil {
		a.evictLocked()
		// Strict cap: if every tracked session is in-flight, refuse to grow the
		// map rather than exceed the bound. The idle case was already pruned by
		// evictLocked above. Capture the count while the lock is still held:
		// reading len(a.sessions) after unlocking would race a concurrent
		// session/new, delete, or lazy create mutating the map.
		if n := len(a.sessions); a.maxSessions > 0 && n >= a.maxSessions {
			a.mu.Unlock()
			return nil, fmt.Errorf(
				"session limit reached: %d sessions tracked, every one with an in-flight turn", n)
		}
		sess = &session{skill: a.skill}
		a.sessions[p.SessionID] = sess
	}
	a.seq++
	sess.lastUsed = a.seq
	resumeTaskID := sess.pendingTaskID
	contextID := sess.contextID
	skill := sess.skill
	hook := a.testHookAfterLookup
	a.mu.Unlock()

	if hook != nil {
		hook()
	}

	if text == "" {
		// Nothing to run and no paused task to resume.
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	}

	// Allocate a fresh cancellable context for this turn and register its cancel
	// function so a session/cancel notification can interrupt a blocking send.
	promptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.mu.Lock()
	sess.turn++
	turn := sess.turn
	if sess.cancels == nil {
		sess.cancels = make(map[uint64]*promptTurn)
	}
	rec := &promptTurn{cancel: cancel}
	sess.cancels[turn] = rec
	dropped := sess.dropped
	cancelled := turn <= sess.cancelledThrough
	if resumeTaskID != "" {
		// Consumed now; a subsequent pause re-sets it via consumeStream.
		sess.pendingTaskID = ""
	}
	a.mu.Unlock()

	// Remove this turn's cancel reference once it ends, so an idle session
	// carries no stale CancelFunc and per-turn entries never accumulate. Other
	// turns are left untouched: each owns (and removes) its own entry.
	defer func() {
		a.mu.Lock()
		delete(sess.cancels, turn)
		a.mu.Unlock()
	}()

	if dropped {
		// The session was deleted or evicted after this prompt looked it up but
		// before it registered its turn; do not run a turn on a detached session.
		cancel()
		return PromptResponse{StopReason: StopReasonCancelled}, nil
	}

	if cancelled {
		// A session/cancel arrived before this turn registered its cancel
		// function; honour it instead of starting an un-cancellable turn.
		cancel()
		return PromptResponse{StopReason: StopReasonCancelled}, nil
	}

	req := a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: newID("msg"),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.TextPart(text)},
		},
	}
	if resumeTaskID != "" {
		// Resume the paused task; the server infers the agent and context.
		req.Message.TaskID = resumeTaskID
	} else {
		req.Message.Metadata = map[string]any{a2a.SkillIDMetadataKey: skill}
		if contextID != "" {
			req.Message.ContextID = contextID
		}
	}

	stream, err := a.client.StreamMessage(promptCtx, req)
	if err != nil {
		if promptCtx.Err() != nil {
			return PromptResponse{StopReason: StopReasonCancelled}, nil
		}
		return nil, fmt.Errorf("memory: %w", err)
	}
	defer func() { _ = stream.Close() }()

	emitter := &sessionEmitter{sid: p.SessionID, send: send}
	newContextID, newPendingTaskID, stopReason, err := a.consumeStream(promptCtx, stream, emitter, func(taskID string) {
		a.mu.Lock()
		rec.taskID = taskID
		a.mu.Unlock()
	})
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	if newContextID != "" {
		sess.contextID = newContextID
	}
	if newPendingTaskID != "" {
		sess.pendingTaskID = newPendingTaskID
	}
	a.mu.Unlock()

	return PromptResponse{StopReason: stopReason}, nil
}

// consumeStream drains an A2A SSE stream, emitting session/update deltas as they
// arrive: agent_message_chunk for streamed text, tool_call/tool_call_update for
// the server's structured tool artifacts, and a single agent_thought_chunk on
// the turn's WORKING transition so the client shows progress before the first
// step produces output. It returns the accumulated contextId, any pending HITL
// task id, the stop reason, and an error (only for transport failures).
//
// track receives the A2A task id as soon as an event names it, and "" once the
// turn stops running (terminal or paused), so a session/cancel can cancel the
// right Memory run without cancelling a paused task the user is about to resume.
func (a *Agent) consumeStream(ctx context.Context, stream *a2a.SSEStream, emitter *sessionEmitter, track func(string)) (string, string, string, error) {
	var contextID, pendingTaskID string
	stopReason := StopReasonEndTurn

	// knownTaskID is the id already reported to track; it is cleared once the
	// turn stops running so a late session/cancel has nothing to cancel.
	var knownTaskID string
	noteTask := func(id string) {
		if id == "" || id == knownTaskID || track == nil {
			return
		}
		knownTaskID = id
		track(id)
	}
	clearTask := func() {
		if knownTaskID == "" || track == nil {
			return
		}
		knownTaskID = ""
		track("")
	}
	working := false

	for {
		ev, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return contextID, "", StopReasonCancelled, nil
			}
			return contextID, "", StopReasonEndTurn, fmt.Errorf("memory: stream: %w", err)
		}

		switch {
		case ev.ArtifactUpdate != nil:
			au := ev.ArtifactUpdate
			if au.ContextID != "" {
				contextID = au.ContextID
			}
			noteTask(au.TaskID)
			if tc, ok := toolCallFromArtifact(au); ok {
				if err := emitter.emitToolCall(tc); err != nil {
					return contextID, "", StopReasonEndTurn, err
				}
				continue
			}
			if err := emitter.emit(artifactText(au.Artifact)); err != nil {
				return contextID, "", StopReasonEndTurn, err
			}

		case ev.Message != nil:
			if ev.Message.ContextID != "" {
				contextID = ev.Message.ContextID
			}
			noteTask(ev.Message.TaskID)
			if err := emitter.emit(messageText(ev.Message)); err != nil {
				return contextID, "", StopReasonEndTurn, err
			}

		case ev.StatusUpdate != nil:
			su := ev.StatusUpdate
			if su.ContextID != "" {
				contextID = su.ContextID
			}
			noteTask(su.TaskID)
			switch su.Status.State {
			case a2a.TaskStateWorking:
				// The turn has started but has produced nothing yet: one
				// progress frame so a slow first step is not invisible.
				if !working {
					working = true
					if err := emitter.emitWorking(); err != nil {
						return contextID, "", stopReason, err
					}
				}
			case a2a.TaskStateInputRequired:
				// HITL pause: record the task id for a later resume and surface
				// the question. The server closes the stream after this event.
				// The run is no longer executing, so a cancel from here on must
				// not cancel it (that would strand the paused task).
				pendingTaskID = su.TaskID
				clearTask()
				if q := messageText(su.Status.Message); q != "" {
					if err := emitter.emit(q); err != nil {
						return contextID, "", stopReason, err
					}
				}
			case a2a.TaskStateFailed, a2a.TaskStateRejected:
				// A failed/rejected turn is not a clean end_turn; surface the
				// failure message and report refusal so clients can distinguish.
				stopReason = StopReasonRefusal
				clearTask()
				if msg := messageText(su.Status.Message); msg != "" {
					if err := emitter.emit(msg); err != nil {
						return contextID, "", stopReason, err
					}
				}
			case a2a.TaskStateCanceled:
				stopReason = StopReasonCancelled
				clearTask()
			}

		case ev.Task != nil:
			// Initial SUBMITTED snapshot or terminal snapshot. Its text is
			// already emitted via artifact/message events; capture context id
			// and any terminal failure/cancellation state.
			if ev.Task.ContextID != "" {
				contextID = ev.Task.ContextID
			}
			noteTask(ev.Task.ID)
			switch ev.Task.Status.State {
			case a2a.TaskStateFailed, a2a.TaskStateRejected:
				stopReason = StopReasonRefusal
				clearTask()
			case a2a.TaskStateCanceled:
				stopReason = StopReasonCancelled
				clearTask()
			}
		}
	}
	clearTask()

	if ctx.Err() != nil {
		return contextID, "", StopReasonCancelled, nil
	}
	return contextID, pendingTaskID, stopReason, nil
}

// turnCancel is a snapshot of an in-flight turn taken under Agent.mu: the local
// cancel function plus the Memory task id (empty when the stream has not named
// one yet, or the turn has already stopped running).
type turnCancel struct {
	cancel context.CancelFunc
	taskID string
}

// cancel handles the session/cancel notification by cancelling every in-flight
// prompt for the session, locally and on the server. The turns are copied under
// the lock so their cancel functions and task ids are never read concurrently
// with the writes in prompt and consumeStream. It also advances the
// cancelledThrough watermark so a turn that has looked the session up but not yet
// registered its cancel function still observes the cancel; the cancels are
// invoked outside the lock.
func (a *Agent) cancel(params CancelParams) {
	a.mu.Lock()
	sess := a.sessions[params.SessionID]
	var turns []turnCancel
	if sess != nil {
		turns = make([]turnCancel, 0, len(sess.cancels))
		for _, t := range sess.cancels {
			turns = append(turns, turnCancel{cancel: t.cancel, taskID: t.taskID})
		}
		// Record the cancel against the turn generation. With a turn in flight it
		// applies up to the newest registered generation; with none in flight it
		// applies to the next turn to register (a cancel racing a prompt's
		// lookup/registration window). The watermark only ever moves forward, so
		// a turn after the cancelled one always has a greater generation and is
		// never spuriously cancelled.
		target := sess.turn
		if len(sess.cancels) == 0 {
			target = sess.turn + 1
		}
		if target > sess.cancelledThrough {
			sess.cancelledThrough = target
		}
	}
	a.mu.Unlock()
	for _, t := range turns {
		a.stopTurn(t)
	}
}

// stopTurn stops one in-flight turn: it cancels the local stream request and, as
// a backstop, cancels the Memory run behind it. Cancelling only the local context
// left the run executing in its sandbox — 9 more minutes and 30 steps on the
// reported turn (issue #868) — because the server does not reliably observe a
// dropped SSE connection. CancelTask is a no-op for a task the server has already
// finalised, so it is safe to send even when the stream is about to end on its
// own.
func (a *Agent) stopTurn(t turnCancel) {
	t.cancel()
	if t.taskID == "" || a.client == nil {
		return
	}
	go func(taskID string) {
		ctx, cancel := context.WithTimeout(context.Background(), cancelTaskTimeout)
		defer cancel()
		if _, err := a.client.CancelTask(ctx, taskID); err != nil {
			a.logf("acp: session/cancel: cancel Memory task %s: %v", taskID, err)
		}
	}(t.taskID)
}

// logf writes a best-effort diagnostic to the process error log (stderr). The
// ACP wire is stdout only, so nothing diagnostic may be written there.
func (a *Agent) logf(format string, args ...any) {
	a.mu.Lock()
	w := a.errLog
	a.mu.Unlock()
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

// sessionEmitter emits ACP session/update notifications for one prompt turn. It
// dedupes incremental text deltas — A2A stream events carry the full growing text
// (and the terminal message/task repeat the final text), so it emits only the
// suffix not yet sent, falling back to the full text when a non-monotonic value
// arrives — and maps the server's structured tool artifacts onto ACP tool_call
// frames.
type sessionEmitter struct {
	sid     string
	send    func(any) error
	emitted string
}

func (e *sessionEmitter) emit(text string) error {
	if text == "" {
		return nil
	}
	switch {
	case e.emitted == "":
		e.emitted = text
		return e.send(sessionUpdate(e.sid, text))
	case text == e.emitted:
		return nil
	case strings.HasPrefix(text, e.emitted):
		chunk := text[len(e.emitted):]
		e.emitted = text
		return e.send(sessionUpdate(e.sid, chunk))
	default:
		e.emitted = text
		return e.send(sessionUpdate(e.sid, text))
	}
}

// emitToolCall reports one tool-call artifact to the client as a tool_call (on
// the start artifact) or tool_call_update (on the Append:true end artifact).
// Without this the bridge kept only text parts and dropped every tool artifact,
// so a 30-step turn was invisible until the final message (issue #866).
func (e *sessionEmitter) emitToolCall(tc toolCall) error {
	return e.send(toolCallFrame(e.sid, tc))
}

// emitWorking reports the turn's WORKING transition as a single thought chunk so
// the client shows the turn as started even when the first model step takes
// minutes to produce anything. It is sent once per turn (the server emits one
// WORKING status per stream).
func (e *sessionEmitter) emitWorking() error {
	return e.send(map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": e.sid,
			"update": map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"content":       map[string]any{"type": "text", "text": workingNotice},
			},
		},
	})
}

// workingNotice is the text of the single progress frame emitted on a turn's
// WORKING transition. It is a status line, not model output.
const workingNotice = "Working…"

// toolCall is one ACP tool call derived from an A2A tool artifact. The server
// emits the start artifact as a structured Data part {toolName, input} and the
// end artifact as {toolName, output} with Append:true under the same artifact id
// (apps/server/domain/agents/a2a_stream.go), so the artifact id is the stable
// ACP toolCallId.
type toolCall struct {
	ID        string
	Name      string
	Input     any
	HasInput  bool
	Output    any
	HasOutput bool
	End       bool
}

// toolCallFromArtifact extracts the tool-call payload from an artifact update.
// It reports false for anything that is not a tool artifact: the growing text
// artifact, A2UI surfaces, and any other structured data a future server version
// emits. Non-object Data parts (A2UI's array of surface messages) never match.
func toolCallFromArtifact(au *a2a.TaskArtifactUpdateEvent) (toolCall, bool) {
	if au == nil || au.Artifact.ArtifactID == "" {
		return toolCall{}, false
	}
	tc := toolCall{ID: au.Artifact.ArtifactID, Name: au.Artifact.Name, End: au.Append}
	for _, p := range au.Artifact.Parts {
		data, ok := p.Data.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := data["toolName"].(string); ok && name != "" {
			tc.Name = name
		}
		if v, ok := data["input"]; ok {
			tc.Input, tc.HasInput = v, true
		}
		if v, ok := data["output"]; ok {
			tc.Output, tc.HasOutput = v, true
		}
	}
	if tc.Name == "" && !tc.HasInput && !tc.HasOutput {
		return toolCall{}, false
	}
	if tc.Name == "" {
		tc.Name = "tool"
	}
	return tc, true
}

// toolCallFrame builds the ACP session/update for one tool artifact: a tool_call
// with status "pending" on the start artifact (the server emits it before
// executing the tool) and a tool_call_update with status "completed" carrying
// rawOutput on the end artifact.
func toolCallFrame(sessionID string, tc toolCall) any {
	update := map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    tc.ID,
		"title":         tc.Name,
		"kind":          toolKind(tc.Name),
		"status":        "pending",
	}
	if tc.End {
		update = map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    tc.ID,
			"status":        "completed",
		}
		if tc.HasOutput {
			update["rawOutput"] = tc.Output
		}
	} else if tc.HasInput {
		update["rawInput"] = tc.Input
	}
	return map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update":    update,
		},
	}
}

// toolKinds pins the ACP ToolKind for the workspace and web tools a Memory
// coding agent uses, so a client renders the right icon instead of "other". A
// tool added to the server without a row here still reaches the client: toolKind
// falls back to a keyword match, then to "other".
var toolKinds = map[string]string{
	"workspace_bash":     "execute",
	"workspace_git":      "execute",
	"run_python":         "execute",
	"run_go":             "execute",
	"workspace_read":     "read",
	"workspace_glob":     "search",
	"workspace_grep":     "search",
	"workspace_ast_grep": "search",
	"workspace_write":    "edit",
	"workspace_edit":     "edit",
	"web-fetch":          "fetch",
}

// toolKind maps a Memory tool name onto an ACP ToolKind. The explicit table wins,
// then a keyword match. The keyword order matters: the specific verbs are checked
// before "run_"/"git", which are substrings of names like `run_read` and would
// otherwise classify a read (or an edit) as shell execution.
func toolKind(name string) string {
	lower := strings.ToLower(name)
	if kind, ok := toolKinds[lower]; ok {
		return kind
	}
	switch {
	case hasAny(lower, "read", "cat", "view"):
		return "read"
	case hasAny(lower, "grep", "glob", "search", "find"):
		return "search"
	case hasAny(lower, "write", "edit", "patch", "replace"):
		return "edit"
	case hasAny(lower, "fetch", "download", "http"):
		return "fetch"
	case hasAny(lower, "bash", "shell", "exec", "command", "run_", "git"):
		return "execute"
	default:
		return "other"
	}
}

// hasAny reports whether s contains any of the substrings.
func hasAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// sessionUpdate builds a session/update agent_message_chunk notification.
func sessionUpdate(sessionID, text string) any {
	return map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": text},
			},
		},
	}
}

// currentModeUpdate builds a session/update current_mode_update notification.
// Mode state is delivered to clients via this notification; the session/set_mode
// response itself is an empty object per the ACP schema.
func currentModeUpdate(sessionID, modeID string) any {
	return map[string]any{
		"jsonrpc": jsonrpcVersion,
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "current_mode_update",
				"currentModeId": modeID,
			},
		},
	}
}

// extractText flattens a prompt's content blocks into a single text string.
// Text blocks are concatenated; resource_link URIs are appended as context.
func extractText(blocks []ContentBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "text":
			sb.WriteString(b.Text)
		case "resource_link":
			if b.URI != "" {
				if sb.Len() > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(b.URI)
			}
		}
	}
	return sb.String()
}

// messageText joins the text parts of an A2A message.
func messageText(m *a2a.Message) string {
	if m == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Text != nil {
			sb.WriteString(*p.Text)
		}
	}
	return sb.String()
}

// artifactText joins the text parts of an A2A artifact.
func artifactText(art a2a.Artifact) string {
	var sb strings.Builder
	for _, p := range art.Parts {
		if p.Text != nil {
			sb.WriteString(*p.Text)
		}
	}
	return sb.String()
}

// newID returns a random hex id with the given prefix. If the entropy source
// fails it falls back to a nanosecond timestamp, keeping ids unique rather
// than collapsing every id to the same constant.
func newID(prefix string) string {
	if h, ok := idgen.Hex(16); ok {
		return prefix + "-" + h
	}
	return idgen.FallbackID(prefix)
}
