package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// pollFailureCaptureInterval is the minimum gap between Sentry captures while a
// background poll failure streak continues.
const pollFailureCaptureInterval = 5 * time.Minute

// convSub is one subscribed conversation: the subscriber channels plus the
// session context captured when the first subscriber subscribed (nil when the
// subscription came from a no-session / dev client). The poller reuses it so
// every memory call made on the conversation's behalf carries the session's
// project headers (memory v0.73 requires X-Project-ID on project-scoped calls).
type convSub struct {
	chans map[chan []byte]struct{}
	sc    *sessionContext
}

// conversationHub fans out {"type":"refresh"} events to browsers subscribed to
// a conversation when its state changes (a run ends, a tool approval is
// decided, or an ask_user question is answered elsewhere). A single global
// poller feeds every subscription — there is no per-conversation goroutine.
type conversationHub struct {
	mu            sync.Mutex
	subs          map[string]*convSub // conversationID → subscriber channels + session context
	last          map[string]string   // conversationID → last broadcast fingerprint
	pollerCancel  context.CancelFunc
	pollerRunning bool
	srv           *Server // for conversation-state polling
	// pollFailures tracks each session group's consecutive poll failures so a
	// sustained outage is reported once at the start and then at most once per
	// pollFailureCaptureInterval. Keyed by a non-secret session identifier.
	pollFailures map[string]*pollFailureState
}

func newConversationHub(srv *Server) *conversationHub {
	return &conversationHub{
		subs:         make(map[string]*convSub),
		last:         make(map[string]string),
		srv:          srv,
		pollFailures: make(map[string]*pollFailureState),
	}
}

// pollFailureState tracks one session group's consecutive background poll
// failures.
type pollFailureState struct {
	streakActive bool
	lastCapture  time.Time
}

// pollFailureKey is a non-secret session-group identifier for the poll failure
// limiter. The bearer token is deliberately excluded so it never ends up in a
// map key, log line or Sentry event.
func pollFailureKey(sc *sessionContext) string {
	if sc == nil {
		return "no-session"
	}
	return sc.Sub + "\x00" + sc.ProjectID
}

// pollLogContext renders a non-secret triage label (conversation id + active
// project) for background poll logs. It never includes the bearer token.
func pollLogContext(conversationID string, sc *sessionContext) string {
	project := ""
	if sc != nil {
		project = sc.ProjectID
	}
	return fmt.Sprintf("conversation=%s project=%s", conversationID, project)
}

// shouldCapturePollFailure reports whether a poll failure for key should be
// reported to Sentry now, and records the capture. The first failure of a new
// streak is always reported; while the streak continues, reports are limited to
// one per pollFailureCaptureInterval. now is a parameter so tests can advance
// time without sleeping.
func (h *conversationHub) shouldCapturePollFailure(key string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.pollFailures[key]
	if st == nil {
		st = &pollFailureState{}
		h.pollFailures[key] = st
	}
	if st.streakActive && now.Sub(st.lastCapture) < pollFailureCaptureInterval {
		return false
	}
	st.streakActive = true
	st.lastCapture = now
	return true
}

// resetPollFailure clears key's failure streak after a fully successful poll,
// so the next failure is reported immediately again.
func (h *conversationHub) resetPollFailure(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pollFailures, key)
}

// subscribe registers a subscriber channel for a conversation. sc is the
// subscriber's session context (nil for a no-session client); the first
// subscriber's sc is stored on the conversation and later subscribers share it
// (all browsers watching one conversation belong to the same project by
// construction). When the global subscriber count transitions 0→1 the single
// poller goroutine is started.
func (h *conversationHub) subscribe(id string, sc *sessionContext) chan []byte {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if cs := h.subs[id]; cs != nil {
		cs.chans[ch] = struct{}{}
	} else {
		h.subs[id] = &convSub{chans: map[chan []byte]struct{}{ch: {}}, sc: sc}
	}
	if h.subscriberCountLocked() == 1 && !h.pollerRunning {
		h.pollerRunning = true
		ctx, cancel := context.WithCancel(context.Background())
		h.pollerCancel = cancel
		go h.pollLoop(ctx)
	}
	return ch
}

