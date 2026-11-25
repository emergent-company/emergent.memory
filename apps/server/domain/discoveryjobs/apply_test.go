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
		{TypeName: "Relationship", Confidence: 0.9},    // relationship-like
		{TypeName: "Junk", Confidence: 0.4},            // below confidence
		{TypeName: "Mal)formed!Name", Confidence: 0.9}, // invalid type name
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
	if reasons["Mal)formed!Name"] != "invalid type name" {
		t.Fatalf("Malformed name reason = %q", reasons["Mal)formed!Name"])
	}
}

func TestValidTypeName(t *testing.T) {
	valid := []string{"Person", "Meeting Series", "DecisionOption", "Org-2", "A"}
	for _, s := range valid {
		if !validTypeName(s) {
			t.Fatalf("validTypeName(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "1st", "bad)name", "a;b", "😀", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, s := range invalid {
		if validTypeName(s) {
			t.Fatalf("validTypeName(%q) = true, want false", s)
		}
	}
}
