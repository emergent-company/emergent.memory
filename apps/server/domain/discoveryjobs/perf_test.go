package discoveryjobs

import (
	"strings"
	"testing"
)

func TestExtractionPromptsFingerprint(t *testing.T) {
	types := []DiscoveredType{
		{TypeName: "Person", Description: "A person"},
		{TypeName: "Organization", Description: "An org"},
	}
	rels := []DiscoveredRelationship{
		{SourceType: "Person", RelationType: "WORKS_FOR", TargetType: "Organization"},
	}

	base := extractionPromptsFingerprint(types, rels, "kb purpose")
	if base == "" {
		t.Fatal("fingerprint must not be empty")
	}
	if base != extractionPromptsFingerprint(types, rels, "kb purpose") {
		t.Fatal("fingerprint must be deterministic")
	}

	// Any input change must change the fingerprint.
	if base == extractionPromptsFingerprint(types, rels, "other purpose") {
		t.Fatal("kbPurpose change must change fingerprint")
	}
	changedTypes := []DiscoveredType{
		{TypeName: "Person", Description: "A person"},
		{TypeName: "Organization", Description: "changed desc"},
	}
	if base == extractionPromptsFingerprint(changedTypes, rels, "kb purpose") {
		t.Fatal("type description change must change fingerprint")
	}
	changedRels := []DiscoveredRelationship{
		{SourceType: "Person", RelationType: "OWNS", TargetType: "Organization"},
	}
	if base == extractionPromptsFingerprint(types, changedRels, "kb purpose") {
		t.Fatal("relationship change must change fingerprint")
	}

	// Order must not matter (sorted internally).
	reordered := []DiscoveredType{
		{TypeName: "Organization", Description: "An org"},
		{TypeName: "Person", Description: "A person"},
	}
	if base != extractionPromptsFingerprint(reordered, rels, "kb purpose") {
		t.Fatal("fingerprint must be order-independent over types")
	}
}

func TestBuildRelationshipDiscoveryPromptIsLean(t *testing.T) {
	s := &Service{}
	types := []DiscoveredType{
		{TypeName: "Person", Description: "A person", Properties: map[string]any{"name": map[string]any{}}},
	}
	prompt := s.buildRelationshipDiscoveryPrompt(types, "kb purpose")

	if strings.Contains(prompt, "cardinality: one-to-one") || strings.Contains(prompt, "- cardinality:") {
		t.Fatalf("relationship prompt must not request cardinality:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Do not include cardinality") {
		t.Fatalf("relationship prompt must explicitly forbid cardinality:\n%s", prompt)
	}
	if !strings.Contains(prompt, "at most 8 words") {
		t.Fatalf("relationship prompt must cap description length:\n%s", prompt)
	}
	if !strings.Contains(prompt, "confidence") {
		t.Fatalf("relationship prompt must keep confidence:\n%s", prompt)
	}
}
