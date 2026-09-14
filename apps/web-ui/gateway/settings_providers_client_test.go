package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- MemoryClient provider-configuration methods ---

func TestListProjectProviders(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"pc1","projectId":"proj-1","provider":"openai","generativeModel":"gpt-4o","embeddingModel":"text-embedding-3-small","createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"},
			{"id":"pc2","projectId":"proj-1","provider":"deepseek","baseUrl":"https://api.example.com"}
		]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	got, err := m.ListProjectProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/projects/proj-1/providers" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if len(got) != 2 {
		t.Fatalf("got %d providers, want 2", len(got))
	}
	if got[0].Provider != "openai" || got[0].GenerativeModel != "gpt-4o" || got[0].EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("provider[0] = %+v", got[0])
	}
	if got[1].BaseURL != "https://api.example.com" {
		t.Errorf("provider[1].BaseURL = %q", got[1].BaseURL)
	}
}

func TestListProviderModels(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"m1","provider":"deepseek","modelName":"deepseek-v4-pro","modelType":"generative","displayName":"DeepSeek V4 Pro","maxOutputTokens":65536,"maxInputTokens":131072,"lastSynced":"2026-09-01T00:00:00Z"},
			{"id":"m2","provider":"deepseek","modelName":"deepseek-v3","modelType":"generative","displayName":"DeepSeek V3"}
		]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	got, err := m.ListProviderModels(context.Background(), "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/providers/deepseek/models" {
		t.Errorf("path = %q", gotPath)
	}
	if len(got) != 2 {
		t.Fatalf("got %d models, want 2", len(got))
	}
	if got[0].ModelName != "deepseek-v4-pro" || got[0].DisplayName != "DeepSeek V4 Pro" {
		t.Errorf("model[0] = %+v", got[0])
	}
	if got[0].MaxOutputTokens == nil || *got[0].MaxOutputTokens != 65536 {
		t.Errorf("model[0].MaxOutputTokens = %v", got[0].MaxOutputTokens)
	}
	if got[1].MaxOutputTokens != nil {
		t.Errorf("absent maxOutputTokens should decode nil, got %v", got[1].MaxOutputTokens)
	}
}

func TestListPricing(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"pr1","provider":"openai","model":"gpt-4o","textInputPrice":2.5,"imageInputPrice":0,"videoInputPrice":0,"audioInputPrice":0,"outputPrice":10,"lastSynced":"2026-09-01T00:00:00Z"},
			{"id":"pr2","provider":"deepseek","model":"deepseek-v4-pro","textInputPrice":0.28,"outputPrice":0.42}
		]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	got, err := m.ListPricing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/pricing" {
		t.Errorf("path = %q", gotPath)
	}
	if len(got) != 2 {
		t.Fatalf("got %d pricing rows, want 2", len(got))
	}
	if got[0].Provider != "openai" || got[0].Model != "gpt-4o" || got[0].TextInputPrice != 2.5 || got[0].OutputPrice != 10 {
		t.Errorf("pricing[0] = %+v", got[0])
	}
	if got[1].TextInputPrice != 0.28 || got[1].OutputPrice != 0.42 {
		t.Errorf("pricing[1] = %+v", got[1])
	}
}

func TestListProjectPricingOverrides(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"id":"o1","projectId":"proj-1","provider":"openai","model":"gpt-4o","textInputPrice":1.0,"imageInputPrice":0,"videoInputPrice":0,"audioInputPrice":0,"outputPrice":4.0,"createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"}
		]`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	got, err := m.ListProjectPricingOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/projects/proj-1/pricing-overrides" {
		t.Errorf("path = %q", gotPath)
	}
	if len(got) != 1 {
		t.Fatalf("got %d overrides, want 1", len(got))
	}
	if got[0].Provider != "openai" || got[0].Model != "gpt-4o" || got[0].TextInputPrice != 1.0 || got[0].OutputPrice != 4.0 {
		t.Errorf("override = %+v", got[0])
	}
}

func TestListProjectPricingOverridesNull(t *testing.T) {
	// Memory returns JSON null when there are no overrides — decode as empty.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `null`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	got, err := m.ListProjectPricingOverrides(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("null overrides should decode to an empty slice, got %#v", got)
	}
}

func TestUpsertProjectPricingOverride(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"o1","projectId":"proj-1","provider":"openai","model":"gpt-4o","textInputPrice":1.5,"imageInputPrice":0,"videoInputPrice":0,"audioInputPrice":0,"outputPrice":6,"createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	rates := modelPriceRates{TextInputPrice: 1.5, OutputPrice: 6}
	got, err := m.UpsertProjectPricingOverride(context.Background(), "openai", "gpt-4o", rates)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/v1/projects/proj-1/pricing-overrides" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
	if gotBody["provider"] != "openai" || gotBody["model"] != "gpt-4o" {
		t.Errorf("body provider/model = %v / %v", gotBody["provider"], gotBody["model"])
	}
	for _, k := range []string{"textInputPrice", "imageInputPrice", "videoInputPrice", "audioInputPrice", "outputPrice"} {
		if _, ok := gotBody[k]; !ok {
			t.Errorf("body missing %q: %v", k, gotBody)
		}
	}
	if gotBody["textInputPrice"] != 1.5 || gotBody["outputPrice"] != float64(6) {
		t.Errorf("body rates = %v", gotBody)
	}
	if got.Provider != "openai" || got.Model != "gpt-4o" || got.TextInputPrice != 1.5 {
		t.Errorf("saved override = %+v", got)
	}
}

func TestDeleteProjectPricingOverride(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := NewMemoryClient(srv.URL, "tok", "proj-1")
	if err := m.DeleteProjectPricingOverride(context.Background(), "openai", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/projects/proj-1/pricing-overrides/openai/gpt-4o" {
		t.Errorf("got %s %s", gotMethod, gotPath)
	}
}

func TestDeleteProjectPricingOverrideError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"code":"upstream","message":"down"}}`)
	}))
	defer srv.Close()
	m := NewMemoryClient(srv.URL, "tok", "proj")
	if err := m.DeleteProjectPricingOverride(context.Background(), "openai", "gpt-4o"); err == nil {
		t.Fatal("want error, got nil")
	} else if !strings.Contains(err.Error(), "down") {
		t.Errorf("error should surface the backend message, got %v", err)
	}
}
