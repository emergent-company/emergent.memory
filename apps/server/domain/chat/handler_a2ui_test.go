package chat

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

// #427: only the authoritative finished-response frame may replace the persisted
// surface set. A progressive frame is a partial snapshot and a deleteSurface
// teardown must not be stored as history.
func TestFinalA2UISurfacesKeepsOnlyTheFinalFrame(t *testing.T) {
	progressive := []a2ui.Message{{UpdateComponents: &a2ui.UpdateComponents{SurfaceID: "s1"}}}
	final := []a2ui.Message{
		{CreateSurface: &a2ui.CreateSurface{SurfaceID: "s1", CatalogID: a2ui.CatalogID}},
		{UpdateComponents: &a2ui.UpdateComponents{SurfaceID: "s1"}},
	}
	teardown := []a2ui.Message{{DeleteSurface: &a2ui.DeleteSurface{SurfaceID: "s1"}}}

	var got []a2ui.Message
	got = finalA2UISurfaces(got, agents.StreamEvent{Type: agents.StreamEventA2UI, SurfaceID: "s1", A2UI: progressive, Final: false})
	if len(got) != 0 {
		t.Fatalf("progressive frame was persisted: %+v", got)
	}
	got = finalA2UISurfaces(got, agents.StreamEvent{Type: agents.StreamEventA2UI, SurfaceID: "s1", A2UI: final, Final: true})
	if len(got) != 2 || got[0].CreateSurface == nil {
		t.Fatalf("final frame = %+v, want the createSurface set", got)
	}
	got = finalA2UISurfaces(got, agents.StreamEvent{Type: agents.StreamEventA2UI, SurfaceID: "s1", A2UI: teardown, Final: false})
	if len(got) != 2 || got[0].CreateSurface == nil {
		t.Fatalf("teardown replaced the final frame: %+v", got)
	}
}

// A card-only turn stores the summary for text-only consumers, while a turn
// with real prose keeps the prose.
func TestAssistantPersistText(t *testing.T) {
	surfaces := []a2ui.Message{{UpdateComponents: &a2ui.UpdateComponents{SurfaceID: "s1", Components: []a2ui.Component{{
		ID: "c1", Component: "proposal", Props: map[string]any{"kind": "change", "summary": "Ship it"},
	}}}}}
	got := assistantPersistText("", surfaces)
	if want := a2ui.SummaryText(surfaces); got != want {
		t.Fatalf("assistantPersistText() = %q, want the summary %q", got, want)
	}
	if got := assistantPersistText("real answer", surfaces); got != "real answer" {
		t.Fatalf("assistantPersistText() = %q, want the real answer preserved", got)
	}
}
