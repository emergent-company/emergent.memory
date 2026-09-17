package agents

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Group policy resolution (effectiveToolPolicy) ---

func TestEffectiveToolPolicy_ExplicitBeatsGroup(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Disabled: true},
			"entity-delete":      {Confirm: true},
		},
	}
	p, ok := d.effectiveToolPolicy("entity-delete")
	require.True(t, ok)
	assert.True(t, p.Confirm, "explicit entry must win over the group policy")
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicy_GroupBeatsDefault(t *testing.T) {
	d := &AgentDefinition{
		DefaultToolPolicy: ToolPolicyDefaultDeny,
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Confirm: true},
		},
	}
	p, ok := d.effectiveToolPolicy("entity-delete")
	require.True(t, ok)
	assert.True(t, p.Confirm, "group policy must win over the default")
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicy_GroupAbsentFallsToDefault(t *testing.T) {
	d := &AgentDefinition{
		DefaultToolPolicy: ToolPolicyDefaultDeny,
		ToolPolicies: map[string]ToolPolicy{
			"@group:schema-migrate": {Confirm: true},
		},
	}
	// entity-delete belongs to graph-write, which has no stored group policy.
	p, ok := d.effectiveToolPolicy("entity-delete")
	require.True(t, ok)
	assert.True(t, p.Disabled, "default deny must apply when no group policy exists")
}

func TestEffectiveToolPolicy_GroupConfirm(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Confirm: true, Message: "really?"},
		},
	}
	p, ok := d.effectiveToolPolicy("entity-create")
	require.True(t, ok)
	assert.True(t, p.Confirm)
	assert.Equal(t, "really?", p.Message)
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicy_GroupDisabled(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:schema-migrate": {Disabled: true},
		},
	}
	p, ok := d.effectiveToolPolicy("schema-migrate-execute")
	require.True(t, ok)
	assert.True(t, p.Disabled)
}

func TestEffectiveToolPolicy_EmptyGroupEntryIsAllow(t *testing.T) {
	// A stored `{}` group entry resolves to (zero ToolPolicy, true): it is a
	// stored "allow" that does not confirm or disable, mirroring a per-tool `{}`.
	d := &AgentDefinition{
		DefaultToolPolicy: ToolPolicyDefaultDeny,
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {},
		},
	}
	p, ok := d.effectiveToolPolicy("entity-delete")
	require.True(t, ok)
	assert.Equal(t, ToolPolicy{}, p)
	assert.False(t, p.Confirm)
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicy_OtherToolFallsToDefault(t *testing.T) {
	// A tool in the "other" group with no stored group entry falls to default.
	d := &AgentDefinition{
		DefaultToolPolicy: ToolPolicyDefaultAsk,
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Disabled: true},
		},
	}
	p, ok := d.effectiveToolPolicy("totally-unknown-tool")
	require.True(t, ok)
	assert.True(t, p.Confirm, "default ask must apply to an ungrouped tool")
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicy_OtherGroupPolicyApplies(t *testing.T) {
	// A stored @group:other policy governs otherwise-unmatched tools.
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:other": {Disabled: true},
		},
	}
	p, ok := d.effectiveToolPolicy("totally-unknown-tool")
	require.True(t, ok)
	assert.True(t, p.Disabled)
}

// --- Executor boundary: a group-disabled tool is rejected before execution ---

// TestGroupDisabledRejectedBeforeExecution mirrors the exact disabled-check
// pattern in executor.go beforeToolCb: resolve effectiveToolPolicy, then block
// when hasPolicy && policy.Disabled. A tool covered only by a group
// {disabled:true} must be rejected without executing.
func TestGroupDisabledRejectedBeforeExecution(t *testing.T) {
	def := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Disabled: true},
		},
	}

	policy, hasPolicy := def.effectiveToolPolicy("entity-delete")
	blocked := hasPolicy && policy.Disabled
	assert.True(t, blocked, "group-disabled tool must be blocked before execution")
}

func TestGroupConfirmRequiresApprovalBeforeExecution(t *testing.T) {
	def := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Confirm: true},
		},
	}

	policy, hasPolicy := def.effectiveToolPolicy("entity-delete")
	requiresConfirm := hasPolicy && policy.Confirm
	assert.True(t, requiresConfirm, "group-confirm tool must require approval")
	assert.False(t, policy.Disabled)
}

// --- Catalog-aware resolution (enforcement parity with the read DTO) ---

