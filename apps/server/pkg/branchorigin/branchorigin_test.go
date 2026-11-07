package branchorigin

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveDefaults pins the default and the closed vocabulary: a caller that
// does not attribute itself is manual, whitespace is trimmed, and an origin
// outside the four stored values is rejected rather than stored.
func TestResolveDefaults(t *testing.T) {
	for _, in := range []*Attribution{nil, {}, {Origin: ""}, {Origin: "   "}} {
		got, err := Resolve(in)
		require.NoError(t, err)
		assert.Equal(t, Manual, got.Origin)
		assert.Nil(t, got.ID)
		assert.Nil(t, got.Label)
	}

	for _, origin := range []string{Extraction, Agent, Manual, System} {
		got, err := Resolve(&Attribution{Origin: origin})
		require.NoError(t, err)
		assert.Equal(t, origin, got.Origin)
	}

	for _, origin := range []string{"scheduled", "Extraction", "extractions"} {
		_, err := Resolve(&Attribution{Origin: origin})
		assert.ErrorIs(t, err, ErrInvalidOrigin, origin)
	}
}

// TestResolveID covers the id contract: trimmed, canonicalised, parsed into the
// typed form, and a malformed id rejected as a client error rather than left to
// fail on insert against a uuid column.
func TestResolveID(t *testing.T) {
	raw := strings.ToUpper(uuid.NewString())
	got, err := Resolve(&Attribution{Origin: Agent, ID: strPtr("  " + raw + "  ")})
	require.NoError(t, err)
	require.NotNil(t, got.ID)
	assert.Equal(t, strings.ToLower(raw), *got.ID, "the id is canonicalised")
	assert.Equal(t, uuid.MustParse(raw), got.ParsedID)

	blank, err := Resolve(&Attribution{ID: strPtr("   ")})
	require.NoError(t, err)
	assert.Nil(t, blank.ID, "a blank id stores NULL, not ''")
	assert.Equal(t, uuid.Nil, blank.ParsedID)

	for _, bad := range []string{"not-a-uuid", "123", "11111111-1111-1111-1111-11111111111z"} {
		_, err := Resolve(&Attribution{Origin: Extraction, ID: &bad})
		assert.ErrorIs(t, err, ErrInvalidID, bad)
	}
}

// TestResolveLabel pins the display-text rules: trimmed, blank becomes nil, and
// truncation is by rune so a multibyte filename is never cut mid-character.
func TestResolveLabel(t *testing.T) {
	none, err := Resolve(nil)
	require.NoError(t, err)
	assert.Nil(t, none.Label)

	blank, err := Resolve(&Attribution{Label: strPtr("   ")})
	require.NoError(t, err)
	assert.Nil(t, blank.Label)

	short, err := Resolve(&Attribution{Label: strPtr("  Invoice.pdf  ")})
	require.NoError(t, err)
	require.NotNil(t, short.Label)
	assert.Equal(t, "Invoice.pdf", *short.Label)

	long := strings.Repeat("ż", MaxLabelRunes+20)
	truncated, err := Resolve(&Attribution{Label: &long})
	require.NoError(t, err)
	require.NotNil(t, truncated.Label)
	assert.Len(t, []rune(*truncated.Label), MaxLabelRunes)
	assert.Equal(t, strings.Repeat("ż", MaxLabelRunes), *truncated.Label)
}

func TestValid(t *testing.T) {
	for _, origin := range []string{Extraction, Agent, Manual, System} {
		assert.True(t, Valid(origin), origin)
	}
	for _, origin := range []string{"", "extractions", "Extraction", "root"} {
		assert.False(t, Valid(origin), origin)
	}
}

func strPtr(s string) *string { return &s }
