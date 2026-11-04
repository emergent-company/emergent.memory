package agents

import "context"

// TestLLMFlagChecker reports whether a project has deterministic test-LLM mode
// enabled (project setting feature_flags/test_llm = {"enabled": true}).
// It satisfies both adk.TestLLMChecker and embeddings.TestLLMChecker.
type TestLLMFlagChecker struct {
	repo *Repository
}

// NewTestLLMFlagChecker creates a TestLLMFlagChecker backed by the repository.
func NewTestLLMFlagChecker(repo *Repository) *TestLLMFlagChecker {
	return &TestLLMFlagChecker{repo: repo}
}

// IsTestLLM implements adk.TestLLMChecker and embeddings.TestLLMChecker.
func (c *TestLLMFlagChecker) IsTestLLM(ctx context.Context, projectID string) bool {
	s, err := c.repo.GetProjectSetting(ctx, projectID, SettingsCategoryFeatureFlags, SettingsKeyTestLLM)
	if err != nil || s == nil {
		return false
	}
	enabled, _ := s.Value["enabled"].(bool)
	return enabled
}
