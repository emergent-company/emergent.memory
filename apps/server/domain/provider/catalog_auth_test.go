package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchOpenAICompatibleModels_UsesDefinitionAuth verifies the /models
// catalog fetch injects the credential using the vendor's declared auth style
// (api-key for Azure OpenAI), not a hardcoded bearer header.
func TestFetchOpenAICompatibleModels_UsesDefinitionAuth(t *testing.T) {
	var gotAPIKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("api-key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "gpt-4o"}},
		})
	}))
	defer srv.Close()

	svc := &ModelCatalogService{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cred := &ResolvedCredential{
		Provider: ProviderAzureOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "sk-azure-models",
		Auth:     AuthAPIKeyHeader,
	}
	if _, err := svc.fetchOpenAICompatibleModels(context.Background(), ProviderAzureOpenAI, cred); err != nil {
		t.Fatalf("fetchOpenAICompatibleModels() error = %v", err)
	}
	if gotAPIKey != "sk-azure-models" {
		t.Errorf("api-key header = %q, want %q", gotAPIKey, "sk-azure-models")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (api-key vendors must not send bearer)", gotAuth)
	}
}

// TestGenerateContentForModel_UsesDefinitionAuth verifies the test-connection
// generate call injects auth per the vendor definition (api-key for Azure
// OpenAI), matching the ADK runtime's ApplyAuth mechanism.
func TestGenerateContentForModel_UsesDefinitionAuth(t *testing.T) {
	var gotAPIKey, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("api-key")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "hello"}}},
		})
	}))
	defer srv.Close()

	svc := &ModelCatalogService{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cred := &ResolvedCredential{
		Provider: ProviderAzureOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "sk-azure-generate",
		Auth:     AuthAPIKeyHeader,
	}
	if _, err := svc.generateContentForModel(context.Background(), ProviderAzureOpenAI, cred, "gpt-4o"); err != nil {
		t.Fatalf("generateContentForModel() error = %v", err)
	}
	if gotAPIKey != "sk-azure-generate" {
		t.Errorf("api-key header = %q, want %q", gotAPIKey, "sk-azure-generate")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (api-key vendors must not send bearer)", gotAuth)
	}
}

// embeddingAuthServer returns an httptest server that answers POST /embeddings
// and records the two credential headers it received.
func embeddingAuthServer(t *testing.T, gotAPIKey, gotAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			http.NotFound(w, r)
			return
		}
		*gotAPIKey = r.Header.Get("api-key")
		*gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{0.1, 0.2}, "index": 0}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestEmbedContentForModel_UsesDefinitionAuth verifies the embedding test path
// injects auth per the vendor definition: api-key for Azure OpenAI and bearer
// for OpenAI-compatible vendors. Azure declares embedding model types, so this
// path is reachable and previously sent a hardcoded bearer header.
func TestEmbedContentForModel_UsesDefinitionAuth(t *testing.T) {
	t.Run("azure api-key", func(t *testing.T) {
		var gotAPIKey, gotAuth string
		srv := embeddingAuthServer(t, &gotAPIKey, &gotAuth)
		svc := &ModelCatalogService{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		cred := &ResolvedCredential{
			Provider: ProviderAzureOpenAI,
			BaseURL:  srv.URL,
			APIKey:   "sk-azure-embed",
			Auth:     AuthAPIKeyHeader,
		}
		if _, err := svc.TestEmbedForModel(context.Background(), ProviderAzureOpenAI, cred, "text-embedding-3-small"); err != nil {
			t.Fatalf("TestEmbedForModel() error = %v", err)
		}
		if gotAPIKey != "sk-azure-embed" {
			t.Errorf("api-key header = %q, want %q", gotAPIKey, "sk-azure-embed")
		}
		if gotAuth != "" {
			t.Errorf("Authorization header = %q, want empty (Azure must not send bearer)", gotAuth)
		}
	})

	t.Run("openai-compatible bearer", func(t *testing.T) {
		var gotAPIKey, gotAuth string
		srv := embeddingAuthServer(t, &gotAPIKey, &gotAuth)
		svc := &ModelCatalogService{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		cred := &ResolvedCredential{
			Provider: ProviderLiteLLM,
			BaseURL:  srv.URL,
			APIKey:   "sk-litellm-embed",
			Auth:     AuthBearer,
		}
		if _, err := svc.TestEmbedForModel(context.Background(), ProviderLiteLLM, cred, "text-embedding-3-small"); err != nil {
			t.Fatalf("TestEmbedForModel() error = %v", err)
		}
		if gotAuth != "Bearer sk-litellm-embed" {
			t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sk-litellm-embed")
		}
		if gotAPIKey != "" {
			t.Errorf("api-key header = %q, want empty (bearer vendors must not send api-key)", gotAPIKey)
		}
	})
}
