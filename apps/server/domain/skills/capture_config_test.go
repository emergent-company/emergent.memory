package skills

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

func TestResolveCaptureMode(t *testing.T) {
	off := &ProjectSkillCaptureConfig{Mode: SkillCaptureOff}
	suggest := &ProjectSkillCaptureConfig{Mode: SkillCaptureSuggest}
	auto := &ProjectSkillCaptureConfig{Mode: SkillCaptureAuto}

	assert.Equal(t, SkillCaptureOff, ResolveCaptureMode(nil, nil))
	assert.Equal(t, SkillCaptureOff, ResolveCaptureMode(&ProjectSkillCaptureConfig{}, nil), "empty mode is off")
	assert.Equal(t, SkillCaptureSuggest, ResolveCaptureMode(suggest, nil))
	assert.Equal(t, SkillCaptureAuto, ResolveCaptureMode(auto, nil))

	// The override may only constrain.
	assert.Equal(t, SkillCaptureOff, ResolveCaptureMode(auto, &AgentSkillCapture{Mode: strp(SkillCaptureOff)}))
	assert.Equal(t, SkillCaptureSuggest, ResolveCaptureMode(auto, &AgentSkillCapture{Mode: strp(SkillCaptureSuggest)}))
	assert.Equal(t, SkillCaptureAuto, ResolveCaptureMode(auto, &AgentSkillCapture{Mode: strp(SkillCaptureAuto)}))
	assert.Equal(t, SkillCaptureSuggest, ResolveCaptureMode(suggest, &AgentSkillCapture{Mode: strp(SkillCaptureAuto)}),
		"an agent override must never widen a suggest project")
	assert.Equal(t, SkillCaptureOff, ResolveCaptureMode(off, &AgentSkillCapture{Mode: strp(SkillCaptureAuto)}))
	assert.Equal(t, SkillCaptureSuggest, ResolveCaptureMode(suggest, &AgentSkillCapture{}), "unset override inherits")
}

func TestResolveAutoApply(t *testing.T) {
	autoOn := &ProjectSkillCaptureConfig{Mode: SkillCaptureAuto, AutoApply: true}
	autoOff := &ProjectSkillCaptureConfig{Mode: SkillCaptureAuto, AutoApply: false}
	suggestOn := &ProjectSkillCaptureConfig{Mode: SkillCaptureSuggest, AutoApply: true}

	assert.True(t, ResolveAutoApply(autoOn, nil))
	assert.False(t, ResolveAutoApply(autoOff, nil), "autoApply must be explicit")
	assert.False(t, ResolveAutoApply(suggestOn, nil), "suggest never auto-applies")
	assert.False(t, ResolveAutoApply(autoOn, &AgentSkillCapture{AutoApply: boolp(false)}),
		"an agent may opt out of auto-apply")
	assert.True(t, ResolveAutoApply(autoOn, &AgentSkillCapture{AutoApply: boolp(true)}))
	assert.False(t, ResolveAutoApply(autoOn, &AgentSkillCapture{Mode: strp(SkillCaptureSuggest)}))
}

func TestValidateProjectSkillCapture(t *testing.T) {
	assert.NoError(t, ValidateProjectSkillCapture(nil))
	assert.NoError(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{}))
	assert.NoError(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{Mode: "suggest"}))
	assert.NoError(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{Mode: "AUTO", AutoApplyConfidence: 0.8}))

	assert.Error(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{Mode: "on"}))
	assert.Error(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{Mode: "auto", AutoApplyConfidence: 1.5}))
	assert.Error(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{Mode: "auto", DedupThreshold: -0.1}))
	assert.Error(t, ValidateProjectSkillCapture(&ProjectSkillCaptureConfig{Mode: "auto", MaxCandidates: -1}))
}

func TestSlugifyAgentNameAndPrefix(t *testing.T) {
	cases := map[string]string{
		"Research Assistant":   "research-assistant",
		"Research  Assistant!": "research-assistant",
		"graph-query-agent":    "graph-query-agent",
		"  ":                   "agent",
		"ÜBER_Agent":           "ber-agent",
	}
	for in, want := range cases {
		assert.Equal(t, want, SlugifyAgentName(in), "slug for %q", in)
	}
	assert.Equal(t, "research-assistant.", AgentSkillPrefix("Research Assistant"))
}

func TestLearnedSkillName(t *testing.T) {
	name := LearnedSkillName("Research Assistant", "Handling Empty Search Results")
	assert.Equal(t, "research-assistant.handling-empty-search-results", name)
	assert.LessOrEqual(t, len(name), maxNameLength)
	assert.NoError(t, ValidateSkillName(name), "learned names must pass the shared name rule")

	long := LearnedSkillName("a-very-long-agent-name-that-goes-on-and-on-and-on", strings.Repeat("topic", 40))
	assert.LessOrEqual(t, len(long), maxNameLength)
	assert.NoError(t, ValidateSkillName(long))

	assert.Equal(t, "agent.lesson", LearnedSkillName("", "!!!"))
}

func TestApplyPatch(t *testing.T) {
	got, err := ApplyPatch("alpha beta gamma", "beta", "BETA")
	require.NoError(t, err)
	assert.Equal(t, "alpha BETA gamma", got)

	_, err = ApplyPatch("alpha beta beta", "beta", "BETA")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not unique")

	_, err = ApplyPatch("alpha", "missing", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	got, err = ApplyPatch("old content", "", "full replacement")
	require.NoError(t, err)
	assert.Equal(t, "full replacement", got)

	_, err = ApplyPatch("old content", "", "")
	require.Error(t, err)
}

func TestNormalized_EarnedTrustDefault(t *testing.T) {
	// 0 / unset means the default floor of one human approval. Autonomy can
	// never be configured down to "the first proposal is automatic".
	assert.Equal(t, 1, ProjectSkillCaptureConfig{Mode: SkillCaptureAuto}.Normalized().MinApprovedBeforeAuto)
	assert.Equal(t, 1, ProjectSkillCaptureConfig{Mode: SkillCaptureAuto, MinApprovedBeforeAuto: 0}.Normalized().MinApprovedBeforeAuto)
	assert.Equal(t, 5, ProjectSkillCaptureConfig{Mode: SkillCaptureAuto, MinApprovedBeforeAuto: 5}.Normalized().MinApprovedBeforeAuto)
}