// unsubscribe removes a subscriber channel. When the global subscriber count
// hits 0 the poller is cancelled.
func (h *conversationHub) unsubscribe(id string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cs := h.subs[id]; cs != nil {
		delete(cs.chans, ch)
		if len(cs.chans) == 0 {
			delete(h.subs, id)
			delete(h.last, id)
		}
	}
	if h.subscriberCountLocked() == 0 && h.pollerRunning {
		if h.pollerCancel != nil {
			h.pollerCancel()
		}
		h.pollerRunning = false
		h.pollerCancel = nil
	}
}

func (h *conversationHub) subscriberCountLocked() int {
	n := 0
	for _, cs := range h.subs {
		n += len(cs.chans)
	}
	return n
}

// subscribedConvs snapshots the subscribed conversation ids together with the
// session context captured at subscribe time (nil for no-session subscribers).
func (h *conversationHub) subscribedConvs() map[string]*sessionContext {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]*sessionContext, len(h.subs))
	for id, cs := range h.subs {
		out[id] = cs.sc
	}
	return out
}

// updateFingerprint records the last-seen fingerprint for a conversation and
// reports whether it changed (and therefore deserves a broadcast).
func (h *conversationHub) updateFingerprint(id, fp string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last[id] == fp {
		return false
	}
	h.last[id] = fp
	return true
}

