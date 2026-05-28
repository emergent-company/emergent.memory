package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// normaliseForgetStrategy mirrors the handler validation logic.
// These tests drive the expected behaviour before the handler is implemented.
// ---------------------------------------------------------------------------

func normaliseForgetStrategy(raw string) (string, bool) {
	if raw == "" {
		return "confirm", true
	}
	switch raw {
	case "auto", "confirm", "ask":
		return raw, true
	default:
		return "", false
	}
}

func normaliseForgetCascadeDepth(raw int) (int, bool) {
	if raw == 0 {
		return 2, true // default
	}
	if raw >= 1 && raw <= 3 {
		return raw, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// strategy validation
// ---------------------------------------------------------------------------

func TestNormaliseForgetStrategy_Empty_DefaultsToConfirm(t *testing.T) {
	strategy, ok := normaliseForgetStrategy("")
	assert.True(t, ok)
	assert.Equal(t, "confirm", strategy)
}

func TestNormaliseForgetStrategy_Auto_Valid(t *testing.T) {
	strategy, ok := normaliseForgetStrategy("auto")
	assert.True(t, ok)
	assert.Equal(t, "auto", strategy)
}

func TestNormaliseForgetStrategy_Confirm_Valid(t *testing.T) {
	strategy, ok := normaliseForgetStrategy("confirm")
	assert.True(t, ok)
	assert.Equal(t, "confirm", strategy)
}

func TestNormaliseForgetStrategy_Ask_Valid(t *testing.T) {
	strategy, ok := normaliseForgetStrategy("ask")
	assert.True(t, ok)
	assert.Equal(t, "ask", strategy)
}

func TestNormaliseForgetStrategy_Invalid_ReturnsFalse(t *testing.T) {
	for _, bad := range []string{"unknown", "reuse_only", "CONFIRM", "yes", "1"} {
		_, ok := normaliseForgetStrategy(bad)
		assert.False(t, ok, "strategy %q must be rejected", bad)
	}
}

// ---------------------------------------------------------------------------
// cascade_depth validation
// ---------------------------------------------------------------------------

func TestNormaliseForgetCascadeDepth_Zero_DefaultsToTwo(t *testing.T) {
	depth, ok := normaliseForgetCascadeDepth(0)
	assert.True(t, ok)
	assert.Equal(t, 2, depth)
}

func TestNormaliseForgetCascadeDepth_One_Valid(t *testing.T) {
	depth, ok := normaliseForgetCascadeDepth(1)
	assert.True(t, ok)
	assert.Equal(t, 1, depth)
}

func TestNormaliseForgetCascadeDepth_Two_Valid(t *testing.T) {
	depth, ok := normaliseForgetCascadeDepth(2)
	assert.True(t, ok)
	assert.Equal(t, 2, depth)
}

func TestNormaliseForgetCascadeDepth_Three_Valid(t *testing.T) {
	depth, ok := normaliseForgetCascadeDepth(3)
	assert.True(t, ok)
	assert.Equal(t, 3, depth)
}

func TestNormaliseForgetCascadeDepth_TooLow_Invalid(t *testing.T) {
	_, ok := normaliseForgetCascadeDepth(-1)
	assert.False(t, ok)
}

func TestNormaliseForgetCascadeDepth_TooHigh_Invalid(t *testing.T) {
	for _, bad := range []int{4, 5, 10, 100} {
		_, ok := normaliseForgetCascadeDepth(bad)
		assert.False(t, ok, "cascade_depth %d must be rejected", bad)
	}
}

// ---------------------------------------------------------------------------
// ForgetStreamRequest struct field behaviour
// (mirrors the handler's parsing expectations)
// ---------------------------------------------------------------------------

func TestForgetStreamRequest_DefaultStrategy_IsConfirm(t *testing.T) {
	// Zero-value request has empty Strategy — must default to "confirm"
	var req ForgetStreamRequest
	strategy, ok := normaliseForgetStrategy(req.Strategy)
	assert.True(t, ok)
	assert.Equal(t, "confirm", strategy)
}

func TestForgetStreamRequest_DefaultCascadeDepth_IsTwo(t *testing.T) {
	var req ForgetStreamRequest
	depth, ok := normaliseForgetCascadeDepth(req.CascadeDepth)
	assert.True(t, ok)
	assert.Equal(t, 2, depth)
}

func TestForgetStreamRequest_DryRunDefault_IsFalse(t *testing.T) {
	var req ForgetStreamRequest
	assert.False(t, req.DryRun)
}

// ---------------------------------------------------------------------------
// Agent message assembly
// buildForgetAgentMessage must include key context fields
// ---------------------------------------------------------------------------

func TestBuildForgetAgentMessage_IncludesStrategy(t *testing.T) {
	msg := buildForgetAgentMessage("forget auth module", "confirm", 2, false)
	assert.Contains(t, msg, "confirm")
}

func TestBuildForgetAgentMessage_IncludesCascadeDepth(t *testing.T) {
	msg := buildForgetAgentMessage("forget auth module", "auto", 3, false)
	assert.Contains(t, msg, "3")
}

func TestBuildForgetAgentMessage_IncludesDryRun_WhenTrue(t *testing.T) {
	msg := buildForgetAgentMessage("forget auth module", "auto", 2, true)
	assert.True(t,
		containsAny(msg, "dry_run: true", "dry_run:true", "dry run"),
		"agent message must indicate dry_run=true")
}

func TestBuildForgetAgentMessage_IncludesUserQuery(t *testing.T) {
	msg := buildForgetAgentMessage("forget all auth related nodes", "auto", 2, false)
	assert.Contains(t, msg, "forget all auth related nodes")
}

func TestBuildForgetAgentMessage_DryRunFalse_NotHighlighted(t *testing.T) {
	// dry_run=false is the default; may or may not be in message, but must not say "dry_run: true"
	msg := buildForgetAgentMessage("forget X", "auto", 2, false)
	assert.NotContains(t, msg, "dry_run: true")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if len(s) >= len(n) {
			for i := 0; i <= len(s)-len(n); i++ {
				if s[i:i+len(n)] == n {
					return true
				}
			}
		}
	}
	return false
}
