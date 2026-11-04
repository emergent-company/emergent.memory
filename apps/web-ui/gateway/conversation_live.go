package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// liveStateHub retains, per conversation, the in-flight tail of the active run:
// open thinking segments and running tool calls. The rewritten frames that pass
// through the /api/chat proxy are teed into it (see liveTee), and it is read by
// /api/conversations/:id/events to synthesize the additive `live_replay` frame
// that reconstructs "what is happening now" after a mid-run reload.
//
// Growth is bounded two ways: each conversation keeps at most
// liveMaxFramesPerConv frames (the oldest is dropped on overflow), and the hub
// keeps at most liveMaxConversations conversations (idle ones past liveIdleTTL
// are evicted first, then the least-recently-seen). State is also cleared when a
// run ends (the stream's `done` frame) and when the poller observes the
// conversation's derived bucket leave `running`.
type liveStateHub struct {
	mu     sync.Mutex
	states map[string]*liveConversationState // conversationID → in-flight tail
}

const (
	// liveMaxFramesPerConv bounds how many in-flight frames one conversation
	// retains. The tail is small (a handful of thinking segments + tool calls),
	// so this is a generous hard cap against pathological streams.
	liveMaxFramesPerConv = 64
	// liveMaxConversations bounds the total number of retained conversations.
	liveMaxConversations = 4096
	// liveIdleTTL is how long a conversation may sit without a live frame before
	// it becomes evictable under memory pressure.
	liveIdleTTL = 10 * time.Minute
)

// liveFrameKind discriminates the retained frame types.
type liveFrameKind int

const (
	liveKindThinking liveFrameKind = iota
	liveKindTool
)

// liveFrame is one retained in-flight frame: the correlation key (a thinking
// segment id, a tool call id, or a tool name for the id-less fallback) plus the
// frame's original JSON, re-emitted verbatim on replay.
type liveFrame struct {
	key  string
	kind liveFrameKind
	raw  json.RawMessage
}

// liveConversationState is the in-flight tail of one conversation's active run.
type liveConversationState struct {
	runID    string
	frames   []liveFrame // ordered by arrival; replayed in this order
	lastSeen time.Time
}

func newLiveStateHub() *liveStateHub {
	return &liveStateHub{states: make(map[string]*liveConversationState)}
}

// stateLocked returns (creating if necessary) the conversation's state and
// stamps it as recently active. It evicts under memory pressure before creating
// a new conversation entry.
func (h *liveStateHub) stateLocked(conversationID string) *liveConversationState {
	st := h.states[conversationID]
	if st == nil {
		if len(h.states) >= liveMaxConversations {
			h.evictLocked()
		}
		st = &liveConversationState{}
		h.states[conversationID] = st
	}
	st.lastSeen = time.Now()
	return st
}

// evictLocked frees conversation state under the memory cap: idle-past-TTL
// conversations first, then the least-recently-seen if still over.
func (h *liveStateHub) evictLocked() {
	now := time.Now()
	for id, st := range h.states {
		if now.Sub(st.lastSeen) > liveIdleTTL {
			delete(h.states, id)
		}
	}
	if len(h.states) < liveMaxConversations {
		return
	}
	var oldest string
	var oldestSeen time.Time
	for id, st := range h.states {
		if oldest == "" || st.lastSeen.Before(oldestSeen) {
			oldest, oldestSeen = id, st.lastSeen
		}
	}
	if oldest != "" {
		delete(h.states, oldest)
	}
}

// beginRun starts a fresh in-flight buffer for conversationID — a new agent run
// began (the stream's `meta` frame). Any previous run's lingering frames are
// dropped, since they belong to a completed (or superseded) run.
func (h *liveStateHub) beginRun(conversationID, runID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.states[conversationID] == nil && len(h.states) >= liveMaxConversations {
		h.evictLocked()
	}
	h.states[conversationID] = &liveConversationState{runID: runID, lastSeen: time.Now()}
}

// endRun clears the conversation's in-flight state. Called on the stream's
// terminal `done` frame; the poller's clearIfNotRunning is the authoritative
// backstop for runs that end without a clean stream.
func (h *liveStateHub) endRun(conversationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.states, conversationID)
}

// clearIfNotRunning clears the conversation's state when the poller observes its
// derived bucket leave `running` (done/failed/needs_input). Reuses the same
// runBucketRunning constant the poller derives buckets with.
func (h *liveStateHub) clearIfNotRunning(conversationID, bucket string) {
	if bucket == runBucketRunning {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.states, conversationID)
}

