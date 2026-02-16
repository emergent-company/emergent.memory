package mcpregistry

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent/emergent-core/domain/mcp"
)

// Module provides the MCP registry domain
//
// Features:
// - Central registry of MCP server configurations (builtin + external)
// - Per-tool enable/disable for fine-grained agent tool control
// - REST API at /api/admin/mcp-servers for managing servers and tools
// - REST API at /api/admin/mcp-registry for browsing/installing from official MCP registry
// - MCP tools (delegated via MCPRegistryToolHandler):
//   - list_mcp_servers, get_mcp_server, create_mcp_server, update_mcp_server, delete_mcp_server
//   - toggle_mcp_server_tool, sync_mcp_server_tools
//   - search_mcp_registry, get_mcp_registry_server, install_mcp_from_registry
var Module = fx.Module("mcpregistry",
	fx.Provide(
		NewRepository,
		NewRegistryClient,
		provideService,
		NewHandler,
		provideMCPRegistryToolHandler,
	),
	fx.Invoke(
		RegisterRoutes,
		registerMCPRegistryToolHandler,
		registerServiceLifecycle,
	),
)

// provideService creates a Service from fx dependencies.
func provideService(repo *Repository, mcpService *mcp.Service, registryClient *RegistryClient, log *slog.Logger) *Service {
	return NewService(repo, mcpService, registryClient, log)
}

// provideMCPRegistryToolHandler creates an MCPRegistryToolHandler from fx dependencies.
func provideMCPRegistryToolHandler(svc *Service, log *slog.Logger) *MCPRegistryToolHandler {
	return NewMCPRegistryToolHandler(svc, log)
}

// registerMCPRegistryToolHandler injects the MCPRegistryToolHandler into the MCP Service
// via setter injection to break the circular dependency (mcpregistry → mcp).
func registerMCPRegistryToolHandler(mcpService *mcp.Service, handler *MCPRegistryToolHandler) {
	mcpService.SetMCPRegistryToolHandler(handler)
}

// registerServiceLifecycle registers the service's Close method with the fx lifecycle
// to ensure all proxy connections are cleanly shut down when the server stops.
func registerServiceLifecycle(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			svc.Close()
			return nil
		},
	})
}
