package provider

import "testing"

// TestRegistryBuiltinsConformance asserts every built-in vendor definition is
// internally coherent: known protocol, valid auth style, non-empty model types,
// known catalog strategy, valid compat enum values, reachable base URLs for the
// model types it actually serves (except generic endpoints whose base URL is
// user-supplied), and a protocol→auth mapping consistent with the adapters.
func TestRegistryBuiltinsConformance(t *testing.T) {
	for _, d := range Builtins() {
		t.Run(string(d.Type), func(t *testing.T) {
			// Protocol must be one of the three implemented adapters.
			switch d.Protocol {
			case ProtocolOpenAIChat, ProtocolGoogleGenAI, ProtocolAnthropicMessages:
			default:
				t.Errorf("protocol %q not in {openai-chat, google-genai, anthropic-messages}", d.Protocol)
			}

			// Auth must be a valid AuthStyle.
			if !validAuthStyle(d.Auth) {
				t.Errorf("invalid auth style %q", d.Auth)
			}

			// Every vendor serves at least one model type.
			if len(d.ModelTypes) == 0 {
				t.Error("no model types declared")
			}

			// Every vendor declares a known catalog strategy (anthropic uses
			// CatalogStatic: it has no catalog-listing integration).
			switch d.CatalogStrategy {
			case CatalogOpenAIModels, CatalogGoogleGenAI, CatalogStatic:
			default:
				t.Errorf("catalog strategy %q not in {openai-models, google-genai, static}", d.CatalogStrategy)
			}

			// Compat enum values must be within the recognized sets.
			switch d.Compat.MaxTokensField {
			case "", "max_tokens", "max_completion_tokens":
			default:
				t.Errorf("MaxTokensField %q not in {\"\", max_tokens, max_completion_tokens}", d.Compat.MaxTokensField)
			}
			switch d.Compat.ThinkingStyle {
			case "", "thinking", "thinking_type", "chat_template_kwargs":
			default:
				t.Errorf("ThinkingStyle %q not in {\"\", thinking, thinking_type, chat_template_kwargs}", d.Compat.ThinkingStyle)
			}

			// Generic endpoints (generic/litellm/gpustack) have user-supplied base
			// URLs, so they are exempt from the reachable-URL checks below.
			isGeneric := d.Type == ProviderGeneric || d.Type == ProviderLiteLLM || d.Type == ProviderGPUStack

			// A vendor that fetches its catalog via GET /models needs a generative
			// base URL for the model types it actually serves.
			if !isGeneric && d.CatalogStrategy == CatalogOpenAIModels && servesGenerative(d) {
				if d.DefaultBaseURLs[ModelTypeGenerative] == "" {
					t.Error("CatalogOpenAIModels vendor serving generative models has no generative base URL")
				}
			}

			// An openai-chat vendor that serves embeddings needs an embedding base URL.
			if !isGeneric && servesEmbedding(d) && d.Protocol == ProtocolOpenAIChat {
				if d.DefaultBaseURLs[ModelTypeEmbedding] == "" {
					t.Error("openai-chat vendor serving embeddings has no embedding base URL")
				}
			}

			// Protocol→auth consistency: the adapters assume these mappings.
			if d.Protocol == ProtocolAnthropicMessages && d.Auth != AuthXAPIKey {
				t.Errorf("anthropic-messages vendor uses auth %q, want %q", d.Auth, AuthXAPIKey)
			}
			if d.Protocol == ProtocolGoogleGenAI {
				if d.Auth != AuthGoogleAPIKey && d.Auth != AuthSigned {
					t.Errorf("google-genai vendor uses auth %q, want %q or %q", d.Auth, AuthGoogleAPIKey, AuthSigned)
				}
			}
		})
	}
}

// servesGenerative reports whether the definition lists the generative model type.
func servesGenerative(def *ProviderDefinition) bool {
	if def == nil {
		return false
	}
	for _, mt := range def.ModelTypes {
		if mt == ModelTypeGenerative {
			return true
		}
	}
	return false
}

// servesEmbedding reports whether the definition lists the embedding model type.
func servesEmbedding(def *ProviderDefinition) bool {
	if def == nil {
		return false
	}
	for _, mt := range def.ModelTypes {
		if mt == ModelTypeEmbedding {
			return true
		}
	}
	return false
}
