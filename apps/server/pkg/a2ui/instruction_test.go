package a2ui

import (
	"strings"
	"testing"
)

// TestCatalogInstruction pins the "prompt and validator cannot drift" contract:
// the instruction rendered for the prompt is generated from the same BasicCatalog
// map the validator reads. Every catalog id, its purpose, and its required props
// must be present; the fence and catalogId must be spelled out; and the output
// must be deterministic.
func TestCatalogInstruction(t *testing.T) {
	got := BasicCatalog.Instruction()

	if !strings.HasPrefix(got, InstructionPrefix) {
		t.Fatalf("Instruction() does not start with the dedupe prefix: %q", got)
	}
	if !strings.Contains(got, fenceOpen) {
		t.Fatalf("Instruction() does not describe the fenced %s block: %q", fenceOpen, got)
	}
	if !strings.Contains(got, CatalogID) {
		t.Fatalf("Instruction() does not name the catalog id %q: %q", CatalogID, got)
	}

	// The JSON examples must be copy-paste-valid (issue #414 review nit): no
	// literal "..." and no angle-bracket placeholders that a verbatim copy would
	// fail to validate and then be silently stripped to an empty reply.
	for _, bad := range []string{"...", "<id>", "<name>"} {
		if strings.Contains(got, bad) {
			t.Fatalf("Instruction() JSON example contains %q (not copy-paste-valid): %q", bad, got)
		}
	}

	for id, spec := range BasicCatalog {
		if !strings.Contains(got, "- "+id+": ") {
			t.Fatalf("Instruction() does not list component %q as a catalog line: %q", id, got)
		}
		if spec.Purpose == "" {
			t.Fatalf("catalog component %q has an empty purpose; the prompt would be silent on it", id)
		}
		if !strings.Contains(got, spec.Purpose) {
			t.Fatalf("Instruction() omits the purpose %q for component %q: %q", spec.Purpose, id, got)
		}
		for _, prop := range spec.Required {
			if !strings.Contains(got, prop) {
				t.Fatalf("Instruction() omits required prop %q for component %q: %q", prop, id, got)
			}
		}
	}

	// Deterministic: sorted by id, so two renders are byte-identical.
	if again := BasicCatalog.Instruction(); again != got {
		t.Fatalf("Instruction() is not deterministic:\nfirst:  %q\nsecond: %q", got, again)
	}
}

// TestBasicCatalogIDs pins the exact set of component ids the catalog exposes.
// Adding, removing, or renaming a component changes the model-facing contract
// (and the validator), so this must be updated deliberately — it is the guard
// that makes a catalog change without an intentional instruction/prop review
// fail loudly (issue #414 review nit).
func TestBasicCatalogIDs(t *testing.T) {
	want := map[string]bool{
		"proposal":    true,
		"approval":    true,
		"question":    true,
		"code":        true,
		"entity":      true,
		"object-form": true,
		"todo":        true,
		"result":      true,
		"sources":     true,
	}
	if len(BasicCatalog) != len(want) {
		t.Fatalf("BasicCatalog has %d components, want %d (a catalog change needs an intentional test update)", len(BasicCatalog), len(want))
	}
	for id := range BasicCatalog {
		if !want[id] {
			t.Fatalf("BasicCatalog exposes unexpected component %q", id)
		}
	}
	for id := range want {
		if !BasicCatalog.Contains(id) {
			t.Fatalf("BasicCatalog is missing component %q", id)
		}
	}
}