// TestEffectiveToolPolicyFor_DynamicAsk proves a dynamic tool (whose scope is
// only known from the catalog, not mcp.LookupToolScope) is governed by its
// scope-derived group policy — the enforcement chokepoint must intercept it,
// not merely report it in the DTO.
func TestEffectiveToolPolicyFor_DynamicAsk(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:documents": {Confirm: true},
		},
	}
	p, ok := d.effectiveToolPolicyFor("document-create", "documents:write")
	require.True(t, ok)
	assert.True(t, p.Confirm, "dynamic tool with @group:documents ask must require approval")
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicyFor_DynamicDeny(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:documents": {Disabled: true},
		},
	}
	p, ok := d.effectiveToolPolicyFor("document-create", "documents:write")
	require.True(t, ok)
	assert.True(t, p.Disabled, "dynamic tool with @group:documents deny must be unavailable")
}

// TestEffectiveToolPolicyFor_StaticUnchanged proves the catalog-aware resolver
// and the catalog-less wrapper agree for static tools.
func TestEffectiveToolPolicyFor_StaticUnchanged(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write": {Disabled: true},
		},
	}
	p, ok := d.effectiveToolPolicyFor("entity-delete", "graph:write")
	require.True(t, ok)
	assert.True(t, p.Disabled)

	p2, ok2 := d.effectiveToolPolicy("entity-delete")
	require.True(t, ok2)
	assert.True(t, p2.Disabled, "catalog-less wrapper must resolve static tools identically")
}

// TestEffectiveToolPolicyFor_OtherGroupDoesNotGovernDynamic guards against
// @group:other accidentally governing a tool whose real scope maps to a proper
// group: document-create (documents:write) must not match @group:other.
func TestEffectiveToolPolicyFor_OtherGroupDoesNotGovernDynamic(t *testing.T) {
	d := &AgentDefinition{
		DefaultToolPolicy: ToolPolicyDefaultAllow,
		ToolPolicies: map[string]ToolPolicy{
			"@group:other": {Disabled: true},
		},
	}
	p, ok := d.effectiveToolPolicyFor("document-create", "documents:write")
	assert.False(t, ok, "@group:other must not govern a documents-scoped tool")
	assert.False(t, p.Disabled)
}

func TestEffectiveToolPolicyFor_ExplicitBeatsGroupDynamic(t *testing.T) {
	d := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:documents": {Disabled: true},
			"document-create":  {Confirm: true},
		},
	}
	p, ok := d.effectiveToolPolicyFor("document-create", "documents:write")
	require.True(t, ok)
	assert.True(t, p.Confirm, "explicit per-tool entry must beat the group entry")
	assert.False(t, p.Disabled)
}

// TestDynamicGroupDenyRejectedBeforeExecution mirrors the beforeToolCb
// disabled-check for a dynamic tool: with the catalog scope supplied, a
// @group:documents deny blocks document-create before it executes.
func TestDynamicGroupDenyRejectedBeforeExecution(t *testing.T) {
	def := &AgentDefinition{
		ToolPolicies: map[string]ToolPolicy{
			"@group:documents": {Disabled: true},
		},
	}
	// toolScopes supplies documents:write at the chokepoint.
	policy, hasPolicy := def.effectiveToolPolicyFor("document-create", "documents:write")
	blocked := hasPolicy && policy.Disabled
	assert.True(t, blocked, "dynamic group-disabled tool must be blocked before execution")
}

// --- @group: key round-trip through the stored jsonb (no migration) ---

func TestToolPoliciesGroupKeyJSONRoundTrip(t *testing.T) {
	d := &AgentDefinition{
		Name: "x",
		ToolPolicies: map[string]ToolPolicy{
			"@group:graph-write":    {Confirm: true},
			"@group:schema-migrate": {Disabled: true},
			"entity-delete":         {Confirm: false},
		},
	}
	data, err := json.Marshal(d)
	require.NoError(t, err)

	var back AgentDefinition
	require.NoError(t, json.Unmarshal(data, &back))

	require.Contains(t, back.ToolPolicies, "@group:graph-write")
	require.Contains(t, back.ToolPolicies, "@group:schema-migrate")
	assert.True(t, back.ToolPolicies["@group:graph-write"].Confirm)
	assert.True(t, back.ToolPolicies["@group:schema-migrate"].Disabled)
	assert.Contains(t, back.ToolPolicies, "entity-delete")
}
