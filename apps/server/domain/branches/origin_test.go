package branches

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeCreateOrigin pins the create-time attribution rules: empty means
// manual, values are trimmed, and an unknown origin is rejected rather than
// stored (a typo would hide a branch from an origin filter).
func TestNormalizeCreateOrigin(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to manual", in: "", want: OriginManual},
		{name: "whitespace defaults to manual", in: "   ", want: OriginManual},
		{name: "extraction", in: OriginExtraction, want: OriginExtraction},
		{name: "agent", in: OriginAgent, want: OriginAgent},
		{name: "manual", in: OriginManual, want: OriginManual},
		{name: "system", in: OriginSystem, want: OriginSystem},
		{name: "trimmed", in: "  agent  ", want: OriginAgent},
		{name: "unknown rejected", in: "scheduled", wantErr: true},
		{name: "case sensitive", in: "Extraction", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeCreateOrigin(&CreateBranchRequest{Origin: tc.in})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestValidOrigin keeps the stored vocabulary closed: the migration's CHECK
// constraint accepts exactly these four values.
func TestValidOrigin(t *testing.T) {
	for _, origin := range []string{OriginExtraction, OriginAgent, OriginManual, OriginSystem} {
		assert.True(t, ValidOrigin(origin), origin)
	}
	for _, origin := range []string{"", "extractions", "Extraction", "root"} {
		assert.False(t, ValidOrigin(origin), origin)
	}
}

// TestToResponseOrigin covers the legacy-row default and the attribution
// passthrough (origin_id/origin_label are what the UI labels a branch with).
func TestToResponseOrigin(t *testing.T) {
	legacy := ToResponse(&Branch{ID: "b1", Name: "old-branch"})
	require.NotNil(t, legacy)
	assert.Equal(t, OriginManual, legacy.Origin, "a row predating the column reads as manual")
	assert.Nil(t, legacy.OriginID)
	assert.Nil(t, legacy.OriginLabel)

	jobID := "11111111-1111-1111-1111-111111111111"
	label := "Invoice.pdf"
	resp := ToResponse(&Branch{
		ID:          "b2",
		Name:        "extraction/aaaa/bbbb",
		Origin:      OriginExtraction,
		OriginID:    &jobID,
		OriginLabel: &label,
	})
	require.NotNil(t, resp)
	assert.Equal(t, OriginExtraction, resp.Origin)
	assert.Equal(t, &jobID, resp.OriginID)
	assert.Equal(t, &label, resp.OriginLabel)
}

// TestTrimHelpers pins the blank-to-NULL rule and rune-safe label truncation.
func TestTrimHelpers(t *testing.T) {
	assert.Nil(t, trimOptional(nil))
	assert.Nil(t, trimOptional(strPtr("   ")))
	assert.Equal(t, "value", *trimOptional(strPtr("  value  ")))

	assert.Nil(t, trimLabel(nil, 10))
	assert.Nil(t, trimLabel(strPtr(" "), 10))
	assert.Equal(t, "short", *trimLabel(strPtr(" short "), 10))
	// Truncation is by rune, so a multibyte filename cannot be cut mid-character.
	long := strings.Repeat("ż", 20)
	got := trimLabel(&long, 5)
	require.NotNil(t, got)
	assert.Equal(t, strings.Repeat("ż", 5), *got)
	assert.Len(t, []rune(*got), 5)
}

func strPtr(s string) *string { return &s }
