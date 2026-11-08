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
