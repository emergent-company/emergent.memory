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

// a2uiWithdrawSurfaceEvents returns a deleteSurface frame for a surface that was
// shown progressively (at least one message parsed, so a frame may have been
// emitted) but will never receive its authoritative final frame. The renderer
// removes the card instead of leaving one on screen that no longer has an owner.
// It returns nil when no message was parsed: nothing can have been drawn.
func a2uiWithdrawSurfaceEvents(progressive *a2ui.SurfaceStream) []StreamEvent {
	if progressive == nil || len(progressive.Messages()) == 0 {
		return nil
	}
	sid := progressive.SurfaceID()
	return []StreamEvent{{
		Type:      StreamEventA2UI,
		SurfaceID: sid,
		A2UI:      []a2ui.Message{{DeleteSurface: &a2ui.DeleteSurface{SurfaceID: sid}}},
	}}
}

// a2uiTeardownEvents returns a deleteSurface frame when a surface was shown
// progressively (at least one message parsed, so a frame may have been emitted)
// but the finished response did not validate a surface, so the stale card must
// be removed rather than left half-rendered.
func a2uiTeardownEvents(progressive *a2ui.SurfaceStream, finalEvents []StreamEvent) []StreamEvent {
	if finalResponseHasSurface(finalEvents) {
		return nil
	}
	return a2uiWithdrawSurfaceEvents(progressive)
}

// a2uiStepStreams owns the answer-text filter and the progressive surface stream
// for the step the run is currently on. A surface is scoped to the run and step
// that streamed it (see a2uiNamespace), so a surface left unfinalised by a step
// is never superseded by the next step's card; the stage must tear it down when
// the step is left.
//
// The unfinalised case is real, not theoretical: IsFinalResponse() does not fire
// for a step that ends in a tool call, so a partial surface parsed in that step
// would otherwise stay on screen for the rest of the run (#621).
type a2uiStepStreams struct {
	runID  string
	step   int // step currently staged; -1 before any step has streamed
	stream *a2ui.SurfaceStream
	filter *a2ui.TextFilter
}

func newA2UIStepStreams(runID string) *a2uiStepStreams {
	return &a2uiStepStreams{
		runID:  runID,
		step:   -1,
		stream: a2ui.NewSurfaceStream(),
		filter: a2ui.NewTextFilter(),
	}
}

// beginStep stages the filter and the progressive stream for step and returns
// the frames that must be emitted before the step's answer starts: the
// withdrawal of a surface the previous step showed but never finalised. A step
// that ended in a tool call leaves exactly that behind, and because scoping
// gives the next card a different id the orphan is no longer replaced in place.
func (s *a2uiStepStreams) beginStep(step int) []StreamEvent {
	if step == s.step {
		return nil
	}
	evs := s.withdraw()
	s.step = step
	s.stream = a2ui.NewNamespacedSurfaceStream(a2uiNamespace(s.runID, step))
	s.filter = a2ui.NewTextFilter()
	return evs
}

// resetStep clears what the finished step still holds so a retry or the next
// step starts from a clean filter and surface.
func (s *a2uiStepStreams) resetStep() {
	s.stream = a2ui.NewNamespacedSurfaceStream(a2uiNamespace(s.runID, s.step))
	s.filter = a2ui.NewTextFilter()
}

// withdraw returns a deleteSurface frame for a surface the staged step showed
// progressively and never finalised, and forgets it so a repeated teardown never
// resends a frame for a surface already withdrawn.
func (s *a2uiStepStreams) withdraw() []StreamEvent {
	evs := a2uiWithdrawSurfaceEvents(s.stream)
	if evs != nil {
		s.stream = a2ui.NewNamespacedSurfaceStream(a2uiNamespace(s.runID, s.step))
	}
	return evs
}

// emitStreamEvents forwards events to a run's stream callback, if one is set.
func emitStreamEvents(cb StreamCallback, events []StreamEvent) {
	if cb == nil {
		return
	}
	for _, ev := range events {
		cb(ev)
	}
}
