package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// demoteStageForPolicy
// ---------------------------------------------------------------------------

func TestDemoteStageForPolicy_NewDomain_ReuseOnly(t *testing.T) {
	result := demoteStageForPolicy("new_domain", "reuse_only")
	assert.Equal(t, "no_match", result,
		"new_domain + reuse_only must be demoted to no_match")
}

func TestDemoteStageForPolicy_NewDomain_Auto(t *testing.T) {
	result := demoteStageForPolicy("new_domain", "auto")
	assert.Equal(t, "new_domain", result,
		"new_domain + auto must pass through unchanged")
}

func TestDemoteStageForPolicy_NewDomain_Ask(t *testing.T) {
	result := demoteStageForPolicy("new_domain", "ask")
	assert.Equal(t, "new_domain", result,
		"new_domain + ask must pass through unchanged (confirmation gate handles it)")
}

func TestDemoteStageForPolicy_LLM_ReuseOnly(t *testing.T) {
	result := demoteStageForPolicy("llm", "reuse_only")
	assert.Equal(t, "llm", result,
		"llm stage must not be demoted (schema already matched)")
}

func TestDemoteStageForPolicy_Vector_ReuseOnly(t *testing.T) {
	result := demoteStageForPolicy("vector", "reuse_only")
	assert.Equal(t, "vector", result,
		"vector stage must not be demoted (schema already matched)")
}

func TestDemoteStageForPolicy_Empty_ReuseOnly(t *testing.T) {
	result := demoteStageForPolicy("", "reuse_only")
	assert.Equal(t, "", result,
		"empty stage must pass through unchanged")
}

func TestDemoteStageForPolicy_NewDomain_EmptyPolicy(t *testing.T) {
	result := demoteStageForPolicy("new_domain", "")
	assert.Equal(t, "new_domain", result,
		"empty policy must not trigger demotion")
}

// ---------------------------------------------------------------------------
// schema_policy validation / defaulting
// These mirror the normalisation logic in RememberStream (handler.go:1551-1558).
// Extracted here as pure-function tests against the validation rules.
// ---------------------------------------------------------------------------

// normaliseSchemaPolicy mirrors the handler's normalisation logic.
func normaliseSchemaPolicy(raw string) (string, bool) {
	if raw == "" {
		return "reuse_only", true
	}
	switch raw {
	case "auto", "reuse_only", "ask":
		return raw, true
	default:
		return "", false
	}
}

func TestNormaliseSchemaPolicy_Empty_DefaultsToReuseOnly(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("")
	assert.True(t, ok)
	assert.Equal(t, "reuse_only", policy)
}

func TestNormaliseSchemaPolicy_Auto_Valid(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("auto")
	assert.True(t, ok)
	assert.Equal(t, "auto", policy)
}

func TestNormaliseSchemaPolicy_ReuseOnly_Valid(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("reuse_only")
	assert.True(t, ok)
	assert.Equal(t, "reuse_only", policy)
}

func TestNormaliseSchemaPolicy_Ask_Valid(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("ask")
	assert.True(t, ok)
	assert.Equal(t, "ask", policy)
}

func TestNormaliseSchemaPolicy_Invalid_ReturnsFalse(t *testing.T) {
	_, ok := normaliseSchemaPolicy("invalid_value")
	assert.False(t, ok, "unknown policy value must be rejected")
}

func TestNormaliseSchemaPolicy_CaseSensitive(t *testing.T) {
	_, ok := normaliseSchemaPolicy("Auto")
	assert.False(t, ok, "policy values are case-sensitive")
}

// ---------------------------------------------------------------------------
// no_match + reuse_only combined path
// Verifies the full flow: normalise → demote
// ---------------------------------------------------------------------------

func TestFullPolicyFlow_ReuseOnly_NewDomain_BecomesNoMatch(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("")
	assert.True(t, ok)

	stage := demoteStageForPolicy("new_domain", policy)
	assert.Equal(t, "no_match", stage,
		"default policy (reuse_only) + new_domain classifier result must produce no_match")
}

func TestFullPolicyFlow_Auto_NewDomain_Unchanged(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("auto")
	assert.True(t, ok)

	stage := demoteStageForPolicy("new_domain", policy)
	assert.Equal(t, "new_domain", stage,
		"auto policy must allow new_domain to propagate to the agent")
}

func TestFullPolicyFlow_ReuseOnly_LLMMatch_Unchanged(t *testing.T) {
	policy, ok := normaliseSchemaPolicy("reuse_only")
	assert.True(t, ok)

	stage := demoteStageForPolicy("llm", policy)
	assert.Equal(t, "llm", stage,
		"reuse_only must not affect stages where a schema was matched")
}
