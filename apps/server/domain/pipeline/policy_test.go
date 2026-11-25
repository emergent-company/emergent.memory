package pipeline

import (
	"encoding/json"
	"testing"
)

func TestDefaultPolicyByMode(t *testing.T) {
	suggest := DefaultPolicy(ModeSuggest)
	if suggest.Mode != ModeSuggest {
		t.Fatalf("suggest mode = %q", suggest.Mode)
	}
	if suggest.Extract.ReExtractOnSchemaChange != "off" {
		t.Fatalf("suggest should not re-extract on schema change, got %q", suggest.Extract.ReExtractOnSchemaChange)
	}
	if suggest.Merge.Policy != "partial" {
		t.Fatalf("suggest merge policy = %q", suggest.Merge.Policy)
	}

	auto := DefaultPolicy(ModeAuto)
	if auto.Mode != ModeAuto {
		t.Fatalf("auto mode = %q", auto.Mode)
	}
	if auto.Extract.ReExtractOnSchemaChange != "incremental" {
		t.Fatalf("auto re-extract = %q, want incremental", auto.Extract.ReExtractOnSchemaChange)
	}
	if !auto.Apply.AdditiveOnly {
		t.Fatal("auto must be additive-only")
	}

	if got := DefaultPolicy(ModeOff).Mode; got != ModeOff {
		t.Fatalf("off mode = %q", got)
	}
	if got := DefaultPolicy("bogus").Mode; got != ModeSuggest {
		t.Fatalf("unknown mode should fall back to suggest, got %q", got)
	}
}

func TestValidate(t *testing.T) {
	good := DefaultPolicy(ModeAuto)
	if err := Validate(good); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	bad := good
	bad.Mode = "wat"
	if err := Validate(bad); err == nil {
		t.Fatal("expected invalid mode to fail")
	}

	bad = good
	bad.Discovery.MinConfidence = 1.5
	if err := Validate(bad); err == nil {
		t.Fatal("expected out-of-range confidence to fail")
	}

	bad = good
	bad.Extract.ReExtractOnSchemaChange = "sometimes"
	if err := Validate(bad); err == nil {
		t.Fatal("expected bad re-extract enum to fail")
	}

	bad = good
	bad.Merge.Policy = "nuke"
	if err := Validate(bad); err == nil {
		t.Fatal("expected bad merge policy to fail")
	}

	bad = good
	bad.Apply.Target = "meetings"
	if err := Validate(bad); err == nil {
		t.Fatal("expected non-auto_pack target to fail")
	}
}

func TestParseRoundTrip(t *testing.T) {
	p := DefaultPolicy(ModeAuto)
	raw := p.Marshal()
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !got.Equal(p) {
		t.Fatalf("round trip mismatch:\n got=%+v\n want=%+v", got, p)
	}
}

func TestZeroPolicy(t *testing.T) {
	if !(Policy{}).IsZero() {
		t.Fatal("empty policy must be zero")
	}
	if DefaultPolicy(ModeSuggest).IsZero() {
		t.Fatal("default policy must not be zero")
	}

	got, err := Parse(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Parse {}: %v", err)
	}
	if !got.IsZero() {
		t.Fatal(`Parse("{}") must be zero`)
	}

	got, err = Parse(nil)
	if err != nil {
		t.Fatalf("Parse nil: %v", err)
	}
	if !got.IsZero() {
		t.Fatal("Parse(nil) must be zero")
	}
}
