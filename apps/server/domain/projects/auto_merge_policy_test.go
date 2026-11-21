package projects

import (
	"strings"
	"testing"
)

// TestNormalizeAutoMergePolicy pins the safe default: a row written before the
// column existed, or edited outside the API, must be read as "partial" and not
// as an unknown policy that could make auto-merge destructive (#1074).
func TestNormalizeAutoMergePolicy(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{AutoMergePolicyPartial, AutoMergePolicyPartial},
		{AutoMergePolicyEnrich, AutoMergePolicyEnrich},
		{"", AutoMergePolicyPartial},
		{"ENRICH", AutoMergePolicyPartial},
		{"overwrite", AutoMergePolicyPartial},
	}
	for _, tc := range cases {
		if got := NormalizeAutoMergePolicy(tc.in); got != tc.want {
			t.Errorf("NormalizeAutoMergePolicy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizeRequestedAutoMergePolicyRejectsTypos covers the write side: a
// typo must fail the request rather than silently keep parking every
// near-duplicate for review.
func TestNormalizeRequestedAutoMergePolicyRejectsTypos(t *testing.T) {
	got, err := normalizeRequestedAutoMergePolicy("  enrich  ")
	if err != nil {
		t.Fatalf("normalizeRequestedAutoMergePolicy(enrich) errored: %v", err)
	}
	if got != AutoMergePolicyEnrich {
		t.Errorf("trimmed policy = %q, want enrich", got)
	}

	_, err = normalizeRequestedAutoMergePolicy("enriched")
	if err == nil {
		t.Fatal("expected an error for an unknown policy")
	}
	if !strings.Contains(err.Error(), AutoMergePolicyPartial) || !strings.Contains(err.Error(), AutoMergePolicyEnrich) {
		t.Errorf("error %q should name the valid policies", err)
	}
}

// TestToDTOAlwaysEmitsAutoMergePolicy guards the settings surface: the column
// defaults to "partial", so an omitted field would read as an empty policy.
func TestToDTOAlwaysEmitsAutoMergePolicy(t *testing.T) {
	p := &Project{ID: "p1", OrganizationID: "o1", Name: "P"}
	if got := p.ToDTO().AutoMergeExtractionPolicy; got != AutoMergePolicyPartial {
		t.Fatalf("zero-value project policy = %q, want partial", got)
	}

	p.AutoMergeExtractionPolicy = AutoMergePolicyEnrich
	if got := p.ToDTO().AutoMergeExtractionPolicy; got != AutoMergePolicyEnrich {
		t.Fatalf("stored policy = %q, want enrich", got)
	}
}
