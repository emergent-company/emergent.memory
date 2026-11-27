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
		"```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n")
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
		"```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n")
	if len(events) != 1 {
		t.Fatalf("first frame = %d, want 1", len(events))
	}

	// A second message inside the interval is dropped (not emitted) …
	events = a2uiProgressiveEvents(s, th,
		"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n")
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
	s.Feed("```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n")
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
	s2.Feed("```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n")
	valid := finalResponseStreamEvents([]*genai.Part{{Text: "```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n```"}})
	if !finalResponseHasSurface(valid) {
		t.Fatal("valid final response should have a surface")
	}
	if evs := a2uiTeardownEvents(s2, valid); evs != nil {
		t.Fatalf("valid surface → teardown = %+v, want nil", evs)
	}
}

func TestA2UISurfacesAreScopedPerTurn(t *testing.T) {
	// The #592 repro at the seam where the collision happened: two turns of one
	// conversation, both taught by the prompt to name their surface "s1". Before
	// scoping, both produced the wire surfaceId "s1", so the live renderer
	// (which keys its surface registry by surfaceId) replaced the first turn's
	// card with the second one's and the transcript kept only one card.
	block := "```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"` + a2ui.CatalogID + `"}}` + "\n" +
		`{"updateComponents":{"surfaceId":"s1","components":[{"id":"r1","component":"result","rows":[]}]}}` + "\n" +
		"```\n"
	parts := []*genai.Part{{Text: block}}

	turn := func(runID string) (progressive, final StreamEvent) {
		s := a2ui.NewNamespacedSurfaceStream(a2uiNamespace(runID, 0))
		events := a2uiProgressiveEvents(s, nil, block)
		if len(events) != 1 {
			t.Fatalf("%s: progressive frames = %d, want 1", runID, len(events))
		}
		finalEvents := finalResponseStreamEvents(parts)
		namespaceA2UIEvents(finalEvents, a2uiNamespace(runID, 0))
		var finalEvent StreamEvent
		for _, ev := range finalEvents {
			if ev.Type == StreamEventA2UI {
				finalEvent = ev
			}
		}
		if finalEvent.Type != StreamEventA2UI {
			t.Fatalf("%s: final response produced no A2UI event", runID)
		}
		return events[0], finalEvent
	}

	firstProgressive, firstFinal := turn("11111111-1111-1111-1111-111111111111")
	secondProgressive, secondFinal := turn("22222222-2222-2222-2222-222222222222")

	// The two turns are distinct surfaces on the wire — both live frames and the
	// authoritative final frames.
	if firstProgressive.SurfaceID == secondProgressive.SurfaceID {
		t.Fatalf("both live frames scoped to %q", firstProgressive.SurfaceID)
	}
	if firstFinal.SurfaceID == secondFinal.SurfaceID {
		t.Fatalf("both final frames scoped to %q", firstFinal.SurfaceID)
	}

	// One turn's progressive frame and its final frame agree, so the final frame
	// updates the card the stream already showed instead of appending another.
	if firstProgressive.SurfaceID != firstFinal.SurfaceID {
		t.Fatalf("progressive id %q != final id %q", firstProgressive.SurfaceID, firstFinal.SurfaceID)
	}

	// The ids inside the messages agree with the frame, so a reload of the
	// persisted payload and a card action report the same surface.
	for _, ev := range []StreamEvent{firstProgressive, firstFinal} {
		if len(ev.A2UI) == 0 || ev.A2UI[0].CreateSurface == nil {
			t.Fatalf("%q: payload = %+v, want createSurface", ev.SurfaceID, ev.A2UI)
		}
		if got := ev.A2UI[0].CreateSurface.SurfaceID; got != ev.SurfaceID {
			t.Fatalf("payload surfaceId = %q, frame surfaceId = %q", got, ev.SurfaceID)
		}
	}
}

func TestA2UINamespaceIncludesTheStep(t *testing.T) {
	// A run may answer more than once (a plan step and the final answer), so the
	// scope is per run *and* step, not per run.
	if a2uiNamespace("run-1", 0) == a2uiNamespace("run-1", 1) {
		t.Fatal("two steps of the same run share a surface namespace")
	}
}

