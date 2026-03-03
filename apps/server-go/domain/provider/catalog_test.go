package provider

import (
	"testing"

	"google.golang.org/genai"
)

func TestClassifyModel(t *testing.T) {
	tests := []struct {
		name     string
		model    *genai.Model
		expected ModelType
	}{
		{
			name:     "nil model",
			model:    nil,
			expected: "",
		},
		{
			name: "embedding model with embedContent",
			model: &genai.Model{
				Name:             "models/text-embedding-004",
				SupportedActions: []string{"embedContent", "countTextTokens"},
			},
			expected: ModelTypeEmbedding,
		},
		{
			name: "embedding model with batchEmbedContents",
			model: &genai.Model{
				Name:             "models/gemini-embedding-001",
				SupportedActions: []string{"batchEmbedContents", "embedContent"},
			},
			expected: ModelTypeEmbedding,
		},
		{
			name: "generative model with generateContent",
			model: &genai.Model{
				Name:             "models/gemini-2.0-flash",
				SupportedActions: []string{"generateContent", "streamGenerateContent", "countTextTokens"},
			},
			expected: ModelTypeGenerative,
		},
		{
			name: "generative model with only streamGenerateContent",
			model: &genai.Model{
				Name:             "models/gemini-custom",
				SupportedActions: []string{"streamGenerateContent"},
			},
			expected: ModelTypeGenerative,
		},
		{
			name: "embedding takes priority over generative",
			model: &genai.Model{
				Name:             "models/hybrid-model",
				SupportedActions: []string{"generateContent", "embedContent", "streamGenerateContent"},
			},
			expected: ModelTypeEmbedding,
		},
		{
			name: "unknown actions returns empty",
			model: &genai.Model{
				Name:             "models/mystery-model",
				SupportedActions: []string{"countTextTokens", "createTunedModel"},
			},
			expected: "",
		},
		{
			name: "empty actions returns empty",
			model: &genai.Model{
				Name:             "models/no-actions",
				SupportedActions: nil,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyModel(tt.model)
			if got != tt.expected {
				t.Errorf("classifyModel() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"models/gemini-2.0-flash", "gemini-2.0-flash"},
		{"gemini-2.0-flash", "gemini-2.0-flash"},
		{"models/text-embedding-004", "text-embedding-004"},
		{"", ""},
		{"models/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeModelName(tt.input)
			if got != tt.expected {
				t.Errorf("normalizeModelName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStaticModels(t *testing.T) {
	t.Run("Google AI static models", func(t *testing.T) {
		models := staticModels(ProviderGoogleAI)
		if len(models) == 0 {
			t.Fatal("expected at least one static model for Google AI")
		}

		var hasGenerative, hasEmbedding bool
		for _, m := range models {
			if m.Provider != ProviderGoogleAI {
				t.Errorf("expected provider %q, got %q", ProviderGoogleAI, m.Provider)
			}
			if m.ModelName == "" {
				t.Error("model name should not be empty")
			}
			if m.DisplayName == "" {
				t.Error("display name should not be empty")
			}
			switch m.ModelType {
			case ModelTypeGenerative:
				hasGenerative = true
			case ModelTypeEmbedding:
				hasEmbedding = true
			default:
				t.Errorf("unexpected model type: %q", m.ModelType)
			}
		}

		if !hasGenerative {
			t.Error("static models should include at least one generative model")
		}
		if !hasEmbedding {
			t.Error("static models should include at least one embedding model")
		}
	})

	t.Run("Vertex AI static models", func(t *testing.T) {
		models := staticModels(ProviderVertexAI)
		if len(models) == 0 {
			t.Fatal("expected at least one static model for Vertex AI")
		}

		for _, m := range models {
			if m.Provider != ProviderVertexAI {
				t.Errorf("expected provider %q, got %q", ProviderVertexAI, m.Provider)
			}
		}
	})

	t.Run("static models count matches between providers", func(t *testing.T) {
		googleModels := staticModels(ProviderGoogleAI)
		vertexModels := staticModels(ProviderVertexAI)
		if len(googleModels) != len(vertexModels) {
			t.Errorf("expected same number of models for both providers, got Google AI=%d, Vertex AI=%d",
				len(googleModels), len(vertexModels))
		}
	})
}
