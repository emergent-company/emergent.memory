package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// hubRecordingMemory wraps fakeMemory and records the contexts the hub poller's
// memory calls arrive with, so tests can assert the poller threads the
// subscription's session context through (memory v0.73 requires X-Project-ID on
// every project-scoped call; the header is derived from the ctx's
// sessionContext by sessionHeaders).
type hubRecordingMemory struct {
	*fakeMemory
	mu           sync.Mutex
	historyCtxs  []context.Context // GetConversationHistory call contexts
	questionCtxs []context.Context // ListAgentQuestions call contexts
	approvalCtxs []context.Context // ListToolApprovals call contexts
}

func (m *hubRecordingMemory) record(ctx context.Context, dst *[]context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	*dst = append(*dst, ctx)
}

func (m *hubRecordingMemory) GetConversationHistory(ctx context.Context, id string) (*ConversationHistory, error) {
	m.record(ctx, &m.historyCtxs)
	return &ConversationHistory{ConversationID: id, Items: []json.RawMessage{}}, nil
}

func (m *hubRecordingMemory) ListAgentQuestions(ctx context.Context) ([]AgentQuestionItem, error) {
	m.record(ctx, &m.questionCtxs)
	return nil, nil
}

func (m *hubRecordingMemory) ListToolApprovals(ctx context.Context) ([]ToolApprovalItem, error) {
	m.record(ctx, &m.approvalCtxs)
	return nil, nil
}

func (m *hubRecordingMemory) contexts() (history, questions, approvals []context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]context.Context(nil), m.historyCtxs...),
		append([]context.Context(nil), m.questionCtxs...),
		append([]context.Context(nil), m.approvalCtxs...)
}

func newHubTestServer(m MemoryBackend) *Server {
	s := &Server{cfg: Config{DefaultAgent: "memory"}, memory: m}
	s.hub = newConversationHub(s)
	return s
}

// assertSessionProject asserts ctx carries a sessionContext with the expected
// session identity (the memory client maps it to X-Project-ID/X-Org-ID headers).
func assertSessionProject(t *testing.T, ctx context.Context, wantToken, wantProject string) {
	t.Helper()
	sc, ok := sessionContextFrom(ctx)
	if !ok {
		t.Fatalf("memory call ctx carries no session context")
	}
	if sc.Token != wantToken || sc.ProjectID != wantProject {
		t.Errorf("memory call ctx session = token %q project %q, want token %q project %q", sc.Token, sc.ProjectID, wantToken, wantProject)
	}
}

// TestHubPollerScopesCallsWithSession asserts that a conversation subscribed
// WITH a sessionContext makes the poller's ListAgentQuestions/ListToolApprovals
// and GetConversationHistory calls carry that session's token+project, and that
// the resulting fingerprint change broadcasts a refresh frame.
func TestHubPollerScopesCallsWithSession(t *testing.T) {
	m := &hubRecordingMemory{fakeMemory: &fakeMemory{}}
	s := newHubTestServer(m)

	sc := &sessionContext{Token: "tok-1", ProjectID: "proj-1", OrgID: "org-1"}
	ch := s.hub.subscribe("c1", sc)
	defer s.hub.unsubscribe("c1", ch)

	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())

	history, questions, approvals := m.contexts()
	if len(history) != 1 || len(questions) != 1 || len(approvals) != 1 {
		t.Fatalf("memory calls = history %d questions %d approvals %d, want 1 each", len(history), len(questions), len(approvals))
	}
	assertSessionProject(t, history[0], "tok-1", "proj-1")
	assertSessionProject(t, questions[0], "tok-1", "proj-1")
	assertSessionProject(t, approvals[0], "tok-1", "proj-1")

	// First tick changed the (empty) fingerprint → a refresh frame is broadcast.
	select {
	case msg := <-ch:
		if string(msg) != `{"type":"refresh"}` {
			t.Errorf("broadcast payload = %q, want refresh frame", msg)
		}
	default:
		t.Error("changed fingerprint must broadcast a refresh frame")
	}
}

// TestHubPollerNilSessionKeepsBareCtx asserts that a nil-sc subscription (dev /
// no-session client) leaves the poller on its bare context — sessionContextFrom
// must NOT match — so dev-mode behavior is unchanged.
func TestHubPollerNilSessionKeepsBareCtx(t *testing.T) {
	m := &hubRecordingMemory{fakeMemory: &fakeMemory{}}
	s := newHubTestServer(m)

	ch := s.hub.subscribe("c1", nil)
	defer s.hub.unsubscribe("c1", ch)

	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())

	history, questions, approvals := m.contexts()
	if len(history) != 1 || len(questions) != 1 || len(approvals) != 1 {
		t.Fatalf("memory calls = history %d questions %d approvals %d, want 1 each", len(history), len(questions), len(approvals))
	}
	for _, ctx := range append(append(history, questions...), approvals...) {
		if sc, ok := sessionContextFrom(ctx); ok {
			t.Errorf("nil-sc subscription must keep the bare poller ctx, got session %+v", sc)
		}
	}
}

