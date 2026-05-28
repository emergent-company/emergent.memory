package agents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ToolPolicy struct — unit tests
// These tests focus on the ToolPolicy type itself: zero values, JSON
// serialisation, and the semantics of the Disabled/Confirm fields.
// Executor integration (beforeToolCb) is covered by the integration TODO.
// ---------------------------------------------------------------------------

func TestToolPolicy_ZeroValue_IsPermissive(t *testing.T) {
	var p ToolPolicy
	assert.False(t, p.Confirm, "zero-value ToolPolicy must not require confirmation")
	assert.False(t, p.Disabled, "zero-value ToolPolicy must not disable the tool")
	assert.Empty(t, p.Message)
}

func TestToolPolicy_Disabled_DoesNotImplyConfirm(t *testing.T) {
	p := ToolPolicy{Disabled: true}
	assert.True(t, p.Disabled)
	assert.False(t, p.Confirm, "Disabled and Confirm are independent fields")
}

func TestToolPolicy_Confirm_DoesNotImplyDisabled(t *testing.T) {
	p := ToolPolicy{Confirm: true, Message: "ok?"}
	assert.True(t, p.Confirm)
	assert.False(t, p.Disabled)
}

func TestToolPolicy_BothCanBeSet(t *testing.T) {
	// Unusual but structurally valid; Disabled is checked first in executor.
	p := ToolPolicy{Confirm: true, Disabled: true, Message: "x"}
	assert.True(t, p.Confirm)
	assert.True(t, p.Disabled)
}

// ---------------------------------------------------------------------------
// JSON serialisation
// ---------------------------------------------------------------------------

func TestToolPolicy_Marshal_Disabled(t *testing.T) {
	p := ToolPolicy{Disabled: true}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	s := string(data)
	assert.Contains(t, s, `"disabled":true`)
	// confirm is false — not omitempty so it will be present
	assert.Contains(t, s, `"confirm":false`)
}

func TestToolPolicy_Marshal_Confirm_OmitsDisabled(t *testing.T) {
	p := ToolPolicy{Confirm: true, Message: "approve?"}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	s := string(data)
	// Disabled=false is omitempty — must be absent to keep serialisation lean.
	assert.NotContains(t, s, `"disabled"`, "Disabled=false must be omitted (omitempty)")
	assert.Contains(t, s, `"confirm":true`)
	assert.Contains(t, s, `"message":"approve?"`)
}

func TestToolPolicy_Marshal_AllZero_OmitsOptional(t *testing.T) {
	p := ToolPolicy{}
	data, err := json.Marshal(p)
	require.NoError(t, err)

	s := string(data)
	assert.NotContains(t, s, `"disabled"`)
	assert.NotContains(t, s, `"message"`)
}

func TestToolPolicy_Unmarshal_MissingDisabled_DefaultsFalse(t *testing.T) {
	// Old DB rows without "disabled" field must not break on unmarshal.
	raw := `{"confirm":true,"message":"old row"}`
	var p ToolPolicy
	require.NoError(t, json.Unmarshal([]byte(raw), &p))
	assert.True(t, p.Confirm)
	assert.Equal(t, "old row", p.Message)
	assert.False(t, p.Disabled, "absent disabled field must default to false")
}

func TestToolPolicy_Unmarshal_RoundTrip_Disabled(t *testing.T) {
	original := ToolPolicy{Disabled: true}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var got ToolPolicy
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, original.Disabled, got.Disabled)
	assert.Equal(t, original.Confirm, got.Confirm)
}

// ---------------------------------------------------------------------------
// ToolPolicies map lookup semantics
// ---------------------------------------------------------------------------

func TestToolPolicies_MissingKey_ZeroValue(t *testing.T) {
	policies := map[string]ToolPolicy{
		"some-tool": {Confirm: true},
	}
	// Looking up a key that doesn't exist returns zero-value (not blocking).
	p, ok := policies["other-tool"]
	assert.False(t, ok)
	assert.False(t, p.Disabled)
	assert.False(t, p.Confirm)
}

func TestToolPolicies_DisabledCheck_Pattern(t *testing.T) {
	// Mirrors the executor's exact lookup pattern for Disabled.
	toolName := "finalize-discovery"
	policies := map[string]ToolPolicy{
		toolName: {Disabled: true},
	}

	policy, ok := policies[toolName]
	blocked := ok && policy.Disabled
	assert.True(t, blocked, "executor pattern must correctly detect Disabled=true")
}

func TestToolPolicies_ConfirmCheck_Pattern(t *testing.T) {
	toolName := "finalize-discovery"
	policies := map[string]ToolPolicy{
		toolName: {Confirm: true, Message: "approve?"},
	}

	policy, ok := policies[toolName]
	requiresConfirm := ok && policy.Confirm
	assert.True(t, requiresConfirm)
	assert.False(t, policy.Disabled)
}
