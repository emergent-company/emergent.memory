package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRerankAdapterForMapping(t *testing.T) {
	tests := []struct {
		api  RerankAPI
		want string
		ok   bool
	}{
		{RerankAPICohere, "cohere", true},
		{RerankAPIDashScope, "dashscope", true},
		{RerankAPIVolcengine, "volcengine", true},
		{RerankAPITencentLKEAP, "tencent-lkeap", true},
		{"", "", false},
		{"nope", "", false},
	}
	for _, tt := range tests {
		got, ok := rerankAdapterFor(tt.api)
		assert.Equal(t, tt.ok, ok, "api=%q", tt.api)
		assert.Equal(t, tt.want, string(got), "api=%q", tt.api)
	}
}

func TestNewRerankerRejectsUnsupportedVendor(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.NewRerankerFor(ProviderOpenAI, &ResolvedCredential{}, "m")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support reranking")

	_, err = NewReranker(nil, &ResolvedCredential{}, "m")
	require.Error(t, err)

	_, err = NewReranker(definitionFor(ProviderAliyun), nil, "m")
	require.Error(t, err)
}

// TestNewRerankerSelectsVendorShape proves vendor -> adapter selection by
// pointing each vendor at a stubbed server and asserting the request lands on
// that vendor's distinct wire path.
func TestNewRerankerSelectsVendorShape(t *testing.T) {
	tests := []struct {
		name     string
		provider ProviderType
		wantPath string
		respond  string
	}{
		{
			name:     "cohere",
			provider: ProviderCohere,
			wantPath: "/rerank",
			respond:  `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`,
		},
		{
			name:     "dashscope",
			provider: ProviderAliyun,
			wantPath: "/api/v1/services/rerank/text-rerank/text-rerank",
			respond:  `{"output":{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}}`,
		},
		{
			name:     "volcengine",
			provider: ProviderVolcengine,
			wantPath: "/api/knowledge/service/rerank",
			respond:  `{"code":0,"message":"","data":{"scores":[0.1,0.9]}}`,
		},
		{
			name:     "tencent-lkeap",
			provider: ProviderLKEAP,
			wantPath: "/",
			respond:  `{"Response":{"ScoreList":[0.1,0.9]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.respond))
			}))
			defer srv.Close()

			cred := &ResolvedCredential{Provider: tt.provider, APIKey: "test-key", BaseURL: srv.URL}
			r, err := NewRegistry().NewRerankerFor(tt.provider, cred, "rerank-model")
			require.NoError(t, err)

			results, err := r.Rerank(context.Background(), "query", []string{"doc a", "doc b"})
			require.NoError(t, err)
			require.Len(t, results, 2)

			assert.Equal(t, tt.wantPath, gotPath, "vendor %s hit the wrong endpoint", tt.name)
			// All shapes must normalize to best-first ordering.
			assert.Equal(t, 1, results[0].Index)
			assert.InDelta(t, 0.9, results[0].Score, 1e-9)
		})
	}
}

// TestNewRerankerUsesDefaultBaseURL confirms the definition's rerank default
// base URL is used when the credential carries none.
func TestNewRerankerUsesDefaultBaseURL(t *testing.T) {
	def := definitionFor(ProviderCohere)
	require.NotNil(t, def)
	assert.NotEmpty(t, def.DefaultBaseURLs[ModelTypeRerank])

	// Build with an empty base URL; New must succeed (endpoint comes from the
	// definition's default, which is not dialed here).
	r, err := NewReranker(def, &ResolvedCredential{APIKey: "k"}, "m")
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestResolvedCredentialExtraPlumbing confirms Tencent's extra fields flow into
// the adapter config through the shared plumbing (no panic / still builds).
func TestResolvedCredentialExtraPlumbing(t *testing.T) {
	cred := &ResolvedCredential{
		Provider:     ProviderLKEAP,
		APIKey:       "secret-id",
		Extra:        map[string]string{"secret_key": "secret-key", "region": "ap-shanghai"},
		ExtraHeaders: map[string]string{"X-Custom": "v"},
	}
	r, err := NewRegistry().NewRerankerFor(ProviderLKEAP, cred, "lke-reranker-base")
	require.NoError(t, err)
	require.NotNil(t, r)
}

// Guard: the RerankAPI JSON key is stable for API consumers.
func TestRerankAPIJSONKey(t *testing.T) {
	b, err := json.Marshal(&ProviderDefinition{Type: ProviderCohere, RerankAPI: RerankAPICohere})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"rerankApi":"cohere"`)
}

// TestProviderDefinitionResponseExposesRerankAPI guards the API-facing
// projection: the registry definition's RerankAPI must survive the handler's
// response mapping, not just direct struct marshalling.
func TestProviderDefinitionResponseExposesRerankAPI(t *testing.T) {
	def := definitionFor(ProviderCohere)
	require.NotNil(t, def)

	resp := toProviderDefinitionResponse(def)
	assert.Equal(t, RerankAPICohere, resp.RerankAPI)

	b, err := json.Marshal(&resp)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"rerankApi":"cohere"`)
}

// TestStaticModelsCohereRerankOnly guards that the rerank-only Cohere vendor
// does not inherit the provider-agnostic Google generative/embedding list when
// its catalog is synced (a project provider configure calls SyncModels for any
// provider).
func TestStaticModelsCohereRerankOnly(t *testing.T) {
	models := staticModels(ProviderCohere)
	require.Len(t, models, 1)
	assert.Equal(t, "rerank-v3.5", models[0].ModelName)
	assert.Equal(t, ModelTypeRerank, models[0].ModelType)
	assert.Equal(t, ProviderCohere, models[0].Provider)

	for _, m := range models {
		assert.NotEqual(t, ModelTypeGenerative, m.ModelType, "rerank-only Cohere must not expose generative models")
		assert.NotContains(t, m.ModelName, "gemini", "rerank-only Cohere must not expose Gemini models")
	}
}
