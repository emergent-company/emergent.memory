package agents

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/skills"
)

// fakeSkillRepo implements skills.SkillRepo for executor unit tests.
type fakeSkillRepo struct {
	skills []*skills.Skill
}

func (f *fakeSkillRepo) FindForAgent(ctx context.Context, projectID string, orgID string) ([]*skills.Skill, error) {
	return f.skills, nil
}

func (f *fakeSkillRepo) FindRelevant(ctx context.Context, projectID string, orgID string, vec []float32, topK int) ([]*skills.Skill, error) {
	return f.skills, nil
}

func newTestExecutorWithSkills(repo skills.SkillRepo) *AgentExecutor {
	return &AgentExecutor{
		skillRepo: repo,
		log:       slog.New(slog.NewTextHandler(&strings.Builder{}, nil)),
	}
}

func skill(name string) *skills.Skill {
	// Description deliberately avoids repeating the name so occurrence-count
	// assertions on the prompt match only the skill listing lines.
	return &skills.Skill{Name: name, Description: "Reusable workflow instructions.", Content: "content of " + name}
}

func TestBuildSkillsSystemPrompt_AutoLoadPrefixMatch(t *testing.T) {
	// Agent named diane with auto_load_skills=true and skills diane.meetings +
	// diane.review existing but NOT listed in the definition's Skills field.
	repo := &fakeSkillRepo{skills: []*skills.Skill{
		skill("diane.meetings"),
		skill("diane.review"),
		skill("research"), // not agent-prefixed — must NOT be auto-loaded
	}}
	ae := newTestExecutorWithSkills(repo)

	req := ExecuteRequest{
		ProjectID: "proj-1",
		AgentDefinition: &AgentDefinition{
			Name:           "diane",
			AutoLoadSkills: true,
			Skills:         []string{},
		},
	}

	prompt := ae.buildSkillsSystemPrompt(req)
	require.Contains(t, prompt, "diane.meetings", "auto-load must include {agentName}.{suffix} skills")
	require.Contains(t, prompt, "diane.review")
	assert.NotContains(t, prompt, "research", "non-prefixed skills must not be auto-loaded")
}

func TestBuildSkillsSystemPrompt_ExplicitFirstDedup(t *testing.T) {
	// Agent also lists ["diane.review", "research"] explicitly.
	repo := &fakeSkillRepo{skills: []*skills.Skill{
		skill("diane.meetings"),
		skill("diane.review"),
		skill("research"),
	}}
	ae := newTestExecutorWithSkills(repo)

	req := ExecuteRequest{
		ProjectID: "proj-1",
		AgentDefinition: &AgentDefinition{
			Name:           "diane",
			AutoLoadSkills: true,
			Skills:         []string{"diane.review", "research"},
		},
	}

	prompt := ae.buildSkillsSystemPrompt(req)
	assert.Contains(t, prompt, "research", "explicitly listed skill must be included")
	assert.Contains(t, prompt, "diane.meetings", "auto-prefix match must still be merged in")
	// diane.review appears exactly once (explicit + prefix match deduplicated).
	assert.Equal(t, 1, strings.Count(prompt, "diane.review"), "explicit/auto overlap must be deduplicated")
	// Explicit names appear before auto-loaded ones.
	assert.True(t, strings.Index(prompt, "diane.review") < strings.Index(prompt, "diane.meetings"),
		"explicit skills must come first")
}

func TestBuildSkillsSystemPrompt_AutoLoadOff(t *testing.T) {
	// auto_load_skills=false → only explicitly listed skills are available.
	repo := &fakeSkillRepo{skills: []*skills.Skill{
		skill("diane.meetings"),
		skill("diane.review"),
		skill("research"),
	}}
	ae := newTestExecutorWithSkills(repo)

	req := ExecuteRequest{
		ProjectID: "proj-1",
		AgentDefinition: &AgentDefinition{
			Name:           "diane",
			AutoLoadSkills: false,
			Skills:         []string{"research"},
		},
	}

	prompt := ae.buildSkillsSystemPrompt(req)
	assert.Contains(t, prompt, "research")
	assert.NotContains(t, prompt, "diane.meetings", "prefix matching must be disabled when auto_load_skills=false")
	assert.NotContains(t, prompt, "diane.review")
}

func TestBuildSkillsSystemPrompt_EdgeCases(t *testing.T) {
	repo := &fakeSkillRepo{skills: []*skills.Skill{skill("diane.meetings")}}
	ae := newTestExecutorWithSkills(repo)

	t.Run("nil agent definition returns empty", func(t *testing.T) {
		assert.Equal(t, "", ae.buildSkillsSystemPrompt(ExecuteRequest{}))
	})

	t.Run("no skills and no auto-load returns empty", func(t *testing.T) {
		req := ExecuteRequest{AgentDefinition: &AgentDefinition{Name: "diane", AutoLoadSkills: false}}
		assert.Equal(t, "", ae.buildSkillsSystemPrompt(req))
	})

	t.Run("empty repo returns empty", func(t *testing.T) {
		ae := newTestExecutorWithSkills(&fakeSkillRepo{skills: nil})
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{Name: "diane", AutoLoadSkills: true},
		}
		assert.Equal(t, "", ae.buildSkillsSystemPrompt(req))
	})

	t.Run("wildcard includes all", func(t *testing.T) {
		req := ExecuteRequest{
			AgentDefinition: &AgentDefinition{Name: "diane", AutoLoadSkills: false, Skills: []string{"*"}},
		}
		prompt := ae.buildSkillsSystemPrompt(req)
		assert.Contains(t, prompt, "diane.meetings")
	})
}
