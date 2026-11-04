package main

import "testing"

// TestProviderRequiresBaseURL verifies the endpoint-visibility rule is driven
// by registry metadata: a vendor needs an editable endpoint when its declared
// default is absent or carries a {placeholder}, and canonical openai keeps its
// pre-existing editable endpoint.
func TestProviderRequiresBaseURL(t *testing.T) {
	vendors := []ProviderDefinition{
		{Type: "openai", Protocol: "openai-chat", DefaultBaseURLs: map[string]string{"generative": "https://api.openai.com/v1"}},
		{Type: "azure-openai", Protocol: "openai-chat", DefaultBaseURLs: map[string]string{"generative": "https://{resource}.openai.azure.com/openai/v1"}},
		{Type: "generic", Protocol: "openai-chat"},
		{Type: "deepseek", Protocol: "openai-chat", DefaultBaseURLs: map[string]string{"generative": "https://api.deepseek.com"}},
	}
	cases := []struct {
		provider string
		want     bool
	}{
		{provider: "openai", want: true},
		{provider: "azure-openai", want: true},
		{provider: "generic", want: true},
		{provider: "deepseek", want: false},
		{provider: "unknown", want: false},
	}
	for _, tc := range cases {
		if got := providerRequiresBaseURL(vendors, tc.provider); got != tc.want {
			t.Errorf("providerRequiresBaseURL(%q) = %v, want %v", tc.provider, got, tc.want)
		}
	}
}

// TestProviderDefaultBaseURL verifies the prefill value prefers the generative
// default and falls back to the embedding default, with "" for unknown vendors.
func TestProviderDefaultBaseURL(t *testing.T) {
	vendors := []ProviderDefinition{
		{Type: "azure-openai", DefaultBaseURLs: map[string]string{"generative": "https://{resource}.openai.azure.com/openai/v1"}},
		{Type: "embed-only", DefaultBaseURLs: map[string]string{"embedding": "https://embed.example.com/v1"}},
		{Type: "empty"},
	}
	cases := []struct {
		provider string
		want     string
	}{
		{provider: "azure-openai", want: "https://{resource}.openai.azure.com/openai/v1"},
		{provider: "embed-only", want: "https://embed.example.com/v1"},
		{provider: "empty", want: ""},
		{provider: "unknown", want: ""},
	}
	for _, tc := range cases {
		if got := providerDefaultBaseURL(vendors, tc.provider); got != tc.want {
			t.Errorf("providerDefaultBaseURL(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

// TestProviderPrefillBaseURL verifies a configured URL wins over the registry
// default on the edit form.
func TestProviderPrefillBaseURL(t *testing.T) {
	vendors := []ProviderDefinition{
		{Type: "azure-openai", DefaultBaseURLs: map[string]string{"generative": "https://{resource}.openai.azure.com/openai/v1"}},
	}
	if got := providerPrefillBaseURL(nil, vendors); got != "" {
		t.Errorf("providerPrefillBaseURL(nil) = %q, want empty", got)
	}
	if got := providerPrefillBaseURL(&ProjectProviderConfig{Provider: "azure-openai"}, vendors); got != "https://{resource}.openai.azure.com/openai/v1" {
		t.Errorf("providerPrefillBaseURL(default) = %q, want registry default", got)
	}
	if got := providerPrefillBaseURL(&ProjectProviderConfig{Provider: "azure-openai", BaseURL: "https://my.azure.com/v1"}, vendors); got != "https://my.azure.com/v1" {
		t.Errorf("providerPrefillBaseURL(configured) = %q, want configured URL", got)
	}
}
