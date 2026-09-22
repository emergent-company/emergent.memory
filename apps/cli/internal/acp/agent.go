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

// Agent bridges ACP method calls to the Memory A2A surface. Each ACP session
// maps to a Memory agent (skill) turn; consecutive prompts in the same session
// thread conversation via the A2A contextId, and a paused (human-in-the-loop)
// task is resumed by threading its taskId on the next prompt.
type Agent struct {
	client  *a2a.Client
	skill   string // Memory agent skill id (RFC1123 slug)
	version string // CLI version advertised on initialize

	mu       sync.Mutex
	sessions map[string]*session
}

// session is the per-session state tracked by the agent.
type session struct {
	contextID     string
	pendingTaskID string // set when the last turn paused for HITL input
	cancel        context.CancelFunc
}

// NewAgent creates an ACP agent that fronts the given A2A client and targets
// the Memory agent identified by skill (its RFC1123 slug).
func NewAgent(client *a2a.Client, skill, version string) *Agent {
	return &Agent{
		client:   client,
		skill:    skill,
		version:  version,
		sessions: make(map[string]*session),
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
		},
		AgentInfo:   &AgentInfo{Name: "memory", Title: "Memory", Version: a.version},
		AuthMethods: []any{},
	}
}

// newSession answers the ACP session/new method by allocating a fresh session.
func (a *Agent) newSession() any {
	id := newID("sess")
	a.mu.Lock()
	a.sessions[id] = &session{}
	a.mu.Unlock()
	return SessionNewResponse{SessionID: id}
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
		sess = &session{}
		a.sessions[p.SessionID] = sess
	}
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
	sess.cancel = cancel
	if resumeTaskID != "" {
		// Consumed now; a subsequent pause re-sets it via consumeStream.
		sess.pendingTaskID = ""
	}
	a.mu.Unlock()

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
						return contextID, "", StopReasonEndTurn, err
					}
				}
			case a2a.TaskStateFailed:
				if msg := messageText(su.Status.Message); msg != "" {
					if err := emitter.emit(msg); err != nil {
						return contextID, "", StopReasonEndTurn, err
					}
				}
			}

		case ev.Task != nil:
			// Initial SUBMITTED snapshot or terminal COMPLETED snapshot. Its text
			// is already emitted via artifact/message events; capture context id.
			if ev.Task.ContextID != "" {
				contextID = ev.Task.ContextID
			}
		}
	}

	if ctx.Err() != nil {
		return contextID, "", StopReasonCancelled, nil
	}
	return contextID, pendingTaskID, StopReasonEndTurn, nil
}

// cancel handles the session/cancel notification by cancelling any in-flight
// prompt for the session.
func (a *Agent) cancel(params CancelParams) {
	a.mu.Lock()
	sess := a.sessions[params.SessionID]
	a.mu.Unlock()
	if sess != nil && sess.cancel != nil {
		sess.cancel()
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
