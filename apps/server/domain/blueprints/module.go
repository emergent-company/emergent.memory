package blueprints

import (
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// Module provides the blueprints domain
var Module = fx.Module("blueprints",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(NewBuiltinSeeder),
	fx.Provide(provideMCPBlueprintToolHandler),
	fx.Invoke(RegisterRoutes),
	// Register the always-on update-check scheduler tasks (constructed after the
	// scheduler, before its lifecycle starts it).
	fx.Invoke(NewUpdateCheckScheduler),
	fx.Invoke(RegisterBuiltinSeederLifecycle),
	// Wire the embedding service into catalog type resolution (semantic leg).
	fx.Invoke(wireCatalogEmbedder),
)

// wireCatalogEmbedder injects the embeddings service so catalog resolution can
// use the project's embedding model. Resolution degrades to lexical when it is
// disabled or the project has no model configured.
func wireCatalogEmbedder(svc *Service, emb *embeddings.Service) {
	svc.SetCatalogEmbedder(emb)
}

// provideMCPBlueprintToolHandler exposes the blueprint tool handler as
// mcp.BlueprintToolHandler (blueprints → mcp, injected via fx to avoid a setter).
func provideMCPBlueprintToolHandler(svc *Service) mcp.BlueprintToolHandler {
	return NewMCPBlueprintToolHandler(svc)
}
