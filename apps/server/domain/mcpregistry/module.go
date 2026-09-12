package mcpregistry

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/crypto"
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
	),
	fx.Invoke(
		RegisterRoutes,
		registerServiceLifecycle,
		registerMCPRegistryToolHandler,
	),
)

// serviceParams bundles dependencies for provideService.
type serviceParams struct {
	fx.In

	Repo           *Repository
	MCPService     *mcp.Service
	RegistryClient *RegistryClient
	Cfg            *config.Config
	Log            *slog.Logger
}

// provideService creates a Service from fx dependencies.
// The encryptor is optional: when LLM_ENCRYPTION_KEY is unset, secret MCP
// env/header values cannot be stored or read (see Service).
func provideService(p serviceParams) *Service {
	var enc *crypto.Encryptor
	if p.Cfg != nil && p.Cfg.LLMProvider.IsEncryptionConfigured() {
		e, err := crypto.NewEncryptor(p.Cfg.LLMProvider.EncryptionKey)
		if err != nil {
			p.Log.Error("failed to initialize MCP secret encryptor", "error", err)
		} else {
			enc = e
		}
	}
	return NewService(p.Repo, p.MCPService, p.RegistryClient, enc, p.Log)
}

// registerMCPRegistryToolHandler wires the MCP registry tool handler into
// mcp.Service after construction (mcpregistry → mcp; deferred to fx.Invoke to
// break the constructor cycle).
func registerMCPRegistryToolHandler(mcpService *mcp.Service, svc *Service, log *slog.Logger) {
	mcpService.RegisterMCPRegistryToolHandler(NewMCPRegistryToolHandler(svc, log))
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
