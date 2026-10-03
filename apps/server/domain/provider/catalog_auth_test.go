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
