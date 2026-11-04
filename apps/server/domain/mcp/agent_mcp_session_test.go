package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// In-memory session store
// ============================================================================

// fakeSessionStore is an in-memory agentMCPSessionStore. It implements the
// ClaimSession CAS under a mutex so concurrency tests exercise the same
// serialization contract as Postgres.
type fakeSessionStore struct {
	mu   sync.Mutex
	byID map[string]*AgentMCPSession
	// labels maps a key id to its label, standing in for the join the real
	// ListSessionsByEndpoint performs.
	labels map[string]string
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{byID: map[string]*AgentMCPSession{}, labels: map[string]string{}}
}

func (f *fakeSessionStore) byRef(ref string) *AgentMCPSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getByRefLocked(ref)
}

// onlyRef returns the session ref when exactly one session exists, else "".
func (f *fakeSessionStore) onlyRef(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.byID, 1)
	for _, s := range f.byID {
		return s.SessionRef
	}
	return ""
}

func (f *fakeSessionStore) getByRefLocked(ref string) *AgentMCPSession {
	for _, s := range f.byID {
		if s.SessionRef == ref {
			return s
		}
	}
	return nil
}

func (f *fakeSessionStore) CreateSession(_ context.Context, session *AgentMCPSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getByRefLocked(session.SessionRef) != nil {
		return errors.New("session ref exists")
	}
	if session.Status == "" {
		session.Status = AgentMCPSessionStatusActive
	}
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.LastActiveAt.IsZero() {
		session.LastActiveAt = now
	}
	f.byID[session.ID] = session
	return nil
}

func (f *fakeSessionStore) GetSessionByRef(_ context.Context, sessionRef string) (*AgentMCPSession, error) {
	return f.byRef(sessionRef), nil
}

