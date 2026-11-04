package agents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// buildForgetToolPolicies
// ---------------------------------------------------------------------------

func TestBuildForgetToolPolicies_Auto(t *testing.T) {
	policies := buildForgetToolPolicies("auto")
	assert.Empty(t, policies, "auto must produce no tool restrictions")
}

func TestBuildForgetToolPolicies_EmptyDefault(t *testing.T) {
	policies := buildForgetToolPolicies("")
	assert.Empty(t, policies, "empty strategy must fall through to default (no restrictions)")
}

func TestBuildForgetToolPolicies_Unknown(t *testing.T) {
	policies := buildForgetToolPolicies("unknown")
	assert.Empty(t, policies, "unknown strategy must produce no restrictions")
}

func TestBuildForgetToolPolicies_Confirm(t *testing.T) {
	policies := buildForgetToolPolicies("confirm")

	require.Contains(t, policies, "ask_user")
	p := policies["ask_user"]
	assert.True(t, p.Confirm, "confirm strategy must set Confirm=true on ask_user")
	assert.False(t, p.Disabled)
	assert.NotEmpty(t, p.Message)

	// entity-delete and relationship-delete must NOT be gated in confirm mode
	// (user approves the batch via ask_user, not each individual delete)
	if ep, ok := policies["entity-delete"]; ok {
		assert.False(t, ep.Confirm, "entity-delete must not require per-call confirm in confirm mode")
		assert.False(t, ep.Disabled)
	}
	if rp, ok := policies["relationship-delete"]; ok {
		assert.False(t, rp.Confirm, "relationship-delete must not require per-call confirm in confirm mode")
		assert.False(t, rp.Disabled)
	}
}

func TestBuildForgetToolPolicies_Ask(t *testing.T) {
	policies := buildForgetToolPolicies("ask")

	require.Contains(t, policies, "entity-delete")
	ed := policies["entity-delete"]
	assert.True(t, ed.Confirm, "ask strategy must set Confirm=true on entity-delete")
	assert.False(t, ed.Disabled)
	assert.NotEmpty(t, ed.Message)

	require.Contains(t, policies, "relationship-delete")
	rd := policies["relationship-delete"]
	assert.True(t, rd.Confirm, "ask strategy must set Confirm=true on relationship-delete")
	assert.False(t, rd.Disabled)

	// ask_user must be freely callable (no gate on it)
	if ap, ok := policies["ask_user"]; ok {
		assert.False(t, ap.Confirm)
		assert.False(t, ap.Disabled)
	}
}

// ---------------------------------------------------------------------------
// ForgetAgentDefinition shape
// ---------------------------------------------------------------------------

func TestForgetAgentDefinition_RequiredTools(t *testing.T) {
	def := buildForgetAgentDefinition("confirm", 2)
	required := []string{
		"search-hybrid",
		"entity-query",
		"entity-edges-get",
		"entity-delete",
		"relationship-delete",
		"ask_user",
		"entity-type-list",
	}
	toolSet := make(map[string]bool, len(def.Tools))
	for _, tool := range def.Tools {
		toolSet[tool] = true
	}
	for _, tool := range required {
		assert.True(t, toolSet[tool], "tool %q must be in definition tools list", tool)
	}
}

func TestForgetAgentDefinition_BannedTools(t *testing.T) {
	def := buildForgetAgentDefinition("auto", 1)
	banned := []string{
		"entity-create",
		"relationship-create",
		"schema-migrate-execute",
		"entity-restore", // restore is post-forget user action, not agent job
	}
	toolSet := make(map[string]bool, len(def.Tools))
	for _, tool := range def.Tools {
		toolSet[tool] = true
	}
	for _, tool := range banned {
		assert.False(t, toolSet[tool], "tool %q must NOT be in forget agent tools", tool)
	}
}

func TestForgetAgentDefinition_MaxSteps_Sufficient(t *testing.T) {
	def := buildForgetAgentDefinition("ask", 3)
	require.NotNil(t, def.MaxSteps)
	assert.GreaterOrEqual(t, *def.MaxSteps, 20,
		"cascade=3 with ask strategy needs at least 20 steps headroom")
}

func TestForgetAgentDefinition_HasSystemPrompt(t *testing.T) {
	def := buildForgetAgentDefinition("confirm", 2)
	require.NotNil(t, def.SystemPrompt)
	assert.NotEmpty(t, *def.SystemPrompt)
}

