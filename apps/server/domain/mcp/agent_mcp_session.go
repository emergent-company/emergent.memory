package mcp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// Session lifecycle constants
// ============================================================================

const (
	// agentMCPSessionDefaultTTL is the idle/lifetime TTL applied to a session at
	// creation. Sessions past this expiry are not continuable and are marked
	// expired by the reaper.
	agentMCPSessionDefaultTTL = 24 * time.Hour

	// agentMCPSessionMaxTurns caps how many turns a single session may run. A
	// turn beyond the cap is rejected without running.
	agentMCPSessionMaxTurns = 100

	// agentMCPSessionMaxTotalSteps caps the CUMULATIVE steps a session may
	// spend across all its turns. The per-turn budget (agentShareRunMaxSteps /
	// agentShareRunTimeout) still applies independently to each turn.
	agentMCPSessionMaxTotalSteps = 200

	// agentMCPSessionStuckRunTimeout is how long a session may sit in "running"
	// with no activity before a new turn may take it over. A run that crashed
	// leaves the row running; without this takeover the session would be stuck
	// forever. Takeover is done by the ClaimSession CAS, not by holding a lock
	// across the run.
	agentMCPSessionStuckRunTimeout = 5 * time.Minute
)

// AgentMCPSessionDTO is the session metadata surfaced by get_session and
// list_sessions. Message history is never exposed here.
type AgentMCPSessionDTO struct {
	SessionID    string    `json:"session_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	TurnCount    int       `json:"turn_count"`
}

func (s *AgentMCPSession) toToolDTO() AgentMCPSessionDTO {
	return AgentMCPSessionDTO{
		SessionID:    s.SessionRef,
		Status:       s.Status,
		CreatedAt:    s.CreatedAt,
		LastActiveAt: s.LastActiveAt,
		TurnCount:    s.TurnCount,
	}
}

// ============================================================================
// Store accessor
// ============================================================================

func (s *Service) agentSessionStore() agentMCPSessionStore {
	if s.agentSessions != nil {
		return s.agentSessions
	}
	if s.db != nil {
		return newAgentMCPSessionStore(s.db)
	}
	return nil
}

// ============================================================================
// Result helpers
// ============================================================================

// sessionEnvelope wraps envelopeResult so session tools can return the envelope
// directly as a ToolResult. Marshaling the envelope cannot realistically fail;
// the fallback keeps the contract even then.
func sessionEnvelope(ok bool, data any, meta map[string]any, errMsg string) *ToolResult {
	res, err := envelopeResult(ok, data, meta, errMsg)
	if err != nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: `{"ok":false,"data":null,"error":"failed to encode tool result"}`}},
			IsError: true,
		}
	}
	return res
}

// sessionFailure builds a failed envelope with a machine-readable kind.
func sessionFailure(kind, msg string) *ToolResult {
	return sessionEnvelope(false, nil, map[string]any{"kind": kind}, msg)
}

// runErrorKindAndMessage extracts the AgentRunError kind/message from a run
// failure, defaulting to run_failed for unknown errors.
func runErrorKindAndMessage(err error) (string, string) {
	if err == nil {
		return string(AgentRunErrorFailed), "agent run failed"
	}
	runErr := new(AgentRunError)
	if errors.As(err, &runErr) {
		kind := string(runErr.Kind)
		if kind == "" {
			kind = string(AgentRunErrorFailed)
		}
		msg := runErr.Message
		if msg == "" {
			msg = err.Error()
		}
		return kind, msg
	}
	return string(AgentRunErrorFailed), err.Error()
}

// loadOwnedSession resolves a session ref for the authorizing key. Sessions are
// key-scoped and are NOT capabilities: a ref belonging to another key resolves
// exactly like an unknown ref so its existence is never revealed. The endpoint
// check is redundant with key ownership but asserted defensively.
func (s *Service) loadOwnedSession(ctx context.Context, store agentMCPSessionStore, ep *AgentMCPEndpoint, key *AgentMCPKey, sessionRef string) (*AgentMCPSession, *ToolResult) {
	ref := strings.TrimSpace(sessionRef)
	if ref == "" {
		return nil, sessionFailure("invalid_params", "missing required argument: session_id")
	}
	row, err := store.GetSessionByRef(ctx, ref)
	if err != nil {
		return nil, sessionFailure("storage_error", "failed to load session")
	}
	if row == nil || row.KeyID != key.ID || row.EndpointID != ep.ID {
		return nil, sessionFailure("not_found", "session not found")
	}
	return row, nil
}

// ============================================================================
// Session tools
// ============================================================================

// StartSession creates a new session owned by the authorizing key. With a
// message it runs the first turn and returns the reply; without one it creates
// an empty session and returns only its id. The endpoint/key passed here were
// resolved by AuthorizeAgentEndpoint on this request.
func (s *Service) StartSession(ctx context.Context, ep *AgentMCPEndpoint, key *AgentMCPKey, message string) *ToolResult {
	store := s.agentSessionStore()
	if store == nil {
		return sessionFailure("storage_error", "session storage unavailable")
	}
	now := time.Now().UTC()
	expiresAt := now.Add(agentMCPSessionDefaultTTL)
	sess := &AgentMCPSession{
		ID:           uuid.NewString(),
		EndpointID:   ep.ID,
		KeyID:        key.ID,
		SessionRef:   uuid.NewString(),
		Status:       AgentMCPSessionStatusActive,
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    &expiresAt,
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		return sessionFailure("storage_error", err.Error())
	}
	if strings.TrimSpace(message) == "" {
		return sessionEnvelope(true, map[string]any{
			"session_id": sess.SessionRef,
			"status":     sess.Status,
		}, nil, "")
	}
	return s.runSessionTurn(ctx, store, ep, key, sess, message)
}

// ContinueSession runs a further turn on an existing session, appending to the
// prior conversation context via the shared ADK session key. The session must
// belong to the authorizing key and the endpoint's agent.
func (s *Service) ContinueSession(ctx context.Context, ep *AgentMCPEndpoint, key *AgentMCPKey, sessionRef, message string) *ToolResult {
	store := s.agentSessionStore()
	if store == nil {
		return sessionFailure("storage_error", "session storage unavailable")
	}
	if strings.TrimSpace(message) == "" {
		return sessionFailure("invalid_params", "missing required argument: message")
	}
	sess, fail := s.loadOwnedSession(ctx, store, ep, key, sessionRef)
	if fail != nil {
		return fail
	}
	return s.runSessionTurn(ctx, store, ep, key, sess, message)
}

// GetSession returns one session's metadata, scoped to the authorizing key.
func (s *Service) GetSession(ctx context.Context, ep *AgentMCPEndpoint, key *AgentMCPKey, sessionRef string) *ToolResult {
	store := s.agentSessionStore()
	if store == nil {
		return sessionFailure("storage_error", "session storage unavailable")
	}
	sess, fail := s.loadOwnedSession(ctx, store, ep, key, sessionRef)
	if fail != nil {
		return fail
	}
	return sessionEnvelope(true, sess.toToolDTO(), nil, "")
}

// ListSessions returns the sessions owned by the authorizing key only. It never
// starts a run.
func (s *Service) ListSessions(ctx context.Context, ep *AgentMCPEndpoint, key *AgentMCPKey) *ToolResult {
	store := s.agentSessionStore()
	if store == nil {
		return sessionFailure("storage_error", "session storage unavailable")
	}
	rows, err := store.ListSessionsByKey(ctx, key.ID)
	if err != nil {
		return sessionFailure("storage_error", "failed to list sessions")
	}
	out := make([]AgentMCPSessionDTO, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.EndpointID != ep.ID {
			continue
		}
		out = append(out, row.toToolDTO())
	}
	return sessionEnvelope(true, map[string]any{"sessions": out}, nil, "")
}

// runSessionTurn executes exactly one turn and owns the session's full
// lifecycle: expiry/turn/step caps, the per-session concurrency CAS, the run,
// and the completion/failure bookkeeping.
//
// Concurrency model: a session is serialized by an optimistic CAS on
// status='running' (ClaimSession), NOT by a pg_advisory_xact_lock held across
// the run (which would pin a transaction for up to the run timeout). A lost
// CAS is returned as ok:false, "session busy". A session left running by a
// crashed run is taken over once last_active_at is older than
// agentMCPSessionStuckRunTimeout.
func (s *Service) runSessionTurn(ctx context.Context, store agentMCPSessionStore, ep *AgentMCPEndpoint, key *AgentMCPKey, sess *AgentMCPSession, message string) *ToolResult {
	now := time.Now().UTC()

	// A session past its TTL is not continuable. Mark it expired so get_session
	// reports a non-active status even if the reaper has not run yet.
	if sess.ExpiresAt != nil && !sess.ExpiresAt.After(now) {
		_ = store.SetSessionStatus(context.Background(), sess.ID, AgentMCPSessionStatusExpired)
		return sessionFailure("not_active", "session has expired")
	}
	if sess.TurnCount >= agentMCPSessionMaxTurns {
		return sessionFailure(string(AgentRunErrorBudget), "session turn limit reached")
	}
	if sess.TotalSteps >= agentMCPSessionMaxTotalSteps {
		return sessionFailure(string(AgentRunErrorBudget), "session step budget exceeded")
	}

	// Optimistic CAS: exactly one concurrent turn wins the session. A stale
	// "running" row (crashed run) is taken over after the stuck-run timeout.
	claimed, err := store.ClaimSession(ctx, sess.ID, now, now.Add(-agentMCPSessionStuckRunTimeout))
	if err != nil {
		return sessionFailure("storage_error", "failed to claim session")
	}
	if !claimed {
		return sessionEnvelope(false, nil, map[string]any{"kind": "session_busy"}, "session busy")
	}

	// The turn may outlive the request context (e.g. client disconnect), so the
	// terminal bookkeeping runs on a background context.
	status := AgentMCPSessionStatusActive
	defer func() {
		_ = store.ReleaseSession(context.Background(), sess.ID, status, time.Now().UTC())
	}()

	if s.agentToolHandler == nil {
		return sessionEnvelope(false, nil, map[string]any{"kind": string(AgentRunErrorUnavailable)},
			"agent execution is unavailable")
	}

	reply, runID, steps, runErr := s.agentToolHandler.RunAgentInSession(
		ctx, ep.ProjectID, ep.AgentID, sess.SessionRef, message,
		AgentRunBudget{MaxSteps: agentShareRunMaxSteps, Timeout: agentShareRunTimeout},
	)

	meta := map[string]any{"steps": steps}
	if runID != "" {
		meta["run_id"] = runID
	}

	var runIDPtr *string
	if runID != "" {
		runIDPtr = &runID
	}

	finishedAt := time.Now().UTC()
	_ = store.TouchSession(context.Background(), sess.ID, steps, runIDPtr, finishedAt)

	// A canceled turn is reported as interrupted and never as a success.
	if ctx.Err() != nil {
		status = AgentMCPSessionStatusInterrupted
		kind, _ := runErrorKindAndMessage(runErr)
		if runErr == nil {
			kind = string(AgentRunErrorFailed)
		}
		meta["kind"] = kind
		return sessionEnvelope(false, nil, meta, "turn was interrupted")
	}

	if runErr != nil {
		kind, msg := runErrorKindAndMessage(runErr)
		meta["kind"] = kind
		return sessionEnvelope(false, nil, meta, msg)
	}

	return sessionEnvelope(true, map[string]any{
		"session_id": sess.SessionRef,
		"reply":      reply,
		"run_id":     runID,
		"status":     AgentMCPSessionStatusActive,
	}, meta, "")
}