func (f *fakeSessionStore) ListSessionsByKey(_ context.Context, keyID string) ([]*AgentMCPSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*AgentMCPSession{}
	for _, s := range f.byID {
		if s.KeyID == keyID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeSessionStore) ListSessionsByEndpoint(_ context.Context, endpointID, status string) ([]*AgentMCPSessionDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*AgentMCPSessionDetail{}
	for _, s := range f.byID {
		if s.EndpointID != endpointID {
			continue
		}
		if status != "" && s.Status != status {
			continue
		}
		out = append(out, &AgentMCPSessionDetail{
			ID:           s.ID,
			EndpointID:   s.EndpointID,
			KeyID:        s.KeyID,
			SessionRef:   s.SessionRef,
			Status:       s.Status,
			TurnCount:    s.TurnCount,
			TotalSteps:   s.TotalSteps,
			CreatedAt:    s.CreatedAt,
			LastActiveAt: s.LastActiveAt,
			ExpiresAt:    s.ExpiresAt,
			KeyLabel:     f.labels[s.KeyID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActiveAt.After(out[j].LastActiveAt) })
	return out, nil
}

func (f *fakeSessionStore) SetSessionStatus(_ context.Context, id, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.byID[id]; s != nil {
		s.Status = status
	}
	return nil
}

func (f *fakeSessionStore) TouchSession(_ context.Context, id string, steps int, lastRunID *string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.byID[id]; s != nil {
		s.TurnCount++
		s.TotalSteps += steps
		s.LastRunID = lastRunID
		s.LastActiveAt = at
	}
	return nil
}

func (f *fakeSessionStore) ClaimSession(_ context.Context, id string, at, staleBefore time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.byID[id]
	if s == nil {
		return false, nil
	}
	switch s.Status {
	case AgentMCPSessionStatusActive, AgentMCPSessionStatusInterrupted:
	case AgentMCPSessionStatusRunning:
		if !s.LastActiveAt.Before(staleBefore) {
			return false, nil
		}
	default:
		return false, nil
	}
	s.Status = AgentMCPSessionStatusRunning
	s.LastActiveAt = at
	return true, nil
}

func (f *fakeSessionStore) ReleaseSession(_ context.Context, id, status string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.byID[id]; s != nil {
		s.Status = status
		s.LastActiveAt = at
	}
	return nil
}

func (f *fakeSessionStore) MarkExpiredSessions(_ context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.byID {
		if s.Status == AgentMCPSessionStatusExpired {
			continue
		}
		if s.ExpiresAt != nil && !s.ExpiresAt.After(now) {
			s.Status = AgentMCPSessionStatusExpired
			n++
		}
	}
	return n, nil
}

// ============================================================================
// Helpers
// ============================================================================

func envelopeData(t *testing.T, res *ToolResult) map[string]any {
	t.Helper()
	m := decodeEnvelope(t, res, nil)
	data, ok := m["data"].(map[string]any)
	require.True(t, ok, "data must be an object")
	return data
}

func envelopeMeta(t *testing.T, res *ToolResult) map[string]any {
	t.Helper()
	m := decodeEnvelope(t, res, nil)
	meta, _ := m["meta"].(map[string]any)
	return meta
}

func envelopeIsOK(t *testing.T, res *ToolResult) bool {
	t.Helper()
	m := decodeEnvelope(t, res, nil)
	ok, _ := m["ok"].(bool)
	return ok
}

func (f *endpointTestFixture) ep() *AgentMCPEndpoint { return f.endpoints.byID["ep-1"] }
func (f *endpointTestFixture) key() *AgentMCPKey     { return f.keys.byID["key-1"] }

// ============================================================================
// start_session
// ============================================================================

func TestStartSessionWithoutMessageCreatesEmptySession(t *testing.T) {
	f := newEndpointTestFixture()
	res := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	require.True(t, envelopeIsOK(t, res))
	data := envelopeData(t, res)
	ref, _ := data["session_id"].(string)
	assert.NotEmpty(t, ref)
	_, hasReply := data["reply"]
	assert.False(t, hasReply, "empty start must not report a reply")
	assert.False(t, f.handler.runCalled, "no turn runs without a message")

	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	assert.Equal(t, AgentMCPSessionStatusActive, sess.Status)
	assert.NotNil(t, sess.ExpiresAt, "a TTL is set at creation")
}

func TestStartSessionWithMessageRunsTurnOne(t *testing.T) {
	f := newEndpointTestFixture()
	res := f.svc.StartSession(context.Background(), f.ep(), f.key(), "hello")
	require.True(t, envelopeIsOK(t, res))
	data := envelopeData(t, res)
	assert.Equal(t, "the reply", data["reply"])
	assert.Equal(t, "run-1", data["run_id"])
	assert.Equal(t, AgentMCPSessionStatusActive, data["status"])
	ref, _ := data["session_id"].(string)
	require.NotEmpty(t, ref)

	meta := envelopeMeta(t, res)
	assert.Equal(t, float64(1), meta["steps"])
	assert.Equal(t, "run-1", meta["run_id"])
	assert.True(t, f.handler.runCalled)

	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	assert.Equal(t, 1, sess.TurnCount)
	assert.Equal(t, 1, sess.TotalSteps)
	require.NotNil(t, sess.LastRunID)
	assert.Equal(t, "run-1", *sess.LastRunID)
}

func TestStartSessionIdsAreAlwaysNew(t *testing.T) {
	f := newEndpointTestFixture()
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		res := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
		ref := envelopeData(t, res)["session_id"].(string)
		require.False(t, seen[ref], "each start returns a distinct session id")
		seen[ref] = true
	}
}

// ============================================================================
// continue_session
// ============================================================================

func TestContinueSessionSharesContext(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "first message")
	ref := envelopeData(t, start)["session_id"].(string)

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "second message")
	require.True(t, envelopeIsOK(t, res))
	data := envelopeData(t, res)
	assert.Equal(t, ref, data["session_id"])
	assert.Equal(t, "the reply", data["reply"])

	// Both turns were dispatched with the SAME session ref (one ADK session key
	// => the executor loads/trims/compresses the prior turn into context) and the
	// messages in order.
	require.Len(t, f.handler.runSessionRefs, 2)
	assert.Equal(t, f.handler.runSessionRefs[0], f.handler.runSessionRefs[1])
	assert.Equal(t, ref, f.handler.runSessionRefs[1])
	assert.Equal(t, []string{"first message", "second message"}, f.handler.runMessages)

	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	assert.Equal(t, 2, sess.TurnCount)
	assert.Equal(t, 2, sess.TotalSteps)
}

