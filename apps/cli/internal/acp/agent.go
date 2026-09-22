package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
)

// defaultMaxSessions bounds the number of sessions tracked in memory. A
// long-lived agent fronting many sessions (e.g. an IDE reusing one ACP process)
// would otherwise grow its session map without bound; once the cap is reached
// the least-recently-used idle session is evicted.
const defaultMaxSessions = 256

// Agent bridges ACP method calls to the Memory A2A surface. Each ACP session
// maps to a Memory agent (skill) turn; consecutive prompts in the same session
// thread conversation via the A2A contextId, and a paused (human-in-the-loop)
// task is resumed by threading its taskId on the next prompt.
type Agent struct {
	client  *a2a.Client
	skill   string // Memory agent skill id (RFC1123 slug)
	version string // CLI version advertised on initialize

	mu          sync.Mutex
	sessions    map[string]*session
	seq         uint64 // monotonic tick updated on each session use, for LRU
	maxSessions int    // bounded session map size; <= 0 disables eviction
}

// session is the per-session state tracked by the agent.
type session struct {
	contextID     string
	pendingTaskID string // set when the last turn paused for HITL input
	cancel        context.CancelFunc
	cancelled     bool   // set by session/cancel; cleared when the next turn starts
	turn          uint64 // generation of the turn that owns cancel
	inFlight      int    // number of prompt turns currently running
	lastUsed      uint64 // agent.seq value at the last use, for LRU eviction
}

// NewAgent creates an ACP agent that fronts the given A2A client and targets
// the Memory agent identified by skill (its RFC1123 slug).
func NewAgent(client *a2a.Client, skill, version string) *Agent {
	return &Agent{
		client:      client,
		skill:       skill,
		version:     version,
		sessions:    make(map[string]*session),
		maxSessions: defaultMaxSessions,
	}
}

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
			},
		},
		AgentInfo:   &AgentInfo{Name: "memory", Title: "Memory", Version: a.version},
		AuthMethods: []any{},
	}
}

// newSession answers the ACP session/new method by allocating a fresh session.
func (a *Agent) newSession() any {
	id := newID("sess")
	a.mu.Lock()
	a.evictLocked()
	a.seq++
	a.sessions[id] = &session{lastUsed: a.seq}
	a.mu.Unlock()
	return SessionNewResponse{SessionID: id}
}

// evictLocked drops least-recently-used idle sessions until the map has room for
// one more entry. Sessions with an in-flight turn are never evicted, so the map
// may briefly exceed maxSessions when that many turns run concurrently. Any
// cancel function on an evicted session is invoked (context cancel funcs are
// non-blocking and never re-enter the agent, so calling them under the lock is
// safe) so a dropped turn cannot leak its goroutine.
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
			if s.inFlight > 0 {
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
		if victim.cancel != nil {
			victim.cancel()
		}
	}
}

// deleteSession handles the ACP session/delete method by removing the session's
// state and cancelling any in-flight turn for it. Deleting an unknown session is
// a no-op so the method succeeds silently, matching ACP delete semantics.
func (a *Agent) deleteSession(sessionID string) {
	a.mu.Lock()
	sess := a.sessions[sessionID]
	if sess == nil {
		a.mu.Unlock()
		return
	}
	delete(a.sessions, sessionID)
	fn := sess.cancel
	a.mu.Unlock()
	// Invoke the cancel function outside the lock, as cancel does.
	if fn != nil {
		fn()
	}
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
		sess = &session{}
		a.sessions[p.SessionID] = sess
	}
	a.seq++
	sess.lastUsed = a.seq
	resumeTaskID := sess.pendingTaskID
	contextID := sess.contextID
	a.mu.Unlock()

	if text == "" {
		// Nothing to run and no paused task to resume.
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	}

	// Allocate a fresh cancellable context for this turn and record its cancel
	// function so a session/cancel notification can interrupt a blocking send.
	promptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.mu.Lock()
	sess.turn++
	turn := sess.turn
	sess.cancel = cancel
	sess.inFlight++
	cancelled := sess.cancelled
	sess.cancelled = false // clear for this turn; a mid-turn cancel re-sets it
	if resumeTaskID != "" {
		// Consumed now; a subsequent pause re-sets it via consumeStream.
		sess.pendingTaskID = ""
	}
	a.mu.Unlock()

	// Mark the turn finished and drop the cancel reference once it ends, so an
	// idle session carries no stale CancelFunc. A later turn that already
	// replaced cancel is left untouched (its own generation owns it).
	defer func() {
		a.mu.Lock()
		sess.inFlight--
		if sess.turn == turn {
			sess.cancel = nil
		}
		a.mu.Unlock()
	}()

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
		req.Message.Metadata = map[string]any{a2a.SkillIDMetadataKey: a.skill}
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

// cancel handles the session/cancel notification by cancelling any in-flight
// prompt for the session. The cancel function is copied under the lock so it is
// never read concurrently with the write in prompt, and a cancelled flag is
// recorded so a cancel arriving before the turn registers still takes effect.
func (a *Agent) cancel(params CancelParams) {
	a.mu.Lock()
	sess := a.sessions[params.SessionID]
	var fn context.CancelFunc
	if sess != nil {
		sess.cancelled = true
		fn = sess.cancel
	}
	a.mu.Unlock()
	if fn != nil {
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

// newID returns a random hex id with the given prefix.
func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, len(b))
	}
	return prefix + "-" + hex.EncodeToString(b)
}
