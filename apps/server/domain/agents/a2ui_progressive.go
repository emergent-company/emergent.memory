package agents

import (
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// a2uiFrameInterval bounds how often one surface's progressive frames are
// emitted. It is a server-side throttle, not a client debounce: a frame that
// arrives inside the window is dropped, and the guaranteed final frame (emitted
// by finalResponseStreamEvents with the complete message set) closes any gap.
const a2uiFrameInterval = 200 * time.Millisecond

// a2uiFrameThrottle rate-limits progressive surface frames to at most one per
// surface per interval. Because every emitted frame carries the cumulative
// message set for its surface, dropping an over-interval frame loses nothing:
// the next emitted frame carries the newer, larger set. The final complete frame
// is emitted by finalResponseStreamEvents directly (not through this throttle),
// so a surface always converges to the complete component set even when its last
// updates fall inside the throttle window.
type a2uiFrameThrottle struct {
	interval time.Duration
	now      func() time.Time
	mu       sync.Mutex
	last     map[string]time.Time
}

func newA2UIFrameThrottle(interval time.Duration) *a2uiFrameThrottle {
	return &a2uiFrameThrottle{
		interval: interval,
		now:      time.Now,
		last:     make(map[string]time.Time),
	}
}

// emit reports whether a frame for surfaceID may be emitted now, recording the
// emit when it returns true. It returns false when a frame for that surface was
// emitted within the last interval; the caller then drops the frame (the next
// emitted frame carries the cumulative set).
func (t *a2uiFrameThrottle) emit(surfaceID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if last, ok := t.last[surfaceID]; ok && now.Sub(last) < t.interval {
		return false
	}
	t.last[surfaceID] = now
	return true
}

// a2uiProgressiveEvents feeds a streamed answer chunk through the progressive
// surface parser and returns the surface frame to emit now (throttled), or nil.
// It is additive to answerTextEvents: the text filter must run first so raw
// protocol JSONL never reaches the answer, and this helper only produces surface
// frames from fence content. Every frame carries the cumulative message set for
// its surface so the renderer can update a card in place.
func a2uiProgressiveEvents(s *a2ui.SurfaceStream, t *a2uiFrameThrottle, chunk string) []StreamEvent {
	if s.Feed(chunk) == nil {
		return nil
	}
	if t != nil && !t.emit(s.SurfaceID()) {
		return nil
	}
	return []StreamEvent{{
		Type:      StreamEventA2UI,
		SurfaceID: s.SurfaceID(),
		A2UI:      s.Messages(),
	}}
}

// finalResponseHasSurface reports whether the finished-response classification
// produced a surface frame (the authoritative, complete one). A run whose final
// response carries no such frame has no surface to converge to, so a surface
// shown progressively must be torn down instead.
func finalResponseHasSurface(events []StreamEvent) bool {
	for _, ev := range events {
		if ev.Type == StreamEventA2UI {
			return true
		}
	}
	return false
}

// a2uiTeardownEvents returns a deleteSurface frame when a surface was shown
// progressively (at least one message parsed, so a frame may have been emitted)
// but the finished response did not validate a surface, so the stale card must
// be removed rather than left half-rendered.
func a2uiTeardownEvents(progressive *a2ui.SurfaceStream, finalEvents []StreamEvent) []StreamEvent {
	if progressive == nil || len(progressive.Messages()) == 0 || finalResponseHasSurface(finalEvents) {
		return nil
	}
	sid := progressive.SurfaceID()
	return []StreamEvent{{
		Type:      StreamEventA2UI,
		SurfaceID: sid,
		A2UI:      []a2ui.Message{{DeleteSurface: &a2ui.DeleteSurface{SurfaceID: sid}}},
	}}
}
