package blueprints

import (
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// Module provides the blueprints domain
var Module = fx.Module("blueprints",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	fx.Provide(provideMCPBlueprintToolHandler),
	fx.Invoke(RegisterRoutes),
)

// provideMCPBlueprintToolHandler exposes the blueprint tool handler as
// mcp.BlueprintToolHandler (blueprints → mcp, injected via fx to avoid a setter).
func provideMCPBlueprintToolHandler(svc *Service) mcp.BlueprintToolHandler {
	return NewMCPBlueprintToolHandler(svc)
}
