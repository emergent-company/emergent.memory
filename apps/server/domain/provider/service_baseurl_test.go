package provider

import "testing"

// TestBuildTempResolvedCredDeepSeekBaseURL verifies that the DeepSeek provider
// honors an explicit BaseURL (e.g. a LiteLLM/OpenAI-compatible proxy) while
// still falling back to the official DeepSeek endpoint when none is provided.
func TestBuildTempResolvedCredDeepSeekBaseURL(t *testing.T) {
	s := &CredentialService{}

	t.Run("explicit base url is preserved", func(t *testing.T) {
		cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
			APIKey:          "k",
			BaseURL:         "http://litellm:4000/v1",
			GenerativeModel: "deepseek/deepseek-v4-flash",
		})
		if cred.BaseURL != "http://litellm:4000/v1" {
			t.Fatalf("BaseURL = %q, want %q", cred.BaseURL, "http://litellm:4000/v1")
		}
		if cred.GenerativeModel != "deepseek-v4-flash" {
			t.Fatalf("GenerativeModel = %q, want %q", cred.GenerativeModel, "deepseek-v4-flash")
		}
	})

	t.Run("empty base url falls back to deepseek default", func(t *testing.T) {
		cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
			APIKey:          "k",
			GenerativeModel: "deepseek-v4-flash",
		})
		if cred.BaseURL != "https://api.deepseek.com/v1" {
			t.Fatalf("BaseURL = %q, want %q", cred.BaseURL, "https://api.deepseek.com/v1")
		}
	})
}