// broadcast sends msg to every subscriber of a conversation. Send is
// non-blocking: a slow subscriber's channel buffer overflows and the frame is
// dropped — the next changed fingerprint re-broadcasts.
func (h *conversationHub) broadcast(id string, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cs := h.subs[id]; cs != nil {
		for ch := range cs.chans {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

// pollLoop is the hub's single poller: every 1500ms it gathers project-wide
// question/approval state once per subscribed session, fingerprints each
// subscribed conversation, and broadcasts on change. Exits when cancelled or
// when no subscribers remain.
func (h *conversationHub) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		convs := h.subscribedConvs()
		if len(convs) == 0 {
			// No subscribers this tick. Do NOT self-park: unsubscribe cancels
			// ctx and flips pollerRunning under the mutex. Self-parking here
			// races a concurrent subscribe (which can observe pollerRunning
			// still true and skip starting a poller) and strands a fresh
			// subscriber with no live updates. Just skip this tick.
			continue
		}
		h.srv.broadcastConversationChanges(ctx, convs)
	}
}

// sessionKey identifies one session identity (bearer token + active project)
// so the poller can group the project-wide question/approval snapshots. A nil
// sc (no-session / dev subscriber) maps to the empty key and keeps using the
// bare poller context.
func sessionKey(sc *sessionContext) string {
	if sc == nil {
		return ""
	}
	return sc.Token + "\x00" + sc.ProjectID
}

// Run buckets derived by the gateway from a conversation's newest run
// lifecycle item plus its pending approvals/questions. They reference the
// acp-run-lifecycle status vocabulary rather than redefining it: the ordering
// (needs_input > failed > running > done) mirrors that capability's
// aggregation rule.
const (
	runBucketNeedsInput = "needs_input"
	runBucketFailed     = "failed"
	runBucketRunning    = "running"
	runBucketDone       = "done"
)

// conversationRunState is the derived run-control state for one conversation,
// produced from the single history fetch the poller already performs plus the
// already-fetched project-wide approval/question snapshots. It feeds both the
// SSE change-detection fingerprint and the refresh payload the rail/dock
// consume.
type conversationRunState struct {
	runEndCount      int
	pendingApprovals []string
	pendingQuestions []string
	activeRunID      string
	activeRunStatus  string
	bucket           string
}

// refreshPayload is the shape broadcast to conversation subscribers. Existing
// consumers that only read {"type":"refresh"} keep working: the extra fields
// are additive and ignored by anything that does not decode them.
type refreshPayload struct {
	Type             string `json:"type"`
	Bucket           string `json:"bucket"`
	RunID            string `json:"runId"`
	RunStatus        string `json:"runStatus"`
	PendingApprovals int    `json:"pendingApprovals"`
	PendingQuestions int    `json:"pendingQuestions"`
}

// refreshFrame marshals the per-conversation refresh payload. A marshal
// failure (impossible with this struct) degrades to the bare refresh frame so
// the change-detection fan-out never breaks.
func refreshFrame(st *conversationRunState) []byte {
	if st == nil {
		return []byte(`{"type":"refresh"}`)
	}
	b, err := json.Marshal(refreshPayload{
		Type:             "refresh",
		Bucket:           st.bucket,
		RunID:            st.activeRunID,
		RunStatus:        st.activeRunStatus,
		PendingApprovals: len(st.pendingApprovals),
		PendingQuestions: len(st.pendingQuestions),
	})
	if err != nil {
		return []byte(`{"type":"refresh"}`)
	}
	return b
}

// deriveRunBucket maps a conversation's newest run status (and its pending
// decisions) onto a single bucket. A pending approval or ask_user question
// forces needs_input regardless of status — a pending human decision outranks
// a failure because the failure is already resolved by the time the user acts,
// whereas a pending decision blocks the run. An input-required status is itself
// "waiting on the user". No run lifecycle item yields done.
func deriveRunBucket(status string, pendingApprovals, pendingQuestions []string) string {
	if len(pendingApprovals) > 0 || len(pendingQuestions) > 0 {
		return runBucketNeedsInput
	}
	switch status {
	case "failed":
		return runBucketFailed
	case "submitted", "working", "cancelling":
		return runBucketRunning
	case "input-required":
		return runBucketNeedsInput
	default:
		return runBucketDone
	}
}

// broadcastConversationChanges computes a fingerprint per subscribed
// conversation from one project-wide snapshot per session group and pushes
// refresh frames to those whose state moved on. convs maps each subscribed
// conversation id to the session context captured at subscribe time (nil for
// no-session subscribers). Errors are non-fatal: a failed snapshot is skipped
// and retried on the next tick.
func (s *Server) broadcastConversationChanges(ctx context.Context, convs map[string]*sessionContext) {
	// Group conversations by session identity so each session's
	// question/approval snapshot is fetched once per tick, not once per
	// conversation.
	groups := map[string][]string{}
	for id, sc := range convs {
		k := sessionKey(sc)
		groups[k] = append(groups[k], id)
	}
	for _, ids := range groups {
		sc := convs[ids[0]]
		cctx := ctx
		if sc != nil {
			cctx = withSessionContext(ctx, sc)
		}
		// The poll loop is a 1.5s self-healing tick, so a failed snapshot is
		// logged and retried on the next tick. A sustained failure is still
		// reported (rate-limited) so a multi-hour outage is observable.
		questions, qErr := s.memory.ListAgentQuestions(cctx)
		if qErr != nil {
			log.Printf("conversation poll: list agent questions (%s): %v", pollLogContext(ids[0], sc), qErr)
		}
		approvals, aErr := s.memory.ListToolApprovals(cctx)
		if aErr != nil {
			log.Printf("conversation poll: list tool approvals (%s): %v", pollLogContext(ids[0], sc), aErr)
		}
		key := pollFailureKey(sc)
		if pollErr := errors.Join(qErr, aErr); pollErr != nil {
			if s.hub.shouldCapturePollFailure(key, time.Now()) {
				capturePollFailure(pollErr)
			}
		} else {
			s.hub.resetPollFailure(key)
		}
		for _, id := range ids {
			cc := ctx
			groupSC := convs[id]
			if groupSC != nil {
				cc = withSessionContext(ctx, groupSC)
			}
			st, err := s.conversationState(cc, id, approvals, questions)
			if err != nil {
				// Per-conversation failures are expected (deleted/not-yet-visible
				// conversation) and are skipped; log for triage, never capture.
				log.Printf("conversation poll: conversation state (%s): %v", pollLogContext(id, groupSC), err)
				continue
			}
			// The fingerprint must cover every run-state transition — bucket,
			// active run id, and active run status included — so a completed run
			// followed by a new run_start (runEndCount unchanged) still
			// re-broadcasts instead of leaving the rail stuck at "done".
			fp := fmt.Sprintf("%d|%s|%s|%s|%v|%v", st.runEndCount, st.bucket, st.activeRunID, st.activeRunStatus, st.pendingApprovals, st.pendingQuestions)
			if !s.hub.updateFingerprint(id, fp) {
				continue
			}
			s.hub.broadcast(id, refreshFrame(st))
		}
	}
}

// conversationState fingerprints the parts of a conversation's state the chat
// page cares about: completed runs, pending tool approvals, and unanswered
// ask_user questions — plus the derived run bucket and the active run's id and
// status from the newest run_start/run_end lifecycle item. approvals/questions
// are the already-fetched project-wide snapshots so the poller only hits
// memory once per session group per tick (plus the single history fetch per
// conversation).
func (s *Server) conversationState(ctx context.Context, id string, approvals []ToolApprovalItem, questions []AgentQuestionItem) (*conversationRunState, error) {
	hist, err := s.conversationTimeline(ctx, id)
	if err != nil {
		return nil, err
	}
	st := &conversationRunState{bucket: runBucketDone}
	items := parseTimeline(hist.Items)
	for _, it := range items {
		switch it.Kind {
		case "run_end":
			st.runEndCount++
			st.activeRunID = it.RunID
			st.activeRunStatus = it.RunStatus
		case "run_start":
			st.activeRunID = it.RunID
			st.activeRunStatus = it.RunStatus
		}
	}
	answered := make(map[string]bool, len(questions))
	for _, q := range questions {
		if q.Response != nil && *q.Response != "" {
			answered[q.ID] = true
		}
	}
	for _, a := range approvals {
		if a.ConversationID == id && a.Decision == "pending" {
			st.pendingApprovals = append(st.pendingApprovals, a.QuestionID)
		}
	}
	// Mirror injectQuestionAnswers' question_id correlation: an ask_user tool
	// call whose question has no response yet is still pending.
	for _, it := range items {
		if it.Kind != "tool_call" || it.ToolName != "ask_user" {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(it.ToolOutput, &out); err != nil {
			continue
		}
		qid, _ := out["question_id"].(string)
		if qid == "" || answered[qid] {
			continue
		}
		st.pendingQuestions = append(st.pendingQuestions, qid)
	}
	st.bucket = deriveRunBucket(st.activeRunStatus, st.pendingApprovals, st.pendingQuestions)
	return st, nil
}

// conversationEvents is the SSE endpoint a chat page subscribes to. It sends an
// immediate refresh so a fresh/reconnecting client gets current state, then
// forwards hub broadcasts plus a 25s heartbeat until the client disconnects.
func (s *Server) conversationEvents(c echo.Context) error {
	id := c.Param("id")
	w := c.Response().Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		return c.NoContent(http.StatusInternalServerError)
	}

	if s.hub == nil {
		// Degrade gracefully (bare Server in tests): still stream a single
		// refresh frame so the client has a valid conversation state snapshot.
		sendSSE(w, flusher, `{"type":"refresh"}`)
		select {
		case <-c.Request().Context().Done():
		case <-s.shutdownCh:
		}
		return nil
	}

	// Capture the subscriber's session context so the background poller can
	// scope its memory calls (X-Project-ID etc.) to this conversation's
	// session. Requests reach this handler through authDispatch/attachSession,
	// so the context carries the session in session mode; dev-mode callers
	// pass nil and the poller keeps using its bare context.
	sc, _ := sessionContextFrom(c.Request().Context())
	ch := s.hub.subscribe(id, sc)
	defer s.hub.unsubscribe(id, ch)

	sendSSE(w, flusher, `{"type":"refresh"}`)

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-s.shutdownCh:
			return nil
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		case msg := <-ch:
			if _, err := io.WriteString(w, "data: "+string(msg)+"\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}

// sendSSE writes one `data: <payload>\n\n` frame and flushes.
func sendSSE(w io.Writer, f http.Flusher, payload string) {
	_, _ = io.WriteString(w, "data: "+payload+"\n\n")
	f.Flush()
}
