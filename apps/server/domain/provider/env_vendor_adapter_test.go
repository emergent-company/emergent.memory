package provider

import (
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/adk"
)

func TestEnvVendorAdapter_EnvVendor(t *testing.T) {
	a := NewEnvVendorAdapter(NewRegistry())

	tests := []struct {
		provider           string
		wantPrefix         string
		wantBaseURL        string
		wantProtocol       string
		wantResponsesHosts []string
		wantAuth           string
	}{
		{
			provider:     "anthropic",
			wantPrefix:   "ANTHROPIC",
			wantBaseURL:  "https://api.anthropic.com/v1",
			wantProtocol: string(ProtocolAnthropicMessages),
			wantAuth:     string(AuthXAPIKey),
		},
		{
			provider:     "openrouter",
			wantPrefix:   "OPENROUTER",
			wantBaseURL:  "https://openrouter.ai/api/v1",
			wantProtocol: string(ProtocolOpenAIChat),
			wantAuth:     string(AuthBearer),
		},
		{
			// The definition's *default* protocol is carried raw; the Responses
			// host list is carried alongside so the env path can apply the same
			// host rule as the DB path against the resolved (possibly
			// overridden) base URL.
			provider:           "openai",
			wantPrefix:         "OPENAI",
			wantBaseURL:        "https://api.openai.com/v1",
			wantProtocol:       string(ProtocolOpenAIChat),
			wantResponsesHosts: adk.FirstPartyOpenAIResponseHosts,
			wantAuth:           string(AuthBearer),
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got, ok := a.EnvVendor(tt.provider)
			if !ok {
				t.Fatalf("EnvVendor(%q) ok = false, want true", tt.provider)
			}
			if got.Prefix != tt.wantPrefix {
				t.Errorf("Prefix = %q, want %q", got.Prefix, tt.wantPrefix)
			}
			if got.DefaultBaseURL != tt.wantBaseURL {
				t.Errorf("DefaultBaseURL = %q, want %q", got.DefaultBaseURL, tt.wantBaseURL)
			}
			if got.Protocol != tt.wantProtocol {
				t.Errorf("Protocol = %q, want %q", got.Protocol, tt.wantProtocol)
			}
			if len(got.ResponsesHosts) != len(tt.wantResponsesHosts) {
				t.Errorf("ResponsesHosts = %v, want %v", got.ResponsesHosts, tt.wantResponsesHosts)
			} else {
				for i := range tt.wantResponsesHosts {
					if got.ResponsesHosts[i] != tt.wantResponsesHosts[i] {
						t.Errorf("ResponsesHosts[%d] = %q, want %q", i, got.ResponsesHosts[i], tt.wantResponsesHosts[i])
					}
				}
			}
			if got.Auth != tt.wantAuth {
				t.Errorf("Auth = %q, want %q", got.Auth, tt.wantAuth)
			}
		})
	}

	if _, ok := a.EnvVendor("not-a-real-vendor"); ok {
		t.Error("EnvVendor(not-a-real-vendor) ok = true, want false")
	}

	// Embedding-only vendors must not be exposed to the generative env path.
	if _, ok := a.EnvVendor("jina"); ok {
		t.Error("EnvVendor(jina) ok = true, want false (embedding-only)")
	}
}

func TestEnvVendorAdapter_EnvVendorOrder(t *testing.T) {
	registry := NewRegistry()
	a := NewEnvVendorAdapter(registry)

	order := a.EnvVendorOrder()

	present := map[string]bool{}
	for _, p := range order {
		present[p] = true
	}
	// Every generative registry vendor must be discoverable so the env fallback
	// can pick any configured vendor's model; spot-check a non-legacy and the
	// legacy set.
	for _, want := range []string{"anthropic", "openrouter", "deepseek", "openai", "google", "google-vertex"} {
		if !present[want] {
			t.Errorf("EnvVendorOrder() missing %q", want)
		}
	}
	// Embedding-only vendors must be excluded from generative discovery.
	if present["jina"] {
		t.Error("EnvVendorOrder() includes embedding-only vendor jina")
	}
}
