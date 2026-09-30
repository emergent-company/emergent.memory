package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
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

// scriptedMemory returns a fixed sequence of conversation histories, one per
// GetConversationHistory call, so a test can drive two poller ticks with
// different timeline state. It embeds fakeMemory for the project-wide
// ListAgentQuestions/ListToolApprovals (both empty).
type scriptedMemory struct {
	*fakeMemory
	histories []*ConversationHistory
	calls     int
}

func (m *scriptedMemory) GetConversationHistory(ctx context.Context, id string) (*ConversationHistory, error) {
	if m.calls < len(m.histories) {
		h := m.histories[m.calls]
		m.calls++
		return h, nil
	}
	return &ConversationHistory{ConversationID: id, Items: []json.RawMessage{}}, nil
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
		var p refreshPayload
		if err := json.Unmarshal(msg, &p); err != nil {
			t.Fatalf("broadcast payload is not a refresh frame: %v (%s)", err, msg)
		}
		if p.Type != "refresh" {
			t.Errorf("broadcast type = %q, want refresh", p.Type)
		}
		// An empty history with no pending decisions derives the idle bucket.
		if p.Bucket != runBucketDone || p.RunID != "" || p.RunStatus != "" || p.PendingApprovals != 0 || p.PendingQuestions != 0 {
			t.Errorf("empty-history refresh payload = %+v, want done bucket with no run or pending counts", p)
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

// TestHubBroadcastCachesAndClearsLastFrame asserts the hub caches the last
// broadcast refresh frame per conversation (so a later subscriber replays it)
// and drops the cache when the conversation's last subscriber unsubscribes (so
// a stale frame is never replayed into a brand-new conversation). Run-scoped
// broadcasts never populate the cache.
func TestHubBroadcastCachesAndClearsLastFrame(t *testing.T) {
	h := newConversationHub(nil)

	frame := refreshFrame(&conversationRunState{
		bucket:          runBucketRunning,
		activeRunID:     "r1",
		activeRunStatus: "working",
	})

	if got := h.cachedFrame("c1"); got != nil {
		t.Fatalf("cachedFrame before broadcast = %s, want nil", got)
	}

	ch := h.subscribe("c1", nil)
	h.broadcast("c1", frame)
	if got := h.cachedFrame("c1"); string(got) != string(frame) {
		t.Fatalf("cachedFrame after broadcast = %s, want %s", got, frame)
	}

	// Run-scoped broadcasts must not populate the conversation cache.
	runKey := runScopeKey("r9")
	chRun := h.subscribe(runKey, nil)
	h.broadcast(runKey, frame)
	if got := h.cachedFrame(runKey); got != nil {
		t.Fatalf("run-scoped frame must not be cached, got %s", got)
	}

	// Unsubscribing the last conversation subscriber clears the cache.
	h.unsubscribe("c1", ch)
	if got := h.cachedFrame("c1"); got != nil {
		t.Fatalf("cachedFrame after unsubscribe = %s, want nil", got)
	}
	h.unsubscribe(runKey, chRun)
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

// TestPollFailureCaptureLimiter asserts a failure streak captures once when it
// starts, then at most once per pollFailureCaptureInterval, and that a success
// resets the streak.
func TestPollFailureCaptureLimiter(t *testing.T) {
	h := newConversationHub(nil)
	base := time.Now()
	const key = "group-1"

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "first failure of a new streak", now: base, want: true},
		{name: "second failure within window", now: base.Add(time.Second), want: false},
		{name: "still within window", now: base.Add(pollFailureCaptureInterval - time.Second), want: false},
		{name: "window elapsed", now: base.Add(pollFailureCaptureInterval), want: true},
		{name: "within next window", now: base.Add(pollFailureCaptureInterval + time.Second), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := h.shouldCapturePollFailure(key, tt.now); got != tt.want {
				t.Fatalf("shouldCapturePollFailure(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}

	// A success clears the streak, so the next failure reports immediately.
	h.resetPollFailure(key)
	if !h.shouldCapturePollFailure(key, base.Add(pollFailureCaptureInterval+time.Second)) {
		t.Fatal("failure after reset should capture immediately")
	}

	// Distinct session groups have independent streaks.
	if !h.shouldCapturePollFailure("group-2", base) {
		t.Fatal("first failure of a different group should capture")
	}
	if h.shouldCapturePollFailure("group-2", base.Add(time.Second)) {
		t.Fatal("second failure of a different group within window should not capture")
	}
}

// TestPollFailureKeyExcludesToken asserts the limiter/log key never embeds the
// session bearer token.
func TestPollFailureKeyExcludesToken(t *testing.T) {
	sc := &sessionContext{Token: "super-secret-token", Sub: "user-1", ProjectID: "proj-1"}
	key := pollFailureKey(sc)
	if key == "" || key == "no-session" {
		t.Fatalf("pollFailureKey = %q, want a session-group key", key)
	}
	if strings.Contains(key, "super-secret-token") {
		t.Fatalf("pollFailureKey leaked the bearer token: %q", key)
	}
	if got := pollFailureKey(nil); got != "no-session" {
		t.Fatalf("pollFailureKey(nil) = %q, want %q", got, "no-session")
	}
}

// TestRunStateFingerprintChangesOnStepProgress is the unit-level regression
// test for #1203: the change-detection fingerprint MUST move when a run
// persists a step/message/tool call, and MUST stay stable when nothing
// changed. The "stepped" sub-case fails against the pre-fix fingerprint, which
// ignored all step/message/tool progress.
func TestRunStateFingerprintChangesOnStepProgress(t *testing.T) {
	base := &conversationRunState{
		runEndCount:     1,
		bucket:          runBucketRunning,
		activeRunID:     "r1",
		activeRunStatus: "working",
	}

	// Identical state fingerprints identically — no spurious broadcasts.
	same := &conversationRunState{runEndCount: 1, bucket: runBucketRunning, activeRunID: "r1", activeRunStatus: "working"}
	if got, want := runStateFingerprint(base), runStateFingerprint(same); got != want {
		t.Fatalf("identical state fingerprint = %q, want stable %q", got, want)
	}

	// A step advancing while the run stays `running` (same run id/status, no
	// new run_end, no pending decision) MUST change the fingerprint. The legacy
	// fingerprint carries all of those unchanged fields, so it is equal here —
	// which is exactly the pre-fix bug the assertion guards against.
	stepped := *base
	stepped.progressCount = 1
	stepped.progressMark = "m1"
	if got, want := runStateFingerprint(&stepped), runStateFingerprint(base); got == want {
		t.Fatalf("fingerprint did not change on step progress: %q == %q (step/message/tool progress ignored)", got, want)
	}

	// The next step changes it again.
	stepped2 := stepped
	stepped2.progressCount = 2
	stepped2.progressMark = "m2"
	if got, want := runStateFingerprint(&stepped2), runStateFingerprint(&stepped); got == want {
		t.Fatalf("fingerprint did not change on the next step: %q == %q", got, want)
	}

	// The fields the out-of-band refresh depends on still move it.
	variant := *base
	variant.pendingQuestions = []string{"q1"}
	if runStateFingerprint(&variant) == runStateFingerprint(base) {
		t.Fatal("fingerprint must still change when a question becomes pending")
	}
}

// TestHubPollerRebroadcastsOnStepProgressWithinRunningRun is the poller-level
// regression test for #1203: a running run that persists a step re-broadcasts,
// even though runEndCount/bucket/runId/status/pending are all unchanged.
func TestHubPollerRebroadcastsOnStepProgressWithinRunningRun(t *testing.T) {
	runStart := json.RawMessage(`{"kind":"run_start","run_id":"r1","run_status":"working"}`)
	step := json.RawMessage(`{"kind":"tool_call","id":"t1","run_id":"r1","step_number":1,"created_at":"2026-01-01T00:00:01Z","tool_name":"search","tool_status":"completed"}`)
	m := &scriptedMemory{
		fakeMemory: &fakeMemory{},
		histories: []*ConversationHistory{
			{ConversationID: "c1", Items: []json.RawMessage{runStart}},
			{ConversationID: "c1", Items: []json.RawMessage{runStart, step}},
		},
	}
	s := newHubTestServer(m)

	ch := s.hub.subscribe("c1", nil)
	defer s.hub.unsubscribe("c1", ch)

	// First tick: the run started, so the bucket flipped to running and the
	// (empty) baseline fingerprint changed.
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	select {
	case <-ch:
	default:
		t.Fatal("first tick must broadcast the running state")
	}

	// Second tick: a step was persisted while the run stayed running, with no
	// new run_end and no pending decision. Every legacy fingerprint field is
	// unchanged; only the step/message/tool progress signal can re-broadcast.
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	select {
	case msg := <-ch:
		var p refreshPayload
		if err := json.Unmarshal(msg, &p); err != nil {
			t.Fatalf("second-tick payload is not a refresh frame: %v (%s)", err, msg)
		}
		if p.Bucket != runBucketRunning || p.RunID != "r1" {
			t.Errorf("second-tick payload = %+v, want running bucket with run r1", p)
		}
	default:
		t.Error("a persisted step must re-broadcast a refresh frame while the run is still running (#1203)")
	}
}

// TestHubPollerRebroadcastsOnNewRunWithSamePendingState asserts the poller
// fingerprint covers the bucket + active run id/status, so a completed run
// followed by a new run_start (runEndCount and pending approvals/questions both
// unchanged) still re-broadcasts a refresh frame. Without that coverage the
// rail would stay stuck at "done".
func TestHubPollerRebroadcastsOnNewRunWithSamePendingState(t *testing.T) {
	runEnd := json.RawMessage(`{"kind":"run_end","run_id":"r1","run_status":"completed"}`)
	runStart := json.RawMessage(`{"kind":"run_start","run_id":"r2","run_status":"working"}`)
	m := &scriptedMemory{
		fakeMemory: &fakeMemory{},
		histories: []*ConversationHistory{
			{ConversationID: "c1", Items: []json.RawMessage{runEnd}},
			{ConversationID: "c1", Items: []json.RawMessage{runEnd, runStart}},
		},
	}
	s := newHubTestServer(m)

	ch := s.hub.subscribe("c1", nil)
	defer s.hub.unsubscribe("c1", ch)

	// First tick: only the completed run_end. Its fingerprint differs from the
	// empty baseline, so a refresh frame is broadcast.
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	first := <-ch
	var p1 refreshPayload
	if err := json.Unmarshal(first, &p1); err != nil {
		t.Fatalf("first-tick payload is not a refresh frame: %v (%s)", err, first)
	}
	if p1.Bucket != runBucketDone || p1.RunID != "r1" {
		t.Errorf("first-tick payload = %+v, want done bucket with run r1", p1)
	}

	// Second tick: a new run_start follows the completed run with no pending
	// approvals/questions. runEndCount is unchanged, so only the bucket and
	// active run id/status in the fingerprint can trigger the re-broadcast.
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	select {
	case msg := <-ch:
		var p2 refreshPayload
		if err := json.Unmarshal(msg, &p2); err != nil {
			t.Fatalf("second-tick payload is not a refresh frame: %v (%s)", err, msg)
		}
		if p2.Bucket != runBucketRunning || p2.RunID != "r2" || p2.RunStatus != "working" {
			t.Errorf("second-tick payload = %+v, want running bucket with run r2 status working", p2)
		}
	default:
		t.Error("new run must re-broadcast a refresh frame even with unchanged pending state")
	}
}

// runScriptedMemory returns a fixed sequence of GetRunFull results, one per
// call, so a test can drive two poller ticks with different run DTOs. It embeds
// fakeMemory for the project-wide answers/approvals (both empty) and
// GetRunQuestions (nil).
type runScriptedMemory struct {
	*fakeMemory
	runs  []*AgentRunFull
	calls int
}

func (m *runScriptedMemory) GetRunFull(ctx context.Context, runID string) (*AgentRunFull, error) {
	if m.calls < len(m.runs) {
		full := m.runs[m.calls]
		m.calls++
		return full, nil
	}
	return m.runs[len(m.runs)-1], nil
}

// TestHubPollerRunScopeBroadcastsOnStatusOnlyTransition is the regression test
// for the run-scoped fingerprint: runTimelineItems emits no run_start/run_end,
// so without reading the run DTO the run scope would stay id/status-empty and
// a terminal status-only change would never re-broadcast. The run DTO supplies
// the live id + status.
func TestHubPollerRunScopeBroadcastsOnStatusOnlyTransition(t *testing.T) {
	working := &AgentRunFull{Run: &ScheduledAgentRun{ID: "r1", Status: "working", StartedAt: "2026-01-01T00:00:00Z"}}
	completed := &AgentRunFull{Run: &ScheduledAgentRun{ID: "r1", Status: "completed", StartedAt: "2026-01-01T00:00:00Z"}}
	m := &runScriptedMemory{fakeMemory: &fakeMemory{}, runs: []*AgentRunFull{working, completed}}
	s := newHubTestServer(m)

	key := runScopeKey("r1")
	ch := s.hub.subscribe(key, nil)
	defer s.hub.unsubscribe(key, ch)

	// First tick: the run is live → running bucket, broadcast with the run's id
	// and status.
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	first := <-ch
	var p1 refreshPayload
	if err := json.Unmarshal(first, &p1); err != nil {
		t.Fatalf("first-tick payload is not a refresh frame: %v (%s)", err, first)
	}
	if p1.Bucket != runBucketRunning || p1.RunID != "r1" || p1.RunStatus != "working" {
		t.Errorf("first-tick payload = %+v, want running bucket with run r1 status working", p1)
	}

	// Second tick: status-only transition to completed — same run, no new
	// timeline item, no new run_end, no pending decision. Only the run DTO's
	// status can move the fingerprint.
	s.broadcastConversationChanges(context.Background(), s.hub.subscribedConvs())
	select {
	case msg := <-ch:
		var p2 refreshPayload
		if err := json.Unmarshal(msg, &p2); err != nil {
			t.Fatalf("second-tick payload is not a refresh frame: %v (%s)", err, msg)
		}
		if p2.Bucket != runBucketDone || p2.RunID != "r1" || p2.RunStatus != "completed" {
			t.Errorf("second-tick payload = %+v, want done bucket with run r1 status completed", p2)
		}
	default:
		t.Error("a run status-only transition must re-broadcast a refresh frame (#1204 run scope)")
	}
}
