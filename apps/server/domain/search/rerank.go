package search

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/rerank"
)

// Environment variables that opt search into a vendor reranker. Reranking is
// OFF unless SEARCH_RERANK_PROVIDER is set to a registered vendor whose
// registry definition declares a RerankAPI. The default fused ordering is
// otherwise byte-for-byte unchanged.
const (
	envRerankProvider  = "SEARCH_RERANK_PROVIDER"
	envRerankModel     = "SEARCH_RERANK_MODEL"
	envRerankAPIKey    = "SEARCH_RERANK_API_KEY"
	envRerankBaseURL   = "SEARCH_RERANK_BASE_URL"
	envRerankSecretKey = "SEARCH_RERANK_SECRET_KEY"
	envRerankRegion    = "SEARCH_RERANK_REGION"
	envRerankTopN      = "SEARCH_RERANK_TOP_N"

	defaultSearchRerankTopN = 50
	maxSearchRerankTopN     = 100
)

// rerankTimeout bounds the provider rerank call on the search path, so a slow
// or hung vendor cannot stall a search. On timeout the stage falls back to the
// fused order (D4: rerank never fails a search). It is a var so tests can
// shrink it.
var rerankTimeout = 10 * time.Second

// Option configures the search Service at construction time.
type Option func(*Service)

// WithReranker installs an optional vendor reranker with a top-N bound. The
// default (no option) leaves the existing in-house fused ranking unchanged;
// topN <= 0 disables the stage.
func WithReranker(r rerank.Reranker, topN int) Option {
	return func(s *Service) {
		s.reranker = r
		s.rerankTopN = topN
	}
}

// applyRerank runs the optional post-fusion rerank stage. It is a no-op when
// unconfigured, and on any provider error/timeout it returns the pre-rerank
// fused order so search never fails because of rerank.
func (s *Service) applyRerank(ctx context.Context, query string, items []UnifiedSearchResultItem) []UnifiedSearchResultItem {
	if s.reranker == nil || s.rerankTopN <= 0 || len(items) == 0 {
		return items
	}

	rctx, cancel := context.WithTimeout(ctx, rerankTimeout)
	defer cancel()

	out, ok := rerankItems(rctx, query, items, s.reranker, s.rerankTopN)
	if !ok {
		s.log.Warn("rerank stage fell back to fused order")
	}
	return out
}

// rerankItems re-scores and reorders the top-N items using r. It returns the
// reordered list and whether the rerank actually ran. Only the first
// min(topN, len(items)) items are sent to the provider; the untouched tail is
// appended after the reranked block, preserving its relative order.
//
// On error it returns items unchanged (the pre-rerank fused order), so the
// caller can fall back without a second code path.
func rerankItems(ctx context.Context, query string, items []UnifiedSearchResultItem, r rerank.Reranker, topN int) ([]UnifiedSearchResultItem, bool) {
	if r == nil || topN <= 0 || len(items) == 0 {
		return items, false
	}

	n := topN
	if n > len(items) {
		n = len(items)
	}
	block := items[:n]

	docs := make([]string, n)
	for i := range block {
		docs[i] = searchItemText(block[i])
	}

	results, err := r.Rerank(ctx, query, docs)
	if err != nil {
		return items, false
	}

	seen := make([]bool, n)
	reordered := make([]UnifiedSearchResultItem, 0, len(items))
	for _, res := range results {
		if res.Index < 0 || res.Index >= n || seen[res.Index] {
			continue
		}
		seen[res.Index] = true
		item := block[res.Index]
		item.Score = float32(res.Score)
		reordered = append(reordered, item)
	}
	// Defensively append any block items the reranker omitted, in original order.
	for i := 0; i < n; i++ {
		if !seen[i] {
			reordered = append(reordered, block[i])
		}
	}
	reordered = append(reordered, items[n:]...)
	return reordered, true
}

// searchItemText derives the rerankable text for a fused result: the snippet for
// text chunks, the triplet rendering for relationships, and a Key/Fields
// serialization for graph objects. Falls back to the item ID.
func searchItemText(item UnifiedSearchResultItem) string {
	switch item.Type {
	case ItemTypeText:
		if item.Snippet != "" {
			return item.Snippet
		}
	case ItemTypeRelationship:
		if item.TripletText != "" {
			return item.TripletText
		}
	}
	if item.Key != "" || len(item.Fields) > 0 {
		b, err := json.Marshal(map[string]any{"key": item.Key, "fields": item.Fields})
		if err == nil {
			return string(b)
		}
		return item.Key
	}
	if item.Snippet != "" {
		return item.Snippet
	}
	if item.TripletText != "" {
		return item.TripletText
	}
	return item.ID
}

// rerankerFromEnv builds an opt-in vendor reranker from environment config,
// resolving the vendor through the registry (its RerankAPI selects the wire
// shape) and reusing the provider credential plumbing. It returns (nil, 0) when
// unconfigured or when construction fails; failures are logged and never fatal.
func rerankerFromEnv(registry *provider.Registry, log *slog.Logger) (rerank.Reranker, int) {
	name := strings.TrimSpace(os.Getenv(envRerankProvider))
	if name == "" {
		return nil, 0
	}

	pt := provider.ProviderType(name)
	def, err := registry.Get(pt)
	if err != nil || def.RerankAPI == "" {
		log.Warn("search rerank disabled: vendor does not support reranking", slog.String("provider", name))
		return nil, 0
	}

	topN := defaultSearchRerankTopN
	if v := os.Getenv(envRerankTopN); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			topN = n
		}
	}
	if topN > maxSearchRerankTopN {
		topN = maxSearchRerankTopN
	}

	cred := &provider.ResolvedCredential{
		Provider: pt,
		APIKey:   os.Getenv(envRerankAPIKey),
		BaseURL:  os.Getenv(envRerankBaseURL),
		Extra:    map[string]string{},
	}
	if sk := os.Getenv(envRerankSecretKey); sk != "" {
		cred.Extra["secret_key"] = sk
	}
	if rg := os.Getenv(envRerankRegion); rg != "" {
		cred.Extra["region"] = rg
	}

	r, err := provider.NewReranker(def, cred, os.Getenv(envRerankModel))
	if err != nil {
		log.Warn("search rerank disabled: failed to build reranker", logger.Error(err))
		return nil, 0
	}

	log.Info("search rerank enabled",
		slog.String("provider", name),
		slog.String("model", os.Getenv(envRerankModel)),
		slog.Int("top_n", topN),
	)
	return r, topN
}
