package graph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateMergePolicyRejectsUnknown pins #2231 item 3. Before this, an
// unrecognised preset fell through resolveMergePolicy's `!ok` branch to the
// legacy ConflictStrategy — empty for a typo'd policy — and then defaulted to
// "enrich". The merge executed with semantics the caller never asked for, on a
// destructive operation.
func TestValidateMergePolicyRejectsUnknown(t *testing.T) {
	for _, policy := range []string{"enriche", "Enrich", "no_sim", "enrich-no-sim", "overwrite"} {
		err := validateMergePolicy(policy, "")
		require.Error(t, err, "policy %q must be rejected", policy)
		require.Contains(t, err.Error(), "unknown merge policy")
		// The message has to say exactly what is valid, or the caller cannot fix
		// the call. Compared as one string: "enrich" is a substring of
		// "enrich_no_sim", so a per-name Contains would pass on a truncated list.
		require.Contains(t, err.Error(), strings.Join(MergePolicyNames(), ", "),
			"the error for %q must list exactly the accepted policies", policy)
	}
}

func TestValidateMergePolicyAcceptsEveryPreset(t *testing.T) {
	require.NotEmpty(t, mergePolicyPresets)
	for policy := range mergePolicyPresets {
		require.NoError(t, validateMergePolicy(policy, ""), "preset %q must validate", policy)
	}
	// No policy and no legacy strategy is the documented default, not an error.
	require.NoError(t, validateMergePolicy("", ""))
}

func TestValidateMergePolicyCoversLegacyConflictStrategy(t *testing.T) {
	for strategy := range mergeConflictStrategies {
		require.NoError(t, validateMergePolicy("", strategy), "legacy strategy %q must validate", strategy)
	}
	err := validateMergePolicy("", "preserve-target")
	require.Error(t, err, "an unrecognised legacy conflict_strategy must be rejected")
	require.Contains(t, err.Error(), "unknown conflict_strategy")
	require.Contains(t, err.Error(), strings.Join(MergeConflictStrategyNames(), ", "),
		"the error must list exactly the accepted strategies")

	// Policy takes precedence: a legacy value alongside a valid policy is ignored,
	// so it must not fail validation.
	require.NoError(t, validateMergePolicy("mine", "overwrite"))
}

// TestResolveMergePolicyServesWhatValidationAccepts keeps the table and the
// resolver in step: every accepted name must resolve to that preset's settings
// rather than to the fallback branch.
func TestResolveMergePolicyServesWhatValidationAccepts(t *testing.T) {
	for policy, want := range mergePolicyPresets {
		require.NoError(t, validateMergePolicy(policy, ""))
		got := resolveMergePolicy(policy, "", 0)
		require.Equal(t, want, got, "policy %q did not resolve to its preset", policy)
	}

	// The documented default.
	require.Equal(t, mergePolicyPresets["enrich"], resolveMergePolicy("", "", 0))

	// A threshold override applies on top of the preset, and is bounds-checked.
	got := resolveMergePolicy("enrich", "", 0.75)
	require.InDelta(t, 0.75, got.SimilarityThreshold, 1e-6)
	require.Equal(t, mergePolicyPresets["enrich"], resolveMergePolicy("enrich", "", 1.5),
		"an out-of-range threshold must be ignored rather than applied")
}
