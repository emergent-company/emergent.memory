package provider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/emergent-company/emergent.memory/pkg/adk"
)

// networkDisabledTestRepo is a DB-free modelCatalogRepo for the kill-switch
// tests. It returns no cached models: every guarded test supplies its model via
// the ResolvedCredential, so no catalog lookup is needed.
type networkDisabledTestRepo struct{}

func (networkDisabledTestRepo) UpsertSupportedModels(context.Context, []ProviderSupportedModel) error {
	return nil
}
func (networkDisabledTestRepo) DeleteSupportedModelsNotIn(context.Context, ProviderType, []string) error {
	return nil
}
func (networkDisabledTestRepo) ListSupportedModels(context.Context, ProviderType, *ModelType) ([]ProviderSupportedModel, error) {
	return nil, nil
}
func (networkDisabledTestRepo) ListAllSupportedModels(context.Context, *ModelType) ([]ProviderSupportedModel, error) {
	return nil, nil
}

func networkDisabledTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// disabledTestCatalog returns a ModelCatalogService with the
// LLM_NETWORK_DISABLED kill switch on.
func disabledTestCatalog() *ModelCatalogService {
	return &ModelCatalogService{
		repo:            networkDisabledTestRepo{},
		log:             networkDisabledTestLogger(),
		networkDisabled: true,
	}
}

// failIfCalledServer starts an HTTP server that fails the test if any request
// reaches it. Pointing a credential's BaseURL at it proves the guarded path
// never issued a request; the returned counter lets callers assert zero calls
// explicitly as well.
func failIfCalledServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		t.Errorf("unexpected outbound HTTP request %s %s — the kill switch did not prevent a network call", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func assertNetworkDisabled(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, adk.ErrNetworkDisabled) {
		t.Fatalf("error = %v, want adk.ErrNetworkDisabled", err)
	}
}

func assertNoRequests(t *testing.T, calls *int32) {
	t.Helper()
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("outbound HTTP calls = %d, want 0", got)
	}
}

// TestCatalog_NetworkDisabled_FetchModelsFromAPI covers the genai-backed
// catalog fetch. The credential carries deliberately invalid service-account
// JSON: with the guard removed, buildClientConfig fails to parse it before any
// network call, so the sentinel assertion still fails (mutation-detected)
// without ever reaching the network.
func TestCatalog_NetworkDisabled_FetchModelsFromAPI(t *testing.T) {
	s := disabledTestCatalog()
	cred := &ResolvedCredential{
		Provider:           ProviderVertexAI,
		GCPProject:         "proj",
		Location:           "us-central1",
		ServiceAccountJSON: `{"not":"a valid service account"}`,
	}

	_, err := s.fetchModelsFromAPI(context.Background(), ProviderVertexAI, cred)
	assertNetworkDisabled(t, err)
}

// TestCatalog_NetworkDisabled_FetchOpenAICompatibleModels proves the
// OpenAI-compatible /v1/models fetch is gated too.
func TestCatalog_NetworkDisabled_FetchOpenAICompatibleModels(t *testing.T) {
	srv, calls := failIfCalledServer(t)
	s := disabledTestCatalog()

	_, err := s.fetchOpenAICompatibleModels(context.Background(), ProviderOpenAI, &ResolvedCredential{
		Provider: ProviderOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "test-key",
	})
	assertNetworkDisabled(t, err)
	assertNoRequests(t, calls)
}

// TestCatalog_NetworkDisabled_TestGenerate covers the OpenAI-compatible
// generate path used by the credential-verify endpoint.
func TestCatalog_NetworkDisabled_TestGenerate(t *testing.T) {
	srv, calls := failIfCalledServer(t)
	s := disabledTestCatalog()
	cred := &ResolvedCredential{
		Provider:        ProviderOpenAI,
		BaseURL:         srv.URL,
		APIKey:          "test-key",
		GenerativeModel: "gpt-4o",
	}

	_, _, err := s.TestGenerate(context.Background(), ProviderOpenAI, cred)
	assertNetworkDisabled(t, err)
	assertNoRequests(t, calls)
}

// TestCatalog_NetworkDisabled_TestGenerateForModel covers the explicit-model
// generate path (the handler's ?model override).
func TestCatalog_NetworkDisabled_TestGenerateForModel(t *testing.T) {
	srv, calls := failIfCalledServer(t)
	s := disabledTestCatalog()
	cred := &ResolvedCredential{
		Provider: ProviderOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "test-key",
	}

	_, err := s.TestGenerateForModel(context.Background(), ProviderOpenAI, cred, "gpt-4o")
	assertNetworkDisabled(t, err)
	assertNoRequests(t, calls)
}

// TestCatalog_NetworkDisabled_TestEmbed covers the configured-embedding-model
// verify path.
func TestCatalog_NetworkDisabled_TestEmbed(t *testing.T) {
	srv, calls := failIfCalledServer(t)
	s := disabledTestCatalog()
	cred := &ResolvedCredential{
		Provider:       ProviderOpenAI,
		BaseURL:        srv.URL,
		APIKey:         "test-key",
		EmbeddingModel: "text-embedding-3-small",
	}

	_, err := s.TestEmbed(context.Background(), ProviderOpenAI, cred)
	assertNetworkDisabled(t, err)
	assertNoRequests(t, calls)
}

// TestCatalog_NetworkDisabled_EmbedContentForModel covers the shared per-model
// embed helper directly (also reached via TestGenerate for embedding models).
func TestCatalog_NetworkDisabled_EmbedContentForModel(t *testing.T) {
	srv, calls := failIfCalledServer(t)
	s := disabledTestCatalog()
	cred := &ResolvedCredential{
		Provider: ProviderOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "test-key",
	}

	_, err := s.embedContentForModel(context.Background(), ProviderOpenAI, cred, "text-embedding-3-small")
	assertNetworkDisabled(t, err)
	assertNoRequests(t, calls)
}

// TestCatalog_NetworkDisabled_GenerateContentForModel covers the shared raw
// generate helper directly.
func TestCatalog_NetworkDisabled_GenerateContentForModel(t *testing.T) {
	srv, calls := failIfCalledServer(t)
	s := disabledTestCatalog()
	cred := &ResolvedCredential{
		Provider: ProviderOpenAI,
		BaseURL:  srv.URL,
		APIKey:   "test-key",
	}

	_, err := s.generateContentForModel(context.Background(), ProviderOpenAI, cred, "gpt-4o")
	assertNetworkDisabled(t, err)
	assertNoRequests(t, calls)
}

// TestCatalog_NetworkEnabled_GuardInactive locks the default: with the flag
// unset the guard is inert, so existing behaviour is byte-for-byte unchanged.
func TestCatalog_NetworkEnabled_GuardInactive(t *testing.T) {
	s := &ModelCatalogService{repo: networkDisabledTestRepo{}, log: networkDisabledTestLogger()}
	if s.NetworkDisabled() {
		t.Fatal("NetworkDisabled() = true, want false by default")
	}
	if err := s.checkNetwork(); err != nil {
		t.Fatalf("checkNetwork() = %v, want nil when the flag is off", err)
	}
}
