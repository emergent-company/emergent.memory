package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestTestGenerateForModel_GenerativeOverride verifies that
// TestGenerateForModel runs the generate test against exactly the requested
// model, ignoring the credential's configured generative model, and returns the
// LLM reply. It exercises the real direct-HTTP path (DeepSeek) with a fake
// OpenAI-compatible server, following catalog_baseurl_http_test.go.
func TestTestGenerateForModel_GenerativeOverride(t *testing.T) {
	tests := []struct {
		name          string
		configuredGen string
		requestModel  string
	}{
		{name: "override configured model", configuredGen: "deepseek-v4-flash", requestModel: "deepseek-v4-pro"},
		{name: "no configured model", configuredGen: "", requestModel: "deepseek-v4-pro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu        sync.Mutex
				seenModel string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				seenModel, _ = body["model"].(string)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
			}))
			defer srv.Close()

			s := newBaseURLTestService()
			cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
				APIKey:          "test-key",
				BaseURL:         srv.URL + "/v1",
				GenerativeModel: tt.configuredGen,
			})

			reply, err := s.catalog.TestGenerateForModel(t.Context(), ProviderDeepSeek, cred, tt.requestModel)
			if err != nil {
				t.Fatalf("TestGenerateForModel() error = %v", err)
			}
			if reply != "ok" {
				t.Errorf("reply = %q, want %q", reply, "ok")
			}
			mu.Lock()
			defer mu.Unlock()
			if seenModel != tt.requestModel {
				t.Errorf("server saw model = %q, want %q", seenModel, tt.requestModel)
			}
		})
	}
}

// TestTestEmbedForModel_EmbeddingOverride verifies that TestEmbedForModel runs
// the embed test against exactly the requested model (ignoring the configured
// embedding model) and returns the verified model name. It exercises the real
// OpenAI-compatible /embeddings path with a fake server.
func TestTestEmbedForModel_EmbeddingOverride(t *testing.T) {
	var (
		mu        sync.Mutex
		seenModel string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seenModel, _ = body["model"].(string)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderOpenAI, UpsertProviderConfigRequest{
		APIKey:         "test-key",
		BaseURL:        srv.URL + "/v1",
		EmbeddingModel: "text-embedding-3-small", // configured; must be overridden
	})

	got, err := s.catalog.TestEmbedForModel(t.Context(), ProviderOpenAI, cred, "text-embedding-3-large")
	if err != nil {
		t.Fatalf("TestEmbedForModel() error = %v", err)
	}
	if got != "text-embedding-3-large" {
		t.Errorf("verified model = %q, want %q", got, "text-embedding-3-large")
	}
	mu.Lock()
	defer mu.Unlock()
	if seenModel != "text-embedding-3-large" {
		t.Errorf("server saw model = %q, want %q", seenModel, "text-embedding-3-large")
	}
}

// TestTestGenerate_ConfiguredModelUnchanged is a regression test for the
// no-body behaviour: TestGenerate must still resolve the credential's configured
// generative model and produce a reply exactly as before the override API was
// added.
func TestTestGenerate_ConfiguredModelUnchanged(t *testing.T) {
	var (
		mu        sync.Mutex
		seenModel string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		seenModel, _ = body["model"].(string)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
		APIKey:          "test-key",
		BaseURL:         srv.URL + "/v1",
		GenerativeModel: "deepseek-v4-flash",
	})

	model, reply, err := s.catalog.TestGenerate(t.Context(), ProviderDeepSeek, cred)
	if err != nil {
		t.Fatalf("TestGenerate() error = %v", err)
	}
	if model != "deepseek-v4-flash" {
		t.Errorf("model = %q, want %q", model, "deepseek-v4-flash")
	}
	if reply != "ok" {
		t.Errorf("reply = %q, want %q", reply, "ok")
	}
	mu.Lock()
	defer mu.Unlock()
	if seenModel != "deepseek-v4-flash" {
		t.Errorf("server saw model = %q, want %q", seenModel, "deepseek-v4-flash")
	}
}
