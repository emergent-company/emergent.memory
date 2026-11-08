package agents

import (
	"strings"
	"testing"

	"google.golang.org/genai"
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

func TestA2UIRepairPromptCarriesValidatorIssues(t *testing.T) {
	prompt := a2uiRepairPrompt(a2uiPart(invalidSurfaceAlone))
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

func TestA2UIRepairPromptIncludesInvalidJSON(t *testing.T) {
	prompt := a2uiRepairPrompt(a2uiPart("```a2ui\nnot json\n```"))
	if !strings.Contains(prompt, "invalid JSON") {
		t.Fatalf("prompt = %q, want an invalid-JSON issue", prompt)
	}
}

func TestA2UIRepairPromptSkipsWhenThereIsProse(t *testing.T) {
	// A card failure next to a good prose answer is not worth a second model call.
	parts := a2uiPart("Here is the answer.\n\n" + invalidSurfaceAlone)
	if got := a2uiRepairPrompt(parts); got != "" {
		t.Fatalf("prompt = %q, want empty when prose is present", got)
	}
}

func TestA2UIRepairPromptSkipsValidSurface(t *testing.T) {
	if got := a2uiRepairPrompt(a2uiPart(validSurfaceAlone)); got != "" {
		t.Fatalf("prompt = %q, want empty for a valid surface", got)
	}
}

func TestA2UIRepairPromptSkipsNoFence(t *testing.T) {
	if got := a2uiRepairPrompt(a2uiPart("plain answer")); got != "" {
		t.Fatalf("prompt = %q, want empty without a fence", got)
	}
	if got := a2uiRepairPrompt(nil); got != "" {
		t.Fatalf("prompt = %q, want empty for no parts", got)
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
}

func TestA2UIRepairStateOutcomeRepaired(t *testing.T) {
	state := &a2uiRepairState{}
	if _, ok := state.Next(a2uiPart(invalidSurfaceAlone)); !ok {
		t.Fatal("first Next() = false, want one repair")
	}
	if outcome := state.Outcome(a2uiPart(validSurfaceAlone)); outcome != "repaired" {
		t.Fatalf("Outcome() = %q, want repaired", outcome)
	}
}