// TestHubPollerGroupsBySession asserts the project-wide snapshots are fetched
// once per session identity, not once per conversation: two conversations under
// one session share a single ListAgentQuestions/ListToolApprovals call, while a
// conversation under a second session (or no session) gets its own snapshot.
func TestHubPollerGroupsBySession(t *testing.T) {
	m := &hubRecordingMemory{fakeMemory: &fakeMemory{}}
	s := newHubTestServer(m)

	scA := &sessionContext{Token: "tok-a", ProjectID: "proj-a"}
	scB := &sessionContext{Token: "tok-b", ProjectID: "proj-b"}
	ch1 := s.hub.subscribe("c-a1", scA)
	ch2 := s.hub.subscribe("c-a2", scA)
	ch3 := s.hub.subscribe("c-b1", scB)
	chNil := s.hub.subscribe("c-nil", nil)
	defer func() {
		for id, ch := range map[string]chan []byte{"c-a1": ch1, "c-a2": ch2, "c-b1": ch3, "c-nil": chNil} {
			s.hub.unsubscribe(id, ch)
		}
	}()

	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())

	history, questions, approvals := m.contexts()
	// 3 sessions (A, B, nil) → 3 snapshot pairs; 4 conversations → 4 history calls.
	if len(questions) != 3 || len(approvals) != 3 {
		t.Fatalf("snapshot calls = questions %d approvals %d, want 3 each (one per session)", len(questions), len(approvals))
	}
	if len(history) != 4 {
		t.Fatalf("history calls = %d, want 4 (one per conversation)", len(history))
	}
	// Each session group got exactly one snapshot pair, scoped to its own
	// session (map iteration order of the groups is unspecified, so match by
	// session identity).
	var snapTokA, snapTokB, snapBare int
	for _, ctx := range questions {
		if sc, ok := sessionContextFrom(ctx); ok {
			switch {
			case sc.Token == "tok-a" && sc.ProjectID == "proj-a":
				snapTokA++
			case sc.Token == "tok-b" && sc.ProjectID == "proj-b":
				snapTokB++
			default:
				t.Errorf("question snapshot session = token %q project %q, want tok-a/proj-a or tok-b/proj-b", sc.Token, sc.ProjectID)
			}
		} else {
			snapBare++
		}
	}
	if snapTokA != 1 || snapTokB != 1 || snapBare != 1 {
		t.Errorf("question snapshots by session = tok-a %d tok-b %d bare %d, want 1/1/1", snapTokA, snapTokB, snapBare)
	}
	var apprTokA, apprTokB, apprBare int
	for _, ctx := range approvals {
		if sc, ok := sessionContextFrom(ctx); ok {
			switch {
			case sc.Token == "tok-a" && sc.ProjectID == "proj-a":
				apprTokA++
			case sc.Token == "tok-b" && sc.ProjectID == "proj-b":
				apprTokB++
			default:
				t.Errorf("approval snapshot session = token %q project %q, want tok-a/proj-a or tok-b/proj-b", sc.Token, sc.ProjectID)
			}
		} else {
			apprBare++
		}
	}
	if apprTokA != 1 || apprTokB != 1 || apprBare != 1 {
		t.Errorf("approval snapshots by session = tok-a %d tok-b %d bare %d, want 1/1/1", apprTokA, apprTokB, apprBare)
	}
	// History: one fetch per conversation, scoped to that conversation's
	// session — A's two under proj-a, B's under proj-b, the nil-sc one bare.
	var histTokA, histTokB, histBare int
	for _, ctx := range history {
		if sc, ok := sessionContextFrom(ctx); ok {
			switch {
			case sc.Token == "tok-a" && sc.ProjectID == "proj-a":
				histTokA++
			case sc.Token == "tok-b" && sc.ProjectID == "proj-b":
				histTokB++
			default:
				t.Errorf("history session = token %q project %q, want tok-a/proj-a or tok-b/proj-b", sc.Token, sc.ProjectID)
			}
		} else {
			histBare++
		}
	}
	if histTokA != 2 || histTokB != 1 || histBare != 1 {
		t.Errorf("history calls by session = tok-a %d tok-b %d bare %d, want 2/1/1", histTokA, histTokB, histBare)
	}
}

// TestHubSubscribeFirstSessionWins asserts the first subscriber's session
// context is the one stored for the conversation (later subscribers share it).
func TestHubSubscribeFirstSessionWins(t *testing.T) {
	h := newConversationHub(nil)
	scA := &sessionContext{Token: "tok-a", ProjectID: "proj-a"}
	scB := &sessionContext{Token: "tok-b", ProjectID: "proj-b"}
	ch1 := h.subscribe("c1", scA)
	ch2 := h.subscribe("c1", scB)
	defer h.unsubscribe("c1", ch1)
	defer h.unsubscribe("c1", ch2)

	convs := h.subscribedConvs()
	if got := convs["c1"]; got != scA {
		t.Errorf("stored session = %+v, want the first subscriber's %+v", got, scA)
	}
}
