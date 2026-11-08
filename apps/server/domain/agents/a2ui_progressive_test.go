package agents

import (
	"testing"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"

	"google.golang.org/genai"
)

func TestA2UIFrameThrottle_Coalesces(t *testing.T) {
	now := time.Unix(0, 0)
	th := newA2UIFrameThrottle(100 * time.Millisecond)
	th.now = func() time.Time { return now }

	// A surface's first frame always emits.
	if !th.emit("s1") {
		t.Fatal("first emit for s1 should be allowed")
	}
	// Inside the interval the next frame is dropped.
	if th.emit("s1") {
		t.Fatal("emit inside the interval should be dropped")
	}
	// A different surface is rate-limited independently.
	if !th.emit("s2") {
		t.Fatal("first emit for s2 should be allowed")
	}
	// After the interval elapses, s1 may emit again.
	now = now.Add(100 * time.Millisecond)
	if !th.emit("s1") {
		t.Fatal("emit after the interval should be allowed")
	}
}

func TestA2UIProgressiveEvents_EmitsCumulativeFrames(t *testing.T) {
	s := a2ui.NewSurfaceStream()
	th := newA2UIFrameThrottle(0) // interval 0 = never throttle

	events := a2uiProgressiveEvents(s, th,
		"```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n")
	if len(events) != 1 {
		t.Fatalf("createSurface frame = %d events, want 1", len(events))
	}
	if events[0].Type != StreamEventA2UI || events[0].SurfaceID != "s1" {
		t.Fatalf("frame = %+v, want A2UI s1", events[0])
	}
	if len(events[0].A2UI) != 1 || events[0].A2UI[0].CreateSurface == nil {
		t.Fatalf("frame messages = %+v, want just createSurface", events[0].A2UI)
	}

	events = a2uiProgressiveEvents(s, th,
		"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n")
	if len(events) != 1 {
		t.Fatalf("updateComponents frame = %d events, want 1", len(events))
	}
	// Every progressive frame carries the cumulative set so a renderer can
	// update the card in place rather than append a second one.
	if len(events[0].A2UI) != 2 {
		t.Fatalf("cumulative frame = %d messages, want 2", len(events[0].A2UI))
	}
}

func TestA2UIProgressiveEvents_ThrottledFrameDroppedButRetained(t *testing.T) {
	now := time.Unix(0, 0)
	th := newA2UIFrameThrottle(time.Hour)
	th.now = func() time.Time { return now }

	s := a2ui.NewSurfaceStream()

	events := a2uiProgressiveEvents(s, th,
		"```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n")
	if len(events) != 1 {
		t.Fatalf("first frame = %d, want 1", len(events))
	}

	// A second message inside the interval is dropped (not emitted) …
	events = a2uiProgressiveEvents(s, th,
		"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\"}]}}\n")
	if len(events) != 0 {
		t.Fatalf("throttled frame = %d, want 0", len(events))
	}
	// … but it is retained in the cumulative set, so the next emitted frame
	// (or the guaranteed final frame) still carries it.
	if len(s.Messages()) != 2 {
		t.Fatalf("Messages() = %d, want 2 cumulative", len(s.Messages()))
	}
}

func TestA2UITeardownEvents(t *testing.T) {
	// No progressive surface: nothing to tear down.
	if evs := a2uiTeardownEvents(a2ui.NewSurfaceStream(), nil); evs != nil {
		t.Fatalf("no surface → teardown = %+v, want nil", evs)
	}

	// A surface shown progressively, but the finished response did not validate
	// a surface (invalid block): tear the card down.
	s := a2ui.NewSurfaceStream()
	s.Feed("```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n")
	final := finalResponseStreamEvents([]*genai.Part{{Text: "```a2ui\nnot json\n```"}})
	if finalResponseHasSurface(final) {
		t.Fatal("invalid final response unexpectedly has a surface")
	}
	evs := a2uiTeardownEvents(s, final)
	if len(evs) != 1 || evs[0].Type != StreamEventA2UI || len(evs[0].A2UI) != 1 || evs[0].A2UI[0].DeleteSurface == nil {
		t.Fatalf("teardown = %+v, want one deleteSurface A2UI event", evs)
	}
	if evs[0].A2UI[0].DeleteSurface.SurfaceID != "s1" {
		t.Fatalf("teardown surfaceId = %q, want s1", evs[0].A2UI[0].DeleteSurface.SurfaceID)
	}

	// A surface shown progressively, and the finished response validated it: the
	// complete frame supersedes the partials, so no teardown.
	s2 := a2ui.NewSurfaceStream()
	s2.Feed("```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n")
	valid := finalResponseStreamEvents([]*genai.Part{{Text: "```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"basic\"}}\n```"}})
	if !finalResponseHasSurface(valid) {
		t.Fatal("valid final response should have a surface")
	}
	if evs := a2uiTeardownEvents(s2, valid); evs != nil {
		t.Fatalf("valid surface → teardown = %+v, want nil", evs)
	}
}

func TestFinalResponseHasSurface(t *testing.T) {
	if finalResponseHasSurface(nil) {
		t.Fatal("nil events reported a surface")
	}
	if finalResponseHasSurface([]StreamEvent{{Type: StreamEventTextDelta, Text: "hi"}}) {
		t.Fatal("text-only events reported a surface")
	}
	if !finalResponseHasSurface([]StreamEvent{{Type: StreamEventA2UI, SurfaceID: "s1"}}) {
		t.Fatal("A2UI event did not report a surface")
	}
}
