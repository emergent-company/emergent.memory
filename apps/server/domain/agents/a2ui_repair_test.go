package agents

import (
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/a2ui"
)

func a2uiPart(text string) []*genai.Part {
	return []*genai.Part{{Text: text}}
}

const invalidSurfaceAlone = "```a2ui\n" +
	"{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n" +
	"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\"}]}}\n" +
	"```"

const validSurfaceAlone = "```a2ui\n" +
	"{\"createSurface\":{\"surfaceId\":\"s1\",\"catalogId\":\"memory-basic\"}}\n" +
	"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n" +
	"```"

// An invalid block that never opens the surface: the exact case the repair turn
// must patch. The updateComponents is invalid (missing required summary) and
// carries the surface id the repaired answer must reuse.
const invalidSurfaceWithoutCreate = "```a2ui\n" +
	"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\"}]}}\n" +
	"```"

const repairedSurfaceWithoutCreate = "```a2ui\n" +
	"{\"updateComponents\":{\"surfaceId\":\"s1\",\"components\":[{\"id\":\"p1\",\"component\":\"proposal\",\"kind\":\"change\",\"summary\":\"hi\"}]}}\n" +
	"```"

func TestA2UIRepairPromptCarriesValidatorIssues(t *testing.T) {
	prompt := a2uiRepairPrompt(a2uiPart(invalidSurfaceAlone), "s1")
	if prompt == "" {
		t.Fatal("expected a repair prompt for an invalid surface-only answer")
	}
	for _, want := range []string{
		`component "p1" (proposal): missing required prop "summary"`,
		"Reply with only a corrected",
		"Keep the same surfaceId",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// BLOCKER 1 regression: the repair prompt names the failed surface id and
// requires createSurface, so the model can reopen the surface it left behind.
func TestA2UIRepairPromptNamesSurfaceAndRequiresCreateSurface(t *testing.T) {
	prompt := a2uiRepairPrompt(a2uiPart(invalidSurfaceWithoutCreate), "s1")
	if prompt == "" {
		t.Fatal("expected a repair prompt for an invalid surface-only answer")
	}
	for _, want := range []string{
		"surfaceId s1",
		"createSurface",
		a2ui.CatalogID,
		"before any updateComponents line",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestA2UIRepairPromptIncludesInvalidJSON(t *testing.T) {
	prompt := a2uiRepairPrompt(a2uiPart("```a2ui\nnot json\n```"), a2ui.DefaultSurfaceID)
	if !strings.Contains(prompt, "invalid JSON") {
		t.Fatalf("prompt = %q, want an invalid-JSON issue", prompt)
	}
}

func TestA2UIRepairPromptSkipsWhenThereIsProse(t *testing.T) {
	// A card failure next to a good prose answer is not worth a second model call.
	parts := a2uiPart("Here is the answer.\n\n" + invalidSurfaceAlone)
	if got := a2uiRepairPrompt(parts, "s1"); got != "" {
		t.Fatalf("prompt = %q, want empty when prose is present", got)
	}
}

func TestA2UIRepairPromptSkipsValidSurface(t *testing.T) {
	if got := a2uiRepairPrompt(a2uiPart(validSurfaceAlone), "s1"); got != "" {
		t.Fatalf("prompt = %q, want empty for a valid surface", got)
	}
}

func TestA2UIRepairPromptSkipsNoFence(t *testing.T) {
	if got := a2uiRepairPrompt(a2uiPart("plain answer"), ""); got != "" {
		t.Fatalf("prompt = %q, want empty without a fence", got)
	}
	if got := a2uiRepairPrompt(nil, ""); got != "" {
		t.Fatalf("prompt = %q, want empty for no parts", got)
	}
}

func TestA2UIRepairStateCapturesFailedSurfaceID(t *testing.T) {
	state := &a2uiRepairState{}
	if _, ok := state.Next(a2uiPart(invalidSurfaceWithoutCreate)); !ok {
		t.Fatal("first Next() = false, want one repair")
	}
	if got := state.SurfaceID(); got != "s1" {
		t.Fatalf("SurfaceID() = %q, want the failed block's s1", got)
	}
}

func TestA2UIRepairStateIsBoundedToOneTurn(t *testing.T) {
	state := &a2uiRepairState{}
	invalid := a2uiPart(invalidSurfaceAlone)

	if _, ok := state.Next(invalid); !ok {
		t.Fatal("first Next() = false, want one repair")
	}
	if prompt, ok := state.Next(invalid); ok || prompt != "" {
		t.Fatalf("second Next() = (%q, true), want bounded to one repair", prompt)
	}
	if !state.Attempted() {
		t.Fatal("Attempted() = false after a repair")
	}
	if outcome := state.Outcome(invalid); outcome != "degraded" {
		t.Fatalf("Outcome() = %q, want degraded when the repair also failed", outcome)
	}
	// The second failure degrades to plain text: the protocol block is stripped
	// and no card frame is emitted, so the run still completes normally.
	for _, event := range finalResponseStreamEvents(invalid) {
		if event.Type == StreamEventA2UI {
			t.Fatalf("second failure emitted a card frame: %+v", event)
		}
	}
}

func TestA2UIRepairStateOutcomeRepaired(t *testing.T) {
	state := &a2uiRepairState{}
	if _, ok := state.Next(a2uiPart(invalidSurfaceAlone)); !ok {
		t.Fatal("first Next() = false, want one repair")
	}
	if outcome := state.Outcome(a2uiPart(validSurfaceAlone)); outcome != "repaired" {
		t.Fatalf("Outcome() = %q, want repaired", outcome)
	}
	// Exactly one repair turn produced a valid card.
	found := false
	for _, event := range finalResponseStreamEvents(a2uiPart(validSurfaceAlone)) {
		if event.Type == StreamEventA2UI {
			found = true
		}
	}
	if !found {
		t.Fatal("valid repaired answer did not emit a card frame")
	}
}

// A repaired answer that is valid but omits createSurface is stamped with the
// failed block's id, and the injected createSurface precedes updateComponents.
func TestA2UIRepairNormalizesMissingCreateSurface(t *testing.T) {
	state := &a2uiRepairState{}
	if _, ok := state.Next(a2uiPart(invalidSurfaceWithoutCreate)); !ok {
		t.Fatal("first Next() = false, want one repair")
	}

	repaired := ensureRepairedCreateSurface(a2uiPart(repairedSurfaceWithoutCreate), state.SurfaceID())
	events := finalResponseStreamEvents(repaired)

	var surface *StreamEvent
	for i := range events {
		if events[i].Type == StreamEventA2UI {
			surface = &events[i]
			break
		}
	}
	if surface == nil {
		t.Fatalf("events = %+v, want one A2UI surface frame", events)
	}
	if surface.SurfaceID != "s1" {
		t.Fatalf("SurfaceID = %q, want the failed block's s1", surface.SurfaceID)
	}
	if len(surface.A2UI) < 2 {
		t.Fatalf("messages = %+v, want createSurface + updateComponents", surface.A2UI)
	}
	if surface.A2UI[0].CreateSurface == nil || surface.A2UI[0].CreateSurface.SurfaceID != "s1" {
		t.Fatalf("first message = %+v, want createSurface s1", surface.A2UI[0])
	}
	if surface.A2UI[1].UpdateComponents == nil {
		t.Fatalf("second message = %+v, want updateComponents", surface.A2UI[1])
	}
}

func TestA2UIRepairNormalizationLeavesValidSurfaceUntouched(t *testing.T) {
	parts := a2uiPart(validSurfaceAlone)
	got := ensureRepairedCreateSurface(parts, "s1")
	if len(got) != 1 || got[0].Text != validSurfaceAlone {
		t.Fatalf("parts = %+v, want the valid surface unchanged", got)
	}
}

// SHOULD-FIX #417: the RenderA2UI gate is honored — a text-only surface never
// spends a repair turn even when its answer carries an invalid card.
func TestA2UIRepairHonorsRenderA2UIGate(t *testing.T) {
	state := &a2uiRepairState{}
	tracker := newStepTracker(10, 0)
	if _, ok := a2uiRepairFor(ExecuteRequest{RenderA2UI: false}, tracker, state, a2uiPart(invalidSurfaceAlone)); ok {
		t.Fatal("repair ran despite RenderA2UI=false")
	}
	if state.Attempted() {
		t.Fatal("state recorded an attempt despite the RenderA2UI gate")
	}
}

// BLOCKER 2 regression: an invalid card on the last permitted step must not
// issue a repair turn — that follow-up model call would trip the step limit and
// turn the completed answer into a paused run.
func TestA2UIRepairSkippedWhenNoStepRemains(t *testing.T) {
	// steps=2, max=3: the next model call would increment to 3 and pause.
	tracker := newStepTracker(3, 2)
	state := &a2uiRepairState{}
	if _, ok := a2uiRepairFor(ExecuteRequest{RenderA2UI: true}, tracker, state, a2uiPart(invalidSurfaceAlone)); ok {
		t.Fatal("repair ran with only one budget slot left; it would pause the run")
	}
	if !tracker.nextStepExceedsBudget() {
		t.Fatal("tracker should report the next step exceeds the budget")
	}
}

func TestA2UIRepairAllowedWithBudgetRemaining(t *testing.T) {
	// steps=1, max=3: the repair increments to 2, still under the limit.
	tracker := newStepTracker(3, 1)
	state := &a2uiRepairState{}
	if _, ok := a2uiRepairFor(ExecuteRequest{RenderA2UI: true}, tracker, state, a2uiPart(invalidSurfaceAlone)); !ok {
		t.Fatal("repair did not run despite a remaining step")
	}
}
