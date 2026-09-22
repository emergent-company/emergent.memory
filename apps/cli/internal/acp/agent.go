package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/a2a"
)

// Agent bridges ACP method calls to the Memory A2A surface. Each ACP session
// maps to a Memory agent (skill) turn; consecutive prompts in the same session
// thread conversation via the A2A contextId.
type Agent struct {
	client  *a2a.Client
	skill   string // Memory agent skill id (RFC1123 slug)
	version string // CLI version advertised on initialize

	mu       sync.Mutex
	sessions map[string]*session
}

// session is the per-session state tracked by the agent.
type session struct {
	contextID string
	cancel    context.CancelFunc
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

// prompt answers the ACP session/prompt method by running one Memory agent
// turn. It emits a session/update agent_message_chunk notification carrying the
// reply text via send, then returns stopReason "end_turn" (or "cancelled").
func (a *Agent) prompt(ctx context.Context, p PromptParams, send func(any) error) (any, error) {
	text := extractText(p.Prompt)
	if text == "" {
		// Nothing to run; end the turn without a backend call.
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	}

	// Allocate a fresh cancellable context for this turn and record its cancel
	// function so a session/cancel notification can interrupt a blocking send.
	promptCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.mu.Lock()
	sess := a.sessions[p.SessionID]
	if sess == nil {
		sess = &session{}
		a.sessions[p.SessionID] = sess
	}
	sess.cancel = cancel
	contextID := sess.contextID
	a.mu.Unlock()

	req := a2a.SendMessageRequest{
		Message: a2a.Message{
			MessageID: newID("msg"),
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{a2a.TextPart(text)},
			Metadata:  map[string]any{a2a.SkillIDMetadataKey: a.skill},
		},
	}
	if contextID != "" {
		req.Message.ContextID = contextID
	}

	resp, err := a.client.SendMessage(promptCtx, req)
	if err != nil {
		if promptCtx.Err() != nil {
			return PromptResponse{StopReason: StopReasonCancelled}, nil
		}
		return nil, fmt.Errorf("memory: %w", err)
	}

	reply, newContextID, state := a.replyFromResponse(resp)

	if newContextID != "" {
		a.mu.Lock()
		sess.contextID = newContextID
		a.mu.Unlock()
	}

	switch state {
	case a2a.TaskStateFailed:
		if reply == "" {
			reply = "agent task failed"
		}
		if err := send(sessionUpdate(p.SessionID, reply)); err != nil {
			return nil, err
		}
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	case a2a.TaskStateInputRequired:
		// Human-in-the-loop is not resolvable over a stateless stdio turn; surface
		// the agent's question as the reply and end the turn.
		if reply != "" {
			if err := send(sessionUpdate(p.SessionID, reply)); err != nil {
				return nil, err
			}
		}
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	default:
		if reply != "" {
			if err := send(sessionUpdate(p.SessionID, reply)); err != nil {
				return nil, err
			}
		}
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	}
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

// replyFromResponse extracts the reply text, the new context id, and the
// terminal task state from an A2A SendMessageResponse.
func (a *Agent) replyFromResponse(resp *a2a.SendMessageResponse) (text, contextID string, state a2a.TaskState) {
	if resp.Task != nil {
		contextID = resp.Task.ContextID
		state = resp.Task.Status.State
		if state == a2a.TaskStateInputRequired || state == a2a.TaskStateFailed {
			text = messageText(resp.Task.Status.Message)
		} else {
			text = taskText(resp.Task)
		}
		return text, contextID, state
	}
	if resp.Message != nil {
		contextID = resp.Message.ContextID
		text = messageText(resp.Message)
	}
	return text, contextID, state
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

// taskText joins the text parts of a task's artifacts.
func taskText(t *a2a.Task) string {
	var sb strings.Builder
	for _, art := range t.Artifacts {
		for _, p := range art.Parts {
			if p.Text != nil {
				sb.WriteString(*p.Text)
			}
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
