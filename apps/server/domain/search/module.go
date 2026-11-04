package search

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// Module provides search dependencies via fx
var Module = fx.Module("search",
	fx.Provide(
		NewRepository,
		NewTraceStore,
		provideSearchService,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)

// provideSearchService builds the search service and, when the opt-in
// SEARCH_RERANK_* config names a rerank-capable vendor, installs the vendor
// reranker. Unconfigured leaves the reranker nil (existing ranking unchanged).
func provideSearchService(
	repo *Repository,
	graphService *graph.Service,
	embeddingsSvc *embeddings.Service,
	registry *provider.Registry,
	log *slog.Logger,
	traceStore *TraceStore,
) *Service {
	var opts []Option
	if r, topN := rerankerFromEnv(registry, log); r != nil {
		opts = append(opts, WithReranker(r, topN))
	}
	return NewService(repo, graphService, embeddingsSvc, log, traceStore, opts...)
}
