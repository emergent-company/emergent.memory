package provider

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// baseURLTestCatalogRepo is a minimal in-memory modelCatalogRepo, so the
// ModelCatalogService under test can answer classification lookups without a
// database. It declares no supported models: the generative model comes from
// the resolved credential.
type baseURLTestCatalogRepo struct{}

func (baseURLTestCatalogRepo) UpsertSupportedModels(context.Context, []ProviderSupportedModel) error {
	return nil
}

func (baseURLTestCatalogRepo) DeleteSupportedModelsNotIn(context.Context, ProviderType, []string) error {
	return nil
}

func (baseURLTestCatalogRepo) ListSupportedModels(context.Context, ProviderType, *ModelType) ([]ProviderSupportedModel, error) {
	return nil, nil
}

func (baseURLTestCatalogRepo) ListAllSupportedModels(context.Context, *ModelType) ([]ProviderSupportedModel, error) {
	return nil, nil
}

func baseURLTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newBaseURLTestService builds the minimal CredentialService needed to exercise
// the real HTTP path in TestGenerate: a catalog wired to a DB-free repo.
func newBaseURLTestService() *CredentialService {
	return &CredentialService{
		catalog: &ModelCatalogService{repo: baseURLTestCatalogRepo{}, log: baseURLTestLogger()},
	}
}

// baseURLCapturedRequest records what the fake OpenAI-compatible server saw.
type baseURLCapturedRequest struct {
	method string
	path   string
	auth   string
	body   map[string]any
}

// TestDeepSeekTestGenerateCustomBaseURLHTTP is a regression test for the custom
// base URL fix: a DeepSeek credential built from an explicit BaseURL must make
// TestGenerate POST to {BaseURL}/chat/completions with the bearer key and the
// prefix-stripped model, and decode the reply. No HTTP client is mocked.
func TestDeepSeekTestGenerateCustomBaseURLHTTP(t *testing.T) {
	var (
		mu       sync.Mutex
		captured []baseURLCapturedRequest
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the configured endpoint is served; any other path 404s so a
		// wrong base URL fails loudly instead of silently succeeding.
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		mu.Lock()
		captured = append(captured, baseURLCapturedRequest{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			body:   body,
		})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
		APIKey:          "test-key",
		BaseURL:         srv.URL + "/v1",
		GenerativeModel: "deepseek/deepseek-v4-flash",
	})

	model, reply, err := s.catalog.TestGenerate(context.Background(), ProviderDeepSeek, cred)
	if err != nil {
		t.Fatalf("TestGenerate() error = %v", err)
	}
	if model != "deepseek-v4-flash" {
		t.Errorf("model = %q, want %q (routing prefix stripped)", model, "deepseek-v4-flash")
	}
	if reply != "ok" {
		t.Errorf("reply = %q, want %q", reply, "ok")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("server saw %d requests, want exactly 1: %+v", len(captured), captured)
	}
	got := captured[0]
	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", got.path)
	}
	if got.auth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", got.auth, "Bearer test-key")
	}
	if got.body["model"] != "deepseek-v4-flash" {
		t.Errorf("body model = %v, want deepseek-v4-flash", got.body["model"])
	}
	if _, ok := got.body["messages"]; !ok {
		t.Errorf("body missing messages: %v", got.body)
	}
}

// TestDeepSeekTestGenerateDefaultBaseURLIsDeepSeek verifies the negative case:
// when BaseURL is unset, buildTempResolvedCred yields the official DeepSeek
// endpoint, and TestGenerate would target it. Asserted without a network call.
func TestDeepSeekTestGenerateDefaultBaseURLIsDeepSeek(t *testing.T) {
	s := newBaseURLTestService()
	cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
		APIKey:          "test-key",
		GenerativeModel: "deepseek-v4-flash",
	})
	if cred.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("BaseURL = %q, want %q", cred.BaseURL, "https://api.deepseek.com/v1")
	}

	// The exact endpoint TestGenerate would build from the default base URL.
	endpoint := strings.TrimSuffix(cred.BaseURL, "/") + "/chat/completions"
	if endpoint != "https://api.deepseek.com/v1/chat/completions" {
		t.Errorf("TestGenerate endpoint = %q, want the DeepSeek default", endpoint)
	}
}

// TestDeepSeekTestGenerateWrongBaseURLFailsLoudly proves the fake server's 404
// guard: a base URL missing the /v1 suffix surfaces an error rather than a
// false success, so the test can catch a regression that ignores BaseURL.
func TestDeepSeekTestGenerateWrongBaseURLFailsLoudly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	s := newBaseURLTestService()
	// Missing the /v1 suffix: TestGenerate POSTs to /chat/completions and the
	// fake server answers 404.
	cred := s.buildTempResolvedCred(ProviderDeepSeek, UpsertProviderConfigRequest{
		APIKey:          "test-key",
		BaseURL:         srv.URL,
		GenerativeModel: "deepseek/deepseek-v4-flash",
	})

	if _, _, err := s.catalog.TestGenerate(context.Background(), ProviderDeepSeek, cred); err == nil {
		t.Fatal("TestGenerate() error = nil, want error for wrong base URL (404)")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want it to mention 404", err)
	}
}