// observeThinking updates the hub from a rewritten `thinking` frame's data
// payload. A `done:false` delta upserts the open segment (latest delta wins);
// a `done:true` close drops the segment.
func (h *liveStateHub) observeThinking(conversationID, data string) {
	var ev struct {
		ID   string `json:"id"`
		Done bool   `json:"done"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return
	}
	if ev.ID == "" {
		return // cannot correlate a segment without a stable id
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.stateLocked(conversationID)
	if ev.Done {
		st.dropFrame(liveKindThinking, ev.ID)
		return
	}
	st.upsertFrame(liveKindThinking, ev.ID, json.RawMessage(data))
}

// observeTool updates the hub from a rewritten `mcp_tool` frame's data payload.
// A non-terminal status (started/running/…) records a running tool; a terminal
// status (completed/error/awaiting_confirmation) drops it. Tools are correlated
// by their `id` when present, and by tool name (dropping the oldest match) for
// the id-less case produced by older servers.
func (h *liveStateHub) observeTool(conversationID, data string) {
	var ev struct {
		ID     string `json:"id"`
		Tool   string `json:"tool"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.stateLocked(conversationID)
	if isTerminalToolStatus(ev.Status) {
		st.dropFrame(liveKindTool, cmp.Or(ev.ID, ev.Tool))
		return
	}
	if ev.ID != "" {
		st.upsertFrame(liveKindTool, ev.ID, json.RawMessage(data))
		return
	}
	// Id-less (older servers): key on the tool name but never dedupe — parallel
	// same-name calls are distinct and a terminal event drops the oldest match.
	st.appendFrame(liveFrame{key: ev.Tool, kind: liveKindTool, raw: json.RawMessage(data)})
}

// isTerminalToolStatus reports whether a tool status closes the call.
func isTerminalToolStatus(status string) bool {
	switch status {
	case "completed", "error", "awaiting_confirmation":
		return true
	default:
		return false
	}
}

// upsertFrame replaces the frame with the same key (if present) or appends it.
// Used for thinking segments and id'd tool calls, where successive deltas of one
// segment/call share a stable key and should collapse to a single frame.
func (st *liveConversationState) upsertFrame(kind liveFrameKind, key string, raw json.RawMessage) {
	for i := range st.frames {
		if st.frames[i].kind == kind && st.frames[i].key == key {
			st.frames[i].raw = raw
			return
		}
	}
	st.appendFrame(liveFrame{key: key, kind: kind, raw: raw})
}

// dropFrame removes the first frame of the given kind/key (the oldest match).
func (st *liveConversationState) dropFrame(kind liveFrameKind, key string) {
	for i := range st.frames {
		if st.frames[i].kind == kind && st.frames[i].key == key {
			st.frames = append(st.frames[:i], st.frames[i+1:]...)
			return
		}
	}
}

// appendFrame appends f, dropping the oldest frame if the cap is reached.
func (st *liveConversationState) appendFrame(f liveFrame) {
	if len(st.frames) >= liveMaxFramesPerConv {
		st.frames = st.frames[1:]
	}
	st.frames = append(st.frames, f)
}

// liveReplayPayload is the additive frame emitted to a connecting conversation
// events client immediately after the initial `refresh` frame.
type liveReplayPayload struct {
	Type   string            `json:"type"`
	RunID  string            `json:"runId"`
	Frames []json.RawMessage `json:"frames"`
}

// replay returns the current live_replay payload for a conversation: the active
// run id plus the open thinking/running tool frames in arrival order. An empty
// frames array is returned when nothing is in flight.
func (h *liveStateHub) replay(conversationID string) liveReplayPayload {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.states[conversationID]
	if st == nil {
		return liveReplayPayload{Type: "live_replay", Frames: []json.RawMessage{}}
	}
	frames := make([]json.RawMessage, len(st.frames))
	for i, f := range st.frames {
		frames[i] = f.raw
	}
	return liveReplayPayload{Type: "live_replay", RunID: st.runID, Frames: frames}
}

// replayFrame marshals the live_replay payload for SSE emission.
func (h *liveStateHub) replayFrame(conversationID string) []byte {
	payload := h.replay(conversationID)
	b, err := marshalNoEscape(payload)
	if err != nil {
		return []byte(`{"type":"live_replay","runId":"","frames":[]}`)
	}
	return b
}

// liveTee wraps the /api/chat stream's pipe writer: it forwards every byte
// through unchanged while parsing complete SSE frames to keep the hub's
// in-flight tail current. It learns the conversation id from the request (when
// known) or the stream's `meta` frame (new conversations), and the run id from
// `meta`. Frame parsing is lenient — anything it cannot attribute is ignored,
// never forwarded differently.
type liveTee struct {
	w      io.Writer
	hub    *liveStateHub
	convID string // conversation id, from request or the stream's meta frame
	buf    []byte // buffered bytes awaiting a \n\n frame boundary
}

func newLiveTee(w io.Writer, hub *liveStateHub, conversationID string) *liveTee {
	return &liveTee{w: w, hub: hub, convID: conversationID}
}

func (t *liveTee) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.buf = append(t.buf, p[:n]...)
		t.drain()
	}
	return n, err
}

// drain consumes complete SSE frames from the buffer (split on \n\n, the same
// boundary splitSSEEvent uses).
func (t *liveTee) drain() {
	for {
		i := bytes.Index(t.buf, []byte("\n\n"))
		if i < 0 {
			return
		}
		raw := t.buf[:i+2]
		t.buf = t.buf[i+2:]
		t.handle(raw)
	}
}

func (t *liveTee) handle(raw []byte) {
	data := extractSSEData(raw)
	if data == "" {
		return
	}
	var ev struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return
	}
	switch ev.Type {
	case "meta":
		var m struct {
			ConversationID string `json:"conversationId"`
			RunID          string `json:"runId"`
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return
		}
		t.convID = cmp.Or(t.convID, m.ConversationID)
		if t.convID != "" {
			t.hub.beginRun(t.convID, m.RunID)
		}
	case "thinking":
		if t.convID != "" {
			t.hub.observeThinking(t.convID, data)
		}
	case "mcp_tool":
		if t.convID != "" {
			t.hub.observeTool(t.convID, data)
		}
	case "done":
		if t.convID != "" {
			t.hub.endRun(t.convID)
		}
	}
}