func TestContinueSessionUnknownRejected(t *testing.T) {
	f := newEndpointTestFixture()
	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), "does-not-exist", "hi")
	require.False(t, envelopeIsOK(t, res))
	assert.False(t, f.handler.runCalled, "no run for an unknown session")
}

func TestContinueSessionMissingMessageRejected(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)
	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "   ")
	require.False(t, envelopeIsOK(t, res))
	assert.False(t, f.handler.runCalled)
}

// ============================================================================
// key scoping — session ids are not capabilities
// ============================================================================

func TestSessionToolsRejectOtherKeysSession(t *testing.T) {
	f := newEndpointTestFixture()
	key2 := &AgentMCPKey{ID: "key-2", EndpointID: "ep-1", TokenID: "tok-2", Label: "other",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	f.keys.byID["key-2"] = key2

	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "owned by key-1")
	ref := envelopeData(t, start)["session_id"].(string)

	get := f.svc.GetSession(context.Background(), f.ep(), key2, ref)
	assert.False(t, envelopeIsOK(t, get), "other key's session must not resolve")

	cont := f.svc.ContinueSession(context.Background(), f.ep(), key2, ref, "steal")
	assert.False(t, envelopeIsOK(t, cont))
	assert.Equal(t, 1, f.handler.runCount, "key-2 must not start a run on key-1's session")

	list := f.svc.ListSessions(context.Background(), f.ep(), key2)
	data := envelopeData(t, list)
	sessions, _ := data["sessions"].([]any)
	assert.Empty(t, sessions, "list must not expose another key's sessions")
}

func TestListSessionsReturnsOnlyOwnKeysSessions(t *testing.T) {
	f := newEndpointTestFixture()
	key2 := &AgentMCPKey{ID: "key-2", EndpointID: "ep-1", TokenID: "tok-2", Label: "other",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	f.keys.byID["key-2"] = key2

	ref1 := envelopeData(t, f.svc.StartSession(context.Background(), f.ep(), f.key(), ""))["session_id"].(string)
	_ = f.svc.StartSession(context.Background(), f.ep(), key2, "")

	list := f.svc.ListSessions(context.Background(), f.ep(), f.key())
	sessions, _ := envelopeData(t, list)["sessions"].([]any)
	require.Len(t, sessions, 1)
	assert.Equal(t, ref1, sessions[0].(map[string]any)["session_id"])
}

// ============================================================================
// counters on success AND failure
// ============================================================================

func TestSessionCountersRecordedOnFailure(t *testing.T) {
	f := newEndpointTestFixture()
	f.handler.runErr = &AgentRunError{Kind: AgentRunErrorFailed, Message: "boom", RunID: "run-1"}

	res := f.svc.StartSession(context.Background(), f.ep(), f.key(), "hi")
	require.False(t, envelopeIsOK(t, res))
	m := decodeEnvelope(t, res, nil)
	assert.Equal(t, "boom", m["error"])
	meta := m["meta"].(map[string]any)
	assert.Equal(t, string(AgentRunErrorFailed), meta["kind"])

	sess := f.sessions.byRef(f.sessions.onlyRef(t))
	require.NotNil(t, sess)
	assert.Equal(t, 1, sess.TurnCount, "failed turns still count")
	assert.Equal(t, AgentMCPSessionStatusActive, sess.Status, "returned to active after failure")
}

// ============================================================================
// budgets and lifecycle
// ============================================================================

func TestSessionCumulativeStepCapReturnsBudgetExceeded(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)

	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	sess.TotalSteps = agentMCPSessionMaxTotalSteps
	runsBefore := f.handler.runCount

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "more work")
	require.False(t, envelopeIsOK(t, res))
	m := decodeEnvelope(t, res, nil)
	assert.NotEmpty(t, m["error"])
	meta, _ := m["meta"].(map[string]any)
	require.NotNil(t, meta)
	assert.Equal(t, string(AgentRunErrorBudget), meta["kind"])
	assert.Equal(t, runsBefore, f.handler.runCount, "no run starts once the step budget is spent")
}

func TestSessionTurnCapRejectsWithoutRunning(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)

	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	sess.TurnCount = agentMCPSessionMaxTurns

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "one more")
	require.False(t, envelopeIsOK(t, res))
	meta, _ := decodeEnvelope(t, res, nil)["meta"].(map[string]any)
	assert.Equal(t, string(AgentRunErrorBudget), meta["kind"])
	assert.False(t, f.handler.runCalled)
}