func TestPersistedSurfacesAreScopedLikeTheFrames(t *testing.T) {
	// The reload path reads surfaces from the persisted row, so it must carry the
	// same scoped ids the live and final frames carried (#592).
	surfaces := []a2ui.Message{
		{CreateSurface: &a2ui.CreateSurface{SurfaceID: "s1", CatalogID: a2ui.CatalogID}},
		{UpdateComponents: &a2ui.UpdateComponents{SurfaceID: "s1"}},
	}
	got := persistedSurfaces("run-9", 2, surfaces)
	if len(got) != 2 {
		t.Fatalf("persistedSurfaces = %d messages, want 2", len(got))
	}
	if got[0].CreateSurface == nil || got[0].CreateSurface.SurfaceID != "run-9.2:s1" {
		t.Fatalf("persisted createSurface = %+v, want run-9.2:s1", got[0].CreateSurface)
	}
	if got[1].UpdateComponents == nil || got[1].UpdateComponents.SurfaceID != "run-9.2:s1" {
		t.Fatalf("persisted updateComponents = %+v, want run-9.2:s1", got[1].UpdateComponents)
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

func TestA2UITeardownEvents_UpdateComponentsOnly(t *testing.T) {
	// A block that streams only updateComponents (no createSurface) still renders
	// a card — the web renderer draws components directly and iOS lazily opens
	// the surface — so it must be torn down too when the finished response is
	// invalid, even though Opened() is false.
	s := a2ui.NewSurfaceStream()
	if got := s.Feed("```a2ui\n{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n"); len(got) != 1 {
		t.Fatalf("Feed = %d messages, want 1", len(got))
	}
	if s.Opened() {
		t.Fatal("Opened() = true for an updateComponents-only block, want false")
	}

	final := finalResponseStreamEvents([]*genai.Part{{Text: "```a2ui\nnot json\n```"}})
	evs := a2uiTeardownEvents(s, final)
	if len(evs) != 1 || evs[0].Type != StreamEventA2UI || len(evs[0].A2UI) != 1 || evs[0].A2UI[0].DeleteSurface == nil {
		t.Fatalf("teardown = %+v, want one deleteSurface A2UI event", evs)
	}
	if evs[0].SurfaceID != s.SurfaceID() || evs[0].A2UI[0].DeleteSurface.SurfaceID != s.SurfaceID() {
		t.Fatalf("teardown surfaceId = %q / %q, want %q", evs[0].SurfaceID, evs[0].A2UI[0].DeleteSurface.SurfaceID, s.SurfaceID())
	}
}

// a2uiHasID reports whether ids contains id.
func a2uiHasID(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

func TestA2UIStepStreams_AbandonedSurfaceIsWithdrawnWhenTheStepEnds(t *testing.T) {
	// The shape #621 describes: a step streams a card, then ends in a tool call
	// so IsFinalResponse() never fires for it, and the run continues with a new
	// step. With per-run/per-step scoping the next step's card gets a new id, so
	// it no longer supersedes the half-rendered one: only an explicit teardown
	// keeps the orphan off screen.
	const (
		create = "```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n"
		update = "{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n"
		closer = "```\n"
	)

	stage := newA2UIStepStreams("run-1")
	throttle := newA2UIFrameThrottle(0) // never throttle

	var opened, finalised, withdrawn []string
	emit := func(evs []StreamEvent) {
		for _, ev := range evs {
			if ev.Type != StreamEventA2UI {
				continue
			}
			if ev.Final {
				finalised = append(finalised, ev.SurfaceID)
			}
			for _, m := range ev.A2UI {
				switch {
				case m.CreateSurface != nil:
					opened = append(opened, ev.SurfaceID)
				case m.DeleteSurface != nil:
					withdrawn = append(withdrawn, ev.SurfaceID)
				}
			}
		}
	}

	// Step 0: a partial surface is in flight when the step ends in a tool call.
	emit(stage.beginStep(0))
	emit(a2uiProgressiveEvents(stage.stream, throttle, create+update))
	if len(opened) != 1 {
		t.Fatalf("step 0 opened %v, want one surface", opened)
	}
	orphan := opened[0]
	if orphan != a2uiNamespace("run-1", 0)+":s1" {
		t.Fatalf("step 0 surface = %q, want the step-0 scope", orphan)
	}

	// Step 1 begins: the orphan must be withdrawn before the new step streams,
	// or it stays on screen for the rest of the run.
	emit(stage.beginStep(1))
	if len(withdrawn) != 1 || withdrawn[0] != orphan {
		t.Fatalf("withdrawn = %v, want [%s] before step 1 streams", withdrawn, orphan)
	}
	if got := stage.stream.SurfaceID(); got != "" {
		t.Fatalf("step 1 stream already carries surface %q, want empty", got)
	}

	// Step 1's card is a different id — it does not replace the orphan.
	finalEvents := finalResponseStreamEvents([]*genai.Part{{Text: create + update + closer}})
	namespaceA2UIEvents(finalEvents, a2uiNamespace("run-1", 1))
	emit(finalEvents)
	emit(a2uiTeardownEvents(stage.stream, finalEvents))
	stage.resetStep()

	// Run end.
	emit(stage.withdraw())

	// Every surface the client was shown a createSurface for either received its
	// authoritative final frame or was explicitly torn down: no orphan remains.
	for _, id := range opened {
		if !a2uiHasID(finalised, id) && !a2uiHasID(withdrawn, id) {
			t.Fatalf("surface %q was shown but never finalised or withdrawn (opened=%v finalised=%v withdrawn=%v)",
				id, opened, finalised, withdrawn)
		}
	}
	// The final card itself is not torn down.
	for _, id := range finalised {
		if a2uiHasID(withdrawn, id) {
			t.Fatalf("finalised surface %q was withdrawn", id)
		}
	}
	if len(finalised) != 1 {
		t.Fatalf("finalised = %v, want the one final frame", finalised)
	}
}

func TestA2UIStepStreams_RunEndWithdrawsTheLastAbandonedSurface(t *testing.T) {
	stage := newA2UIStepStreams("run-2")
	stage.beginStep(0)
	a2uiProgressiveEvents(stage.stream, nil,
		"```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n")
	shown := stage.stream.SurfaceID()
	if shown == "" {
		t.Fatal("no surface was staged")
	}

	evs := stage.withdraw()
	if len(evs) != 1 || evs[0].Type != StreamEventA2UI || len(evs[0].A2UI) != 1 || evs[0].A2UI[0].DeleteSurface == nil {
		t.Fatalf("withdraw = %+v, want one deleteSurface A2UI event", evs)
	}
	if evs[0].SurfaceID != shown || evs[0].A2UI[0].DeleteSurface.SurfaceID != shown {
		t.Fatalf("withdraw id = %q / %q, want %q", evs[0].SurfaceID, evs[0].A2UI[0].DeleteSurface.SurfaceID, shown)
	}

	// The stage forgets a surface it has withdrawn, so a second teardown (an
	// explicit one plus the defer, for example) never resends the frame.
	if evs := stage.withdraw(); evs != nil {
		t.Fatalf("second withdraw = %+v, want nil", evs)
	}
}

func TestA2UIStepStreams_FinalisedSurfaceIsNotWithdrawn(t *testing.T) {
	const block = "```a2ui\n" +
		`{"createSurface":{"surfaceId":"s1","catalogId":"memory-basic"}}` + "\n" +
		`{"updateComponents":{"surfaceId":"s1","components":[{"id":"r1","component":"result","rows":[]}]}}` + "\n" +
		"```\n"

	stage := newA2UIStepStreams("run-3")
	stage.beginStep(0)
	if evs := a2uiProgressiveEvents(stage.stream, nil, block); len(evs) != 1 {
		t.Fatalf("progressive frames = %d, want 1", len(evs))
	}

	finalEvents := finalResponseStreamEvents([]*genai.Part{{Text: block}})
	namespaceA2UIEvents(finalEvents, a2uiNamespace("run-3", 0))
	if evs := a2uiTeardownEvents(stage.stream, finalEvents); evs != nil {
		t.Fatalf("teardown of a finalised surface = %+v, want nil", evs)
	}
	stage.resetStep()
	if evs := stage.withdraw(); evs != nil {
		t.Fatalf("run end withdrew a finalised surface: %+v", evs)
	}
}

func TestA2UIStepStreams_KeepsTheStreamWithinOneStep(t *testing.T) {
	stage := newA2UIStepStreams("run-4")
	if evs := stage.beginStep(0); evs != nil {
		t.Fatalf("first beginStep withdrew %+v, want nil", evs)
	}
	a2uiProgressiveEvents(stage.stream, nil,
		"```a2ui\n{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n")
	staged := stage.stream
	// Progress within one step must not be mistaken for leaving it.
	if evs := stage.beginStep(0); evs != nil {
		t.Fatalf("same-step beginStep withdrew %+v, want nil", evs)
	}
	if stage.stream != staged {
		t.Fatal("same-step beginStep replaced the stream")
	}
	if got := stage.stream.SurfaceID(); got != a2uiNamespace("run-4", 0)+":s1" {
		t.Fatalf("staged surface = %q, want the step-0 scope", got)
	}
}
