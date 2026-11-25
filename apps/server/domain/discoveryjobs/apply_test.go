package discoveryjobs

import (
	"testing"

	"github.com/emergent-company/emergent.memory/domain/pipeline"
)

func TestGateApplyTypes(t *testing.T) {
	pol := pipeline.DefaultPolicy(pipeline.ModeAuto) // min_confidence 0.75

	types := []DiscoveredType{
		{TypeName: "Person", Confidence: 0.9},
		{TypeName: "Organization", Confidence: 0.8},
		{TypeName: "Relationship", Confidence: 0.9}, // relationship-like
		{TypeName: "Junk", Confidence: 0.4},         // below confidence
	}

	accepted, decisions := gateApplyTypes(pol, types)

	if len(accepted) != 2 || accepted[0] != "Person" || accepted[1] != "Organization" {
		t.Fatalf("accepted = %v, want [Person Organization]", accepted)
	}

	reasons := map[string]string{}
	for _, d := range decisions {
		if !d.Applied {
			reasons[d.Type] = d.Reason
		}
	}
	if reasons["Relationship"] != "relationship-like type" {
		t.Fatalf("Relationship reason = %q", reasons["Relationship"])
	}
	if reasons["Junk"] != "below min_confidence" {
		t.Fatalf("Junk reason = %q", reasons["Junk"])
	}
}
