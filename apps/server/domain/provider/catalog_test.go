package provider

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestNameLooksEmbedding(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"text-embedding-004", true},
		{"publishers/google/models/text-embedding-005", true},
		{"gemini-embedding-001", true},
		{"gemini-embedding-2-preview", true},
		{"text-embed-3-large", true},
		{"gemini-2.0-flash", false},
		{"deepseek-v4-flash", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := nameLooksEmbedding(tt.name); got != tt.expected {
			t.Errorf("nameLooksEmbedding(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Google AI backend: "models/" prefix
		{"models/gemini-2.0-flash", "gemini-2.0-flash"},
		{"gemini-2.0-flash", "gemini-2.0-flash"},
		{"models/text-embedding-004", "text-embedding-004"},
		{"", ""},
		{"models/", ""},
		// Vertex AI backend: "publishers/google/models/" prefix
		{"publishers/google/models/gemini-2.0-flash", "gemini-2.0-flash"},
		{"publishers/google/models/text-embedding-005", "text-embedding-005"},
		{"publishers/google/models/gemma3", "gemma3"},
		// Vertex AI backend: full resource path with location segment
		{"locations/us-central1/publishers/google/models/gemini-2.0-flash", "gemini-2.0-flash"},
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

func newTestCatalogService() *ModelCatalogService {
	return NewModelCatalogService(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func findModel(models []ProviderSupportedModel, name string) *ProviderSupportedModel {
	for i := range models {
		if models[i].ModelName == name {
			return &models[i]
		}
	}
	return nil
}

func TestFetchOpenAICompatibleModels(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[
			{"id":"deepseek-v4-pro","context_length":131072},
			{"id":"gpt-4o"},
			{"id":"gemini/gemini-embedding-001","max_model_len":8192}
		]}`)
	}))
	defer srv.Close()

	svc := newTestCatalogService()
	cred := &ResolvedCredential{
		BaseURL:         srv.URL,
		APIKey:          "sk-test",
		GenerativeModel: "deepseek-v4-pro",
		EmbeddingModel:  "gemini/gemini-embedding-001",
	}

	models, err := svc.fetchOpenAICompatibleModels(context.Background(), ProviderOpenAI, cred)
	if err != nil {
		t.Fatalf("fetchOpenAICompatibleModels() error = %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-test")
	}
	if len(models) != 3 {
		t.Fatalf("got %d models, want 3: %+v", len(models), models)
	}
	for _, m := range models {
		if m.Provider != ProviderOpenAI {
			t.Errorf("fetched model %q provider = %q, want %q", m.ModelName, m.Provider, ProviderOpenAI)
		}
	}

	pro := findModel(models, "deepseek-v4-pro")
	if pro == nil {
		t.Fatal("deepseek-v4-pro not returned")
	}
	if pro.ModelType != ModelTypeGenerative {
		t.Errorf("deepseek-v4-pro type = %q, want generative", pro.ModelType)
	}
	if pro.MaxInputTokens == nil || *pro.MaxInputTokens != 131072 {
		t.Errorf("deepseek-v4-pro input = %v, want 131072", pro.MaxInputTokens)
	}
	if pro.MaxOutputTokens == nil || *pro.MaxOutputTokens != 65536 {
		t.Errorf("deepseek-v4-pro output = %v, want 65536", pro.MaxOutputTokens)
	}

	gpt := findModel(models, "gpt-4o")
	if gpt == nil {
		t.Fatal("gpt-4o not returned")
	}
	if gpt.ModelType != ModelTypeGenerative {
		t.Errorf("gpt-4o type = %q, want generative", gpt.ModelType)
	}
	if gpt.MaxInputTokens == nil || *gpt.MaxInputTokens != openAICompatibleDefaultInputTokens {
		t.Errorf("gpt-4o input = %v, want default %d", gpt.MaxInputTokens, openAICompatibleDefaultInputTokens)
	}

	emb := findModel(models, "gemini/gemini-embedding-001")
	if emb == nil {
		t.Fatal("gemini/gemini-embedding-001 not returned")
	}
	if emb.ModelType != ModelTypeEmbedding {
		t.Errorf("gemini/gemini-embedding-001 type = %q, want embedding", emb.ModelType)
	}
	if emb.DisplayName != "gemini-embedding-001" {
		t.Errorf("gemini/gemini-embedding-001 display = %q, want %q", emb.DisplayName, "gemini-embedding-001")
	}
	if emb.MaxInputTokens == nil || *emb.MaxInputTokens != 8192 {
		t.Errorf("gemini/gemini-embedding-001 input = %v, want 8192 (from max_model_len)", emb.MaxInputTokens)
	}
}

func TestFetchOpenAICompatibleModelsAddsConfiguredModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4o"}]}`)
	}))
	defer srv.Close()

	svc := newTestCatalogService()
	cred := &ResolvedCredential{
		BaseURL:         srv.URL,
		GenerativeModel: "deepseek-v4-pro",             // not in /v1/models
		EmbeddingModel:  "gemini/gemini-embedding-001", // not in /v1/models
	}

	models, err := svc.fetchOpenAICompatibleModels(context.Background(), ProviderOpenAI, cred)
	if err != nil {
		t.Fatalf("fetchOpenAICompatibleModels() error = %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("got %d models, want 3 (1 fetched + 2 configured): %+v", len(models), models)
	}
	if m := findModel(models, "deepseek-v4-pro"); m == nil || m.ModelType != ModelTypeGenerative {
		t.Errorf("configured generative model missing or misclassified: %+v", m)
	}
	if m := findModel(models, "gemini/gemini-embedding-001"); m == nil || m.ModelType != ModelTypeEmbedding {
		t.Errorf("configured embedding model missing or misclassified: %+v", m)
	}
}

func TestFetchOpenAICompatibleModelsRequiresBaseURL(t *testing.T) {
	svc := newTestCatalogService()
	_, err := svc.fetchOpenAICompatibleModels(context.Background(), ProviderOpenAI, &ResolvedCredential{})
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestConfiguredOpenAIModels(t *testing.T) {
	svc := newTestCatalogService()
	models := svc.configuredOpenAIModels(ProviderOpenAI, &ResolvedCredential{
		GenerativeModel: "deepseek-v4-pro",
		EmbeddingModel:  "gemini/gemini-embedding-001",
	})
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2: %+v", len(models), models)
	}
	for _, m := range models {
		if m.Provider != ProviderOpenAI {
			t.Errorf("configured model %q provider = %q, want %q", m.ModelName, m.Provider, ProviderOpenAI)
		}
	}
	// Duplicate generative/embedding name collapses to one row.
	dup := svc.configuredOpenAIModels(ProviderOpenAI, &ResolvedCredential{
		GenerativeModel: "same-model",
		EmbeddingModel:  "same-model",
	})
	if len(dup) != 1 {
		t.Fatalf("got %d models, want 1 (deduped): %+v", len(dup), dup)
	}
}

func TestDisplayNameForOpenAICompatible(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"deepseek-v4-pro", "deepseek-v4-pro"},
		{"gemini/gemini-embedding-001", "gemini-embedding-001"},
		{"openai/gpt-4o", "gpt-4o"},
		{"", ""},
		{"no-slash", "no-slash"},
	}
	for _, tt := range tests {
		if got := displayNameForOpenAICompatible(tt.input); got != tt.want {
			t.Errorf("displayNameForOpenAICompatible(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
