package main

import (
	"context"
	"testing"
)

// TestEnsureBlueprintApplied_FreshInstall verifies a fresh install creates a
// draft, publishes it, and applies it.
func TestEnsureBlueprintApplied_FreshInstall(t *testing.T) {
	f := &fakeMemory{
		blueprint: &BlueprintRecord{ID: "bp-new", Status: "draft"},
	}
	s := &Server{memory: f}
	bp := &BundledBlueprint{
		Name:    "operator",
		Version: "1.0.0",
		Author:  "memory",
		Agents:  []BundledAgent{{Name: "operator", SystemPrompt: "assist"}},
	}

	if _, err := s.ensureBlueprintApplied(context.Background(), bp); err != nil {
		t.Fatalf("ensureBlueprintApplied: %v", err)
	}
	if f.createdBlueprint == nil {
		t.Fatal("expected CreateBlueprint to be called")
	}
	if f.createdBlueprint.Name != "operator" || f.createdBlueprint.Version != "1.0.0" {
		t.Errorf("create request = %+v", f.createdBlueprint)
	}
	if len(f.publishedBlueprint) != 1 || f.publishedBlueprint[0] != "bp-new" {
		t.Errorf("published = %v, want [bp-new]", f.publishedBlueprint)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-new" {
		t.Errorf("applied = %v, want [bp-new]", f.appliedBlueprint)
	}
}

// TestEnsureBlueprintApplied_ExistingVersion verifies a re-install of an already
// published version skips create+publish and only applies.
func TestEnsureBlueprintApplied_ExistingVersion(t *testing.T) {
	f := &fakeMemory{
		blueprintVersions: []BlueprintRecord{{ID: "bp-existing", Version: "1.0.0", Status: "published"}},
		blueprint:         &BlueprintRecord{ID: "bp-existing", Status: "published"},
	}
	s := &Server{memory: f}
	bp := &BundledBlueprint{Name: "operator", Version: "1.0.0", Agents: []BundledAgent{{Name: "operator"}}}

	if _, err := s.ensureBlueprintApplied(context.Background(), bp); err != nil {
		t.Fatalf("ensureBlueprintApplied: %v", err)
	}
	if f.createdBlueprint != nil {
		t.Errorf("expected no create; got %+v", f.createdBlueprint)
	}
	if len(f.publishedBlueprint) != 0 {
		t.Errorf("expected no publish; got %v", f.publishedBlueprint)
	}
	if len(f.appliedBlueprint) != 1 || f.appliedBlueprint[0] != "bp-existing" {
		t.Errorf("applied = %v, want [bp-existing]", f.appliedBlueprint)
	}
}
