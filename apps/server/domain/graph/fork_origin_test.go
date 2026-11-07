package graph

import (
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/branchorigin"
)

// TestApplyAttribution pins the fork path's attribution wiring: Resolve's
// normalised origin, typed id and label land on the branch, and an absent id
// leaves OriginID nil rather than pointing at uuid.Nil (which is not a valid
// reference and would break the uuid foreign-key-less join in cleanup).
func TestApplyAttribution(t *testing.T) {
	jobID := uuid.New()
	label := "Invoice.pdf"
	attribution, err := branchorigin.Resolve(&branchorigin.Attribution{
		Origin: branchorigin.Extraction,
		ID:     func() *string { v := jobID.String(); return &v }(),
		Label:  &label,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var b Branch
	applyAttribution(&b, attribution)

	if b.Origin != branchorigin.Extraction {
		t.Errorf("Origin = %q, want %q", b.Origin, branchorigin.Extraction)
	}
	if b.OriginID == nil || *b.OriginID != jobID {
		t.Errorf("OriginID = %v, want %s", b.OriginID, jobID)
	}
	if b.OriginLabel == nil || *b.OriginLabel != label {
		t.Errorf("OriginLabel = %v, want %q", b.OriginLabel, label)
	}

	// An unattributed fork is manual and carries no id.
	manual, err := branchorigin.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve nil: %v", err)
	}
	var plain Branch
	applyAttribution(&plain, manual)
	if plain.Origin != branchorigin.Manual {
		t.Errorf("Origin = %q, want %q", plain.Origin, branchorigin.Manual)
	}
	if plain.OriginID != nil {
		t.Errorf("OriginID = %v, want nil", plain.OriginID)
	}
}