func TestExpiredSessionIsNotContinuable(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)

	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	past := time.Now().UTC().Add(-time.Minute)
	sess.ExpiresAt = &past

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "too late")
	require.False(t, envelopeIsOK(t, res))
	assert.False(t, f.handler.runCalled)
	assert.Equal(t, AgentMCPSessionStatusExpired, sess.Status, "expired session is marked")

	// get_session reports the non-active status.
	get := f.svc.GetSession(context.Background(), f.ep(), f.key(), ref)
	assert.Equal(t, AgentMCPSessionStatusExpired, envelopeData(t, get)["status"])
}

func TestSessionReaperMarksExpired(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)
	sess := f.sessions.byRef(ref)
	past := time.Now().UTC().Add(-time.Hour)
	sess.ExpiresAt = &past

	reaper := NewAgentMCPSessionReaper(f.svc, slog.Default())
	reaper.reap(context.Background())

	assert.Equal(t, AgentMCPSessionStatusExpired, sess.Status)

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "nope")
	assert.False(t, envelopeIsOK(t, res), "reaped sessions are not continuable")
}

// ============================================================================
// cancellation
// ============================================================================

func TestCanceledTurnIsInterruptedAndResumable(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)

	f.handler.runBlock = make(chan struct{})
	f.handler.runStarted = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan *ToolResult, 1)
	go func() { done <- f.svc.ContinueSession(ctx, f.ep(), f.key(), ref, "long turn") }()
	<-f.handler.runStarted
	cancel()
	res := <-done

	require.False(t, envelopeIsOK(t, res), "a canceled turn is not a success")
	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	assert.Equal(t, AgentMCPSessionStatusInterrupted, sess.Status)

	get := f.svc.GetSession(context.Background(), f.ep(), f.key(), ref)
	assert.Equal(t, AgentMCPSessionStatusInterrupted, envelopeData(t, get)["status"])

	// An interrupted session can be resumed by continuing.
	f.handler.runBlock = nil
	f.handler.runStarted = nil
	resume := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "resume")
	require.True(t, envelopeIsOK(t, resume))
	assert.Equal(t, AgentMCPSessionStatusActive, f.sessions.byRef(ref).Status)
}

// ============================================================================
// concurrency (highest-severity risk)
// ============================================================================

func TestConcurrentContinueYieldsOneBusy(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)

	f.handler.runBlock = make(chan struct{})
	f.handler.runStarted = make(chan struct{}, 2)

	results := make(chan *ToolResult, 2)
	go func() { results <- f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "a") }()
	<-f.handler.runStarted // first turn claimed the session and is running
	go func() { results <- f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "b") }()

	busy := <-results
	require.False(t, envelopeIsOK(t, busy))
	m := decodeEnvelope(t, busy, nil)
	assert.Equal(t, "session busy", m["error"])
	meta, _ := m["meta"].(map[string]any)
	assert.Equal(t, "session_busy", meta["kind"])

	close(f.handler.runBlock)
	won := <-results
	require.True(t, envelopeIsOK(t, won))

	// Only the winner mutated the session: exactly one turn recorded.
	assert.Equal(t, 1, f.handler.runCount)
	sess := f.sessions.byRef(ref)
	require.NotNil(t, sess)
	assert.Equal(t, 1, sess.TurnCount)
	assert.Equal(t, AgentMCPSessionStatusActive, sess.Status)
}

func TestFreshRunningSessionIsBusy(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)
	sess := f.sessions.byRef(ref)
	sess.Status = AgentMCPSessionStatusRunning
	sess.LastActiveAt = time.Now().UTC()

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "x")
	require.False(t, envelopeIsOK(t, res))
	assert.Equal(t, "session busy", decodeEnvelope(t, res, nil)["error"])
	assert.False(t, f.handler.runCalled)
}

