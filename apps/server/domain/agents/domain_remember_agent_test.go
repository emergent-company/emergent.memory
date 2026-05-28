package agents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// buildDomainRememberToolPolicies
// ---------------------------------------------------------------------------

func TestBuildDomainRememberToolPolicies_Auto(t *testing.T) {
	policies := buildDomainRememberToolPolicies("auto")
	assert.Empty(t, policies, "auto policy should produce no tool restrictions")
}

func TestBuildDomainRememberToolPolicies_EmptyDefault(t *testing.T) {
	policies := buildDomainRememberToolPolicies("")
	assert.Empty(t, policies, "empty string should fall through to default (no restrictions)")
}

func TestBuildDomainRememberToolPolicies_Unknown(t *testing.T) {
	policies := buildDomainRememberToolPolicies("unknown_policy")
	assert.Empty(t, policies, "unknown policy should fall through to default (no restrictions)")
}

func TestBuildDomainRememberToolPolicies_Ask(t *testing.T) {
	policies := buildDomainRememberToolPolicies("ask")

	require.Contains(t, policies, "finalize-discovery")
	p := policies["finalize-discovery"]
	assert.True(t, p.Confirm, "ask policy must set Confirm=true on finalize-discovery")
	assert.False(t, p.Disabled, "ask policy must not disable finalize-discovery")
	assert.NotEmpty(t, p.Message, "ask policy must provide a confirmation message")
}

func TestBuildDomainRememberToolPolicies_ReuseOnly(t *testing.T) {
	policies := buildDomainRememberToolPolicies("reuse_only")

	require.Contains(t, policies, "finalize-discovery")
	p := policies["finalize-discovery"]
	assert.True(t, p.Disabled, "reuse_only policy must set Disabled=true on finalize-discovery")
	assert.False(t, p.Confirm, "reuse_only policy must not set Confirm (hard-block, not ask)")
}

// ---------------------------------------------------------------------------
// ToolPolicy sync change detection
// ---------------------------------------------------------------------------

func TestToolPolicySyncDetects_ConfirmToDisabled(t *testing.T) {
	// Simulates existing agent with ask policy being updated to reuse_only.
	curr := ToolPolicy{Confirm: true, Message: "approve?"}
	want := buildDomainRememberToolPolicies("reuse_only")["finalize-discovery"]

	changed := curr.Confirm != want.Confirm || curr.Disabled != want.Disabled
	assert.True(t, changed, "switching ask→reuse_only must trigger a policy sync")
}

func TestToolPolicySyncDetects_DisabledToConfirm(t *testing.T) {
	curr := ToolPolicy{Disabled: true}
	want := buildDomainRememberToolPolicies("ask")["finalize-discovery"]

	changed := curr.Confirm != want.Confirm || curr.Disabled != want.Disabled
	assert.True(t, changed, "switching reuse_only→ask must trigger a policy sync")
}

func TestToolPolicySyncDetects_NoChange_ReuseOnly(t *testing.T) {
	curr := ToolPolicy{Disabled: true}
	want := buildDomainRememberToolPolicies("reuse_only")["finalize-discovery"]

	changed := curr.Confirm != want.Confirm || curr.Disabled != want.Disabled
	assert.False(t, changed, "same policy must not trigger an unnecessary sync")
}

func TestToolPolicySyncDetects_NoChange_Auto(t *testing.T) {
	// auto produces empty map → want is zero-value ToolPolicy
	curr := ToolPolicy{}
	want := buildDomainRememberToolPolicies("auto")["finalize-discovery"] // zero value

	changed := curr.Confirm != want.Confirm || curr.Disabled != want.Disabled
	assert.False(t, changed, "auto→auto must not trigger sync")
}

// ---------------------------------------------------------------------------
// ToolPolicy JSON serialisation
// ---------------------------------------------------------------------------

func TestToolPolicy_JSONRoundTrip_Disabled(t *testing.T) {
	original := ToolPolicy{Disabled: true}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var got ToolPolicy
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, got.Disabled)
	assert.False(t, got.Confirm)
}

func TestToolPolicy_JSONRoundTrip_Confirm(t *testing.T) {
	original := ToolPolicy{Confirm: true, Message: "approve?"}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Disabled should be omitted from JSON when false (omitempty).
	assert.NotContains(t, string(data), `"disabled"`, "Disabled=false should be omitted via omitempty")

	var got ToolPolicy
	require.NoError(t, json.Unmarshal(data, &got))
	assert.True(t, got.Confirm)
	assert.Equal(t, "approve?", got.Message)
	assert.False(t, got.Disabled)
}

func TestToolPolicy_JSONRoundTrip_OldRowsLackDisabled(t *testing.T) {
	// Existing DB rows serialised before Disabled was added must unmarshal safely.
	oldJSON := `{"confirm":false}`
	var got ToolPolicy
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &got))
	assert.False(t, got.Disabled, "missing Disabled field must default to false")
}

func TestToolPolicy_DisabledZeroValue(t *testing.T) {
	var p ToolPolicy
	assert.False(t, p.Disabled, "zero value must be false (non-blocking)")
	assert.False(t, p.Confirm)
}
