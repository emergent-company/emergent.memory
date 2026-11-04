package provider

import (
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/rerank"
)

// rerankAdapterFor maps the registry's RerankAPI dimension onto the concrete
// pkg/rerank adapter identifier. Keeping the mapping here (rather than in the
// registry) means the registry stays free of adapter dependencies while still
// being the single source of truth for vendor -> wire-shape selection.
func rerankAdapterFor(api RerankAPI) (rerank.API, bool) {
	switch api {
	case RerankAPICohere:
		return rerank.APICohere, true
	case RerankAPIDashScope:
		return rerank.APIDashScope, true
	case RerankAPIVolcengine:
		return rerank.APIVolcengine, true
	case RerankAPITencentLKEAP:
		return rerank.APITencentLKEAP, true
	default:
		return "", false
	}
}

// NewReranker builds a vendor rerank adapter for a registry definition using
// resolved credential material. It reuses the same auth + extra-config plumbing
// as the chat/embedding paths:
//
//   - the bearer credential comes from cred.APIKey;
//   - per-vendor extra fields (e.g. Tencent LKEAP secret_key/region) come from
//     cred.Extra;
//   - header-declared extra fields come from cred.ExtraHeaders;
//   - a nil/empty cred.BaseURL falls back to the definition's rerank default
//     base URL (DefaultBaseURLs[ModelTypeRerank]).
//
// model selects the rerank model; an empty model lets the vendor adapter apply
// its own default.
func NewReranker(def *ProviderDefinition, cred *ResolvedCredential, model string) (rerank.Reranker, error) {
	if def == nil {
		return nil, fmt.Errorf("rerank: nil provider definition")
	}
	api, ok := rerankAdapterFor(def.RerankAPI)
	if !ok {
		return nil, fmt.Errorf("rerank: provider %s does not support reranking", def.Type)
	}
	if cred == nil {
		return nil, fmt.Errorf("rerank: nil credential for provider %s", def.Type)
	}

	baseURL := cred.BaseURL
	if baseURL == "" {
		baseURL = def.DefaultBaseURLs[ModelTypeRerank]
	}

	cfg := rerank.Config{
		API:      api,
		BaseURL:  baseURL,
		APIKey:   cred.APIKey,
		Model:    model,
		Headers:  cred.ExtraHeaders,
		SecretID: cred.APIKey,
	}
	if cred.Extra != nil {
		cfg.SecretKey = cred.Extra["secret_key"]
		if region := cred.Extra["region"]; region != "" {
			cfg.Region = region
		}
	}

	return rerank.New(cfg)
}

// NewRerankerFor resolves the vendor definition for pt from the registry and
// builds a rerank adapter for it.
func (r *Registry) NewRerankerFor(pt ProviderType, cred *ResolvedCredential, model string) (rerank.Reranker, error) {
	def, err := r.Get(pt)
	if err != nil {
		return nil, err
	}
	return NewReranker(def, cred, model)
}
