package acp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

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

// Agent bridges ACP method calls to the Memory A2A surface. Each ACP session
// maps to a Memory agent (skill) turn; consecutive prompts in the same session
// thread conversation via the A2A contextId, and a paused (human-in-the-loop)
// task is resumed by threading its taskId on the next prompt.
type Agent struct {
	client  *a2a.Client
	skill   string // default Memory agent skill id (RFC1123 slug)
	version string // CLI version advertised on initialize
	modes   []Mode // selectable agents; always contains the default skill

	mu          sync.Mutex
	sessions    map[string]*session
	seq         uint64 // monotonic tick updated on each session use, for LRU
	maxSessions int    // bounded session map size; <= 0 disables eviction

	// testHookAfterLookup, when non-nil, runs after the session lookup and
	// before the turn registration critical section. Test-only seam for
	// deterministically exercising the lookup/registration race.
	testHookAfterLookup func()
}

// session is the per-session state tracked by the agent.
type session struct {
	skill         string // selected skill slug; defaults to the agent's default skill
	contextID     string
	pendingTaskID string // set when the last turn paused for HITL input
	// cancels holds one cancel function per in-flight prompt turn, keyed by the
	// turn generation. Tracking per turn (rather than a single slot) keeps every
	// concurrent turn cancellable: a cancel notification cancels them all and
	// each turn removes only its own entry when it ends, so none is orphaned.
	cancels map[uint64]context.CancelFunc
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
// selectable agents advertised to clients; the default skill is always present.
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

// ensureDefaultMode guarantees the default skill is present in the mode list so
// that session/new always advertises at least one mode (the configured agent).
func ensureDefaultMode(modes []Mode, skill string) []Mode {
	for _, m := range modes {
		if m.ID == skill {
			return modes
		}
	}
	return append([]Mode{{ID: skill, Name: skill}}, modes...)
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
	fns := make([]context.CancelFunc, 0, len(sess.cancels))
	for _, fn := range sess.cancels {
		fns = append(fns, fn)
	}
	a.mu.Unlock()
	// Invoke the cancel functions outside the lock, as cancel does.
	for _, fn := range fns {
		fn()
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
// mode against the advertised list and returns the updated mode state.
func (a *Agent) setMode(p SetModeParams) (*SessionModeState, error) {
	if !a.hasMode(p.ModeID) {
		return nil, fmt.Errorf("unknown mode %q", p.ModeID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	sess := a.sessions[p.SessionID]
	if sess == nil {
		return nil, fmt.Errorf("session %q not found", p.SessionID)
	}
	sess.skill = p.ModeID
	return a.modeStateFor(p.ModeID), nil
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
		sess.cancels = make(map[uint64]context.CancelFunc)
	}
	sess.cancels[turn] = cancel
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

	emitter := &textEmitter{sid: p.SessionID, send: send}
	newContextID, newPendingTaskID, stopReason, err := a.consumeStream(promptCtx, stream, emitter)
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

// consumeStream drains an A2A SSE stream, emitting agent_message_chunk deltas
// for streamed text and terminal question/error messages. It returns the
// accumulated contextId, any pending HITL task id, the stop reason, and an error
// (only for transport failures).
func (a *Agent) consumeStream(ctx context.Context, stream *a2a.SSEStream, emitter *textEmitter) (string, string, string, error) {
	var contextID, pendingTaskID string
	stopReason := StopReasonEndTurn

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
			if ev.ArtifactUpdate.ContextID != "" {
				contextID = ev.ArtifactUpdate.ContextID
			}
			if err := emitter.emit(artifactText(ev.ArtifactUpdate.Artifact)); err != nil {
				return contextID, "", StopReasonEndTurn, err
			}

		case ev.Message != nil:
			if ev.Message.ContextID != "" {
				contextID = ev.Message.ContextID
			}
			if err := emitter.emit(messageText(ev.Message)); err != nil {
				return contextID, "", StopReasonEndTurn, err
			}

		case ev.StatusUpdate != nil:
			su := ev.StatusUpdate
			if su.ContextID != "" {
				contextID = su.ContextID
			}
			switch su.Status.State {
			case a2a.TaskStateInputRequired:
				// HITL pause: record the task id for a later resume and surface
				// the question. The server closes the stream after this event.
				pendingTaskID = su.TaskID
				if q := messageText(su.Status.Message); q != "" {
					if err := emitter.emit(q); err != nil {
						return contextID, "", stopReason, err
					}
				}
			case a2a.TaskStateFailed, a2a.TaskStateRejected:
				// A failed/rejected turn is not a clean end_turn; surface the
				// failure message and report refusal so clients can distinguish.
				stopReason = StopReasonRefusal
				if msg := messageText(su.Status.Message); msg != "" {
					if err := emitter.emit(msg); err != nil {
						return contextID, "", stopReason, err
					}
				}
			case a2a.TaskStateCanceled:
				stopReason = StopReasonCancelled
			}

		case ev.Task != nil:
			// Initial SUBMITTED snapshot or terminal snapshot. Its text is
			// already emitted via artifact/message events; capture context id
			// and any terminal failure/cancellation state.
			if ev.Task.ContextID != "" {
				contextID = ev.Task.ContextID
			}
			switch ev.Task.Status.State {
			case a2a.TaskStateFailed, a2a.TaskStateRejected:
				stopReason = StopReasonRefusal
			case a2a.TaskStateCanceled:
				stopReason = StopReasonCancelled
			}
		}
	}

	if ctx.Err() != nil {
		return contextID, "", StopReasonCancelled, nil
	}
	return contextID, pendingTaskID, stopReason, nil
}

// cancel handles the session/cancel notification by cancelling every in-flight
// prompt for the session. The cancel functions are copied under the lock so they
// are never read concurrently with the writes in prompt. It also advances the
// cancelledThrough watermark so a turn that has looked the session up but not yet
// registered its cancel function still observes the cancel; the cancel functions
// are invoked outside the lock.
func (a *Agent) cancel(params CancelParams) {
	a.mu.Lock()
	sess := a.sessions[params.SessionID]
	var fns []context.CancelFunc
	if sess != nil {
		fns = make([]context.CancelFunc, 0, len(sess.cancels))
		for _, fn := range sess.cancels {
			fns = append(fns, fn)
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
	for _, fn := range fns {
		fn()
	}
}

// textEmitter dedupes and emits incremental text deltas. A2A stream events carry
// the full growing text (and the terminal message/task repeat the final text),
// so it emits only the suffix not yet sent, falling back to the full text when a
// non-monotonic value arrives.
type textEmitter struct {
	sid     string
	send    func(any) error
	emitted string
}

func (e *textEmitter) emit(text string) error {
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
