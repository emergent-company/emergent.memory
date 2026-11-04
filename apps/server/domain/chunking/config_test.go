package chunking

import (
	"context"
	"testing"
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
