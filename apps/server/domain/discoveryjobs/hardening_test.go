package discoveryjobs

import "testing"

func TestFilterRelationshipsByConfidence(t *testing.T) {
	rels := []DiscoveredRelationship{
		{SourceType: "Person", RelationType: "WORKS_FOR", TargetType: "Organization", Confidence: 0.95},
		{SourceType: "Person", RelationType: "OWNS", TargetType: "Product", Confidence: 0.4},
		{SourceType: "Organization", RelationType: "HAS_SITE", TargetType: "Site", Confidence: 0.75},
		{SourceType: "Person", RelationType: "MARRIED_TO", TargetType: "Person", Confidence: 0.1},
	}

	out := filterRelationshipsByConfidence(rels, 0.75)

	if len(out) != 2 {
		t.Fatalf("filtered = %d relationships, want 2", len(out))
	}
	for _, r := range out {
		if r.Confidence < 0.75 {
			t.Fatalf("relationship %s has confidence %v below threshold, should be dropped", r.RelationType, r.Confidence)
		}
	}
}

func TestFilterRelationshipsByConfidenceEmpty(t *testing.T) {
	if got := filterRelationshipsByConfidence(nil, 0.75); len(got) != 0 {
		t.Fatalf("nil input should yield empty, got %v", got)
	}
}
