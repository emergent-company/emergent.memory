package chunking

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/textsplitter"
)

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }

func TestValidateChunkingConfigDefaults(t *testing.T) {
	strategy, size, overlap, parentChild, err := validateChunkingConfig(UpdateChunkingConfigRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != "recursive_character" || size != 1000 || overlap != 200 || parentChild {
		t.Fatalf("unexpected defaults: strategy=%s size=%d overlap=%d parentChild=%v", strategy, size, overlap, parentChild)
	}
}

func TestValidateChunkingConfigValidExplicit(t *testing.T) {
	strategy, size, overlap, parentChild, err := validateChunkingConfig(UpdateChunkingConfigRequest{
		Strategy:     strPtr("markdown_structure"),
		MaxChunkSize: intPtr(512),
		Overlap:      intPtr(64),
		ParentChild:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strategy != "markdown_structure" || size != 512 || overlap != 64 || !parentChild {
		t.Fatalf("unexpected result: strategy=%s size=%d overlap=%d parentChild=%v", strategy, size, overlap, parentChild)
	}
}

func TestValidateChunkingConfigRejectsUnknownStrategy(t *testing.T) {
	if _, _, _, _, err := validateChunkingConfig(UpdateChunkingConfigRequest{Strategy: strPtr("nope")}); err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestValidateChunkingConfigRejectsOutOfRange(t *testing.T) {
	if _, _, _, _, err := validateChunkingConfig(UpdateChunkingConfigRequest{MaxChunkSize: intPtr(10)}); err == nil {
		t.Fatal("expected error for too-small size")
	}
	if _, _, _, _, err := validateChunkingConfig(UpdateChunkingConfigRequest{MaxChunkSize: intPtr(999999)}); err == nil {
		t.Fatal("expected error for too-large size")
	}
	if _, _, _, _, err := validateChunkingConfig(UpdateChunkingConfigRequest{MaxChunkSize: intPtr(500), Overlap: intPtr(500)}); err == nil {
		t.Fatal("expected error when overlap >= size")
	}
	if _, _, _, _, err := validateChunkingConfig(UpdateChunkingConfigRequest{Overlap: intPtr(-1)}); err == nil {
		t.Fatal("expected error for negative overlap")
	}
}

func TestValidateChunkingConfigRejectsParentChildWithRecursive(t *testing.T) {
	if _, _, _, _, err := validateChunkingConfig(UpdateChunkingConfigRequest{ParentChild: boolPtr(true)}); err == nil {
		t.Fatal("expected error for parent-child with recursive strategy")
	}
}

func TestUpdateChunkingConfigRejectsInvalidBeforeDB(t *testing.T) {
	svc := &Service{} // no db: validation must fail before any query
	_, err := svc.UpdateChunkingConfig(context.Background(), "00000000-0000-0000-0000-000000000000", UpdateChunkingConfigRequest{Strategy: strPtr("bogus")})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateChunkingUpdateAutoProfileSkipsParentChildRule(t *testing.T) {
	// The project store keeps whatever strategy was last written; with
	// auto-profile on it is unused, so parentChild=true must not be rejected
	// for a recursive strategy.
	_, _, _, _, autoProfile, err := validateChunkingUpdate(UpdateChunkingConfigRequest{
		ParentChild: boolPtr(true),
		AutoProfile: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("auto-profile must not enforce the parent-child rule: %v", err)
	}
	if !autoProfile {
		t.Fatal("autoProfile not parsed from the request")
	}

	// Without the opt-in the rule still applies.
	if _, _, _, _, _, err := validateChunkingUpdate(UpdateChunkingConfigRequest{ParentChild: boolPtr(true)}); err == nil {
		t.Fatal("expected the parent-child rule to still apply without auto-profile")
	}
}

func TestValidateChunkingUpdateAutoProfileAbsentMeansOff(t *testing.T) {
	_, _, _, _, autoProfile, err := validateChunkingUpdate(UpdateChunkingConfigRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if autoProfile {
		t.Fatal("autoProfile must default to false so existing projects keep their behaviour")
	}
	if _, _, _, _, autoProfile, err := validateChunkingUpdate(UpdateChunkingConfigRequest{AutoProfile: boolPtr(false)}); err != nil || autoProfile {
		t.Fatalf("explicit false must stay off (autoProfile=%v err=%v)", autoProfile, err)
	}
}

func TestShapeProfilesCoverEveryShape(t *testing.T) {
	profiles := shapeProfiles()
	if len(profiles) != len(textsplitter.SupportedShapes()) {
		t.Fatalf("shapeProfiles() has %d entries, want %d", len(profiles), len(textsplitter.SupportedShapes()))
	}
	for i, p := range profiles {
		want := textsplitter.SupportedShapes()[i]
		if p.Shape != string(want) {
			t.Fatalf("profile %d shape = %q, want %q", i, p.Shape, want)
		}
		if !textsplitter.IsSupportedStrategy(p.Strategy) {
			t.Errorf("%s: unknown strategy %q", p.Shape, p.Strategy)
		}
		if p.MaxChunkSize < minChunkSize || p.MaxChunkSize > maxChunkSize {
			t.Errorf("%s: chunk size %d outside API bounds", p.Shape, p.MaxChunkSize)
		}
		if p.Overlap < 0 || p.Overlap >= p.MaxChunkSize {
			t.Errorf("%s: overlap %d invalid for size %d", p.Shape, p.Overlap, p.MaxChunkSize)
		}
	}
}