func TestStuckRunningSessionIsTakenOver(t *testing.T) {
	f := newEndpointTestFixture()
	start := f.svc.StartSession(context.Background(), f.ep(), f.key(), "")
	ref := envelopeData(t, start)["session_id"].(string)
	sess := f.sessions.byRef(ref)
	sess.Status = AgentMCPSessionStatusRunning
	sess.LastActiveAt = time.Now().UTC().Add(-agentMCPSessionStuckRunTimeout - time.Minute)

	res := f.svc.ContinueSession(context.Background(), f.ep(), f.key(), ref, "takeover")
	require.True(t, envelopeIsOK(t, res), "a crashed run must not strand the session")
	assert.True(t, f.handler.runCalled)
}

// ============================================================================
// handler-level domain e2e (tasks 7.4 / 7.5)
// ============================================================================

// TestAgentEndpointSessionEndToEnd drives the full agent-endpoint surface through
// the HTTP handler: five-tool list, bare call_agent, start/continue sharing one
// ADK session, get/list reflection, and key revocation rejection.
func TestAgentEndpointSessionEndToEnd(t *testing.T) {
	f := newEndpointTestFixture()
	f.handler.runReply = "agent says hi"
	ep := NewAgentEndpointHandler(f.svc, slog.Default())

	// 1. tools/list shows exactly the fixed five-tool catalog.
	c, rec := agentEndpointRequest(t, "tools/list", nil, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	tools := listedToolNames(t, rec)
	assert.ElementsMatch(t, []string{
		agentCallToolName, agentStartSessionToolName, agentContinueSessionToolName,
		agentGetSessionToolName, agentListSessionsToolName,
	}, tools)
	assert.NotContains(t, rec.Body.String(), "entity-search")

	// 2. call_agent returns the bare text reply (no envelope).
	c, rec = agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentCallToolName, Arguments: map[string]any{"message": "ping"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.Equal(t, "agent says hi", toolCallText(t, rec))

	// 3. start_session then continue_session share context (one ADK session key).
	c, rec = agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentStartSessionToolName, Arguments: map[string]any{"message": "turn one"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	startEnv := toolCallEnvelope(t, rec)
	require.True(t, startEnv["ok"].(bool))
	ref := startEnv["data"].(map[string]any)["session_id"].(string)
	require.NotEmpty(t, ref)

	c, rec = agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentContinueSessionToolName, Arguments: map[string]any{"session_id": ref, "message": "turn two"}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	contEnv := toolCallEnvelope(t, rec)
	require.True(t, contEnv["ok"].(bool))
	assert.Equal(t, ref, contEnv["data"].(map[string]any)["session_id"])
	require.Len(t, f.handler.runSessionRefs, 2)
	assert.Equal(t, f.handler.runSessionRefs[0], f.handler.runSessionRefs[1])
	assert.Equal(t, []string{"turn one", "turn two"}, f.handler.runMessages)

	// 4. get_session / list_sessions reflect the session.
	c, rec = agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentGetSessionToolName, Arguments: map[string]any{"session_id": ref}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	getEnv := toolCallEnvelope(t, rec)
	require.True(t, getEnv["ok"].(bool))
	assert.Equal(t, float64(2), getEnv["data"].(map[string]any)["turn_count"])

	c, rec = agentEndpointRequest(t, "tools/call", ToolsCallParams{Name: agentListSessionsToolName, Arguments: map[string]any{}}, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	listEnv := toolCallEnvelope(t, rec)
	require.True(t, listEnv["ok"].(bool))
	sessions := listEnv["data"].(map[string]any)["sessions"].([]any)
	require.Len(t, sessions, 1)
	assert.Equal(t, ref, sessions[0].(map[string]any)["session_id"])

	// 5. revoking the key rejects it at the endpoint.
	require.NoError(t, f.keys.RevokeKey(context.Background(), "key-1", time.Now().UTC()))
	c, rec = agentEndpointRequest(t, "tools/list", nil, "tok-1")
	require.NoError(t, ep.HandleAgentEndpoint(*c))
	assert.Equal(t, 403, rec.Code)
}

// ============================================================================
// response decoding helpers
// ============================================================================

func listedToolNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	result, _ := resp.Result.(map[string]any)
	tools, _ := result["tools"].([]any)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	return names
}

func toolCallText(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	result, _ := resp.Result.(map[string]any)
	content, _ := result["content"].([]any)
	require.NotEmpty(t, content)
	return content[0].(map[string]any)["text"].(string)
}

func toolCallEnvelope(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(toolCallText(t, rec)), &m))
	return m
}
