package discoveryjobs

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

var Module = fx.Module("discoveryjobs",
	fx.Provide(
		NewRepository,
		NewService,
		NewHandler,
	),
	fx.Invoke(
		RegisterRoutes,
		registerDiscoveryServiceWithMCP,
	),
)

// NewService creates a new discovery jobs service.
// modelFactory is injected by the adk.Module registered in main.go.
func NewService(repo *Repository, cfg *config.Config, modelFactory *adk.ModelFactory, log *slog.Logger) *Service {
	return &Service{
		repo:         repo,
		cfg:          cfg,
		modelFactory: modelFactory,
		log:          log.With(slog.String("scope", "discoveryjobs.svc")),
	}
}

// registerDiscoveryServiceWithMCP injects the discovery service into the MCP service
// so the finalize-discovery tool is available to agents.
func registerDiscoveryServiceWithMCP(mcpService *mcp.Service, svc *Service) {
	mcpService.SetDiscoveryService(svc)
}