// ---------------------------------------------------------------------------
// System prompt content
// ---------------------------------------------------------------------------

func TestForgetSystemPrompt_ContainsCascadeInstructions(t *testing.T) {
	for _, depth := range []int{1, 2, 3} {
		prompt := buildForgetSystemPrompt(depth, "auto", false)
		assert.Contains(t, prompt, "cascade", "prompt must mention cascade")
		switch depth {
		case 1:
			assert.Contains(t, prompt, "direct", "depth=1 prompt must describe direct-only scope")
		case 2:
			assert.True(t,
				strings.Contains(prompt, "1-hop") || strings.Contains(prompt, "one hop") || strings.Contains(prompt, "neighbors"),
				"depth=2 prompt must describe neighbor expansion")
		case 3:
			assert.True(t,
				strings.Contains(prompt, "2-hop") || strings.Contains(prompt, "two hop") || strings.Contains(prompt, "second"),
				"depth=3 prompt must describe 2-hop expansion")
		}
	}
}

func TestForgetSystemPrompt_ContainsDryRunInstructions(t *testing.T) {
	prompt := buildForgetSystemPrompt(2, "auto", true)
	assert.True(t,
		strings.Contains(prompt, "dry_run") || strings.Contains(prompt, "dry run"),
		"prompt must mention dry_run")
}

func TestForgetSystemPrompt_ContainsReasonInstruction(t *testing.T) {
	prompt := buildForgetSystemPrompt(2, "confirm", false)
	assert.True(t,
		strings.Contains(prompt, "reason"),
		"prompt must instruct agent to pass reason to entity-delete")
}

func TestForgetSystemPrompt_DryRunSkipsDelete(t *testing.T) {
	prompt := buildForgetSystemPrompt(2, "auto", true)
	// dry run prompt must say to skip/not perform deletes
	assert.True(t,
		strings.Contains(prompt, "skip") || strings.Contains(prompt, "no write") || strings.Contains(prompt, "report only"),
		"dry_run prompt must instruct agent to skip actual deletes")
}

func TestForgetSystemPrompt_ContainsRestoreHint(t *testing.T) {
	prompt := buildForgetSystemPrompt(2, "auto", false)
	// Must tell user how to undo — soft delete is reversible
	assert.True(t,
		strings.Contains(prompt, "restore") || strings.Contains(prompt, "reversible"),
		"prompt must mention soft-delete is reversible")
}

// ---------------------------------------------------------------------------
// Tool policy sync change detection (mirrors remember agent pattern)
// ---------------------------------------------------------------------------

func TestForgetToolPolicySyncDetects_AutoToConfirm(t *testing.T) {
	curr := ToolPolicy{}
	want := buildForgetToolPolicies("confirm")["ask_user"]

	changed := curr.Confirm != want.Confirm || curr.Disabled != want.Disabled
	assert.True(t, changed, "switching auto→confirm must trigger a policy sync")
}

func TestForgetToolPolicySyncDetects_ConfirmToAsk(t *testing.T) {
	curr := buildForgetToolPolicies("confirm")["ask_user"]
	want := buildForgetToolPolicies("ask")["entity-delete"]

	// Different tools but both have Confirm=true; real sync checks by key
	_ = curr
	_ = want
	// What matters: ask has entity-delete Confirm=true, confirm does not
	confirmPolicies := buildForgetToolPolicies("confirm")
	askPolicies := buildForgetToolPolicies("ask")

	_, confirmHasEntityDelete := confirmPolicies["entity-delete"]
	_, askHasEntityDelete := askPolicies["entity-delete"]
	assert.False(t, confirmHasEntityDelete || (confirmPolicies["entity-delete"].Confirm),
		"confirm strategy must not gate entity-delete individually")
	assert.True(t, askHasEntityDelete && askPolicies["entity-delete"].Confirm,
		"ask strategy must gate entity-delete individually")
}

func TestForgetToolPolicySyncDetects_NoChange_Auto(t *testing.T) {
	curr := ToolPolicy{}
	want := buildForgetToolPolicies("auto")["entity-delete"] // zero value

	changed := curr.Confirm != want.Confirm || curr.Disabled != want.Disabled
	assert.False(t, changed, "auto→auto must not trigger sync")
}
