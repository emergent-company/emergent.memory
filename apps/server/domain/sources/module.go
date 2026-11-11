package sources

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/gdrive"
)

var Module = fx.Module("sources",
	fx.Provide(
		NewRepository,
		NewConfiguredRegistry,
		ProvideService,
		NewHandler,
		NewDiscoveryStarter,
		NewSourceExtractReader,
		NewSyncWorker,
		NewReviewToolHandler,
	),
	fx.Invoke(
		RegisterRoutes,
		RegisterSyncWorkerLifecycle,
		RegisterSyncSchedulerTask,
		RegisterSourceToolHandler,
		RegisterReviewToolHandler,
		RegisterSourceMCPBackfill,
		RegisterProbeSchedulerTask,
	),
)

// ProvideService builds the production source service. It wires the Drive
// account-email lookup used to label a source's credentials; unit tests build
// the service directly (NewService) and therefore never make the network call.
func ProvideService(repo *Repository, cfg *config.Config, log *slog.Logger, registry *Registry, publisher NotificationPublisher, graphSvc *graph.Service, schemasSvc *schemas.Service, mcpReg *mcpregistry.Service) *Service {
	return NewService(repo, cfg, log, registry,
		WithAccountEmailResolver(driveAccountEmailResolver),
		WithNotificationPublisher(publisher),
		WithGraphService(graphSvc),
		WithObjectTypeProvisioner(NewSchemaProvisioner(schemasSvc)),
		WithSourceMCPServers(mcpReg))
}

// RegisterSourceToolHandler exposes the sources domain as an mcp.Service tool
// handler, so Gmail tools appear on the official /api/mcp surface and in the
// agent tool pool.
func RegisterSourceToolHandler(mcpSvc *mcp.Service, svc *Service) {
	mcpSvc.RegisterSourceToolHandler(svc)
}

// RegisterReviewToolHandler exposes github-pr-diff / github-review-submit. It is
// unconditional: the sources module is always loaded, unlike the githubapp
// module it replaces, so the static catalog never advertises a tool without an
// executor (#579). mcp refuses a second provider claiming these names.
func RegisterReviewToolHandler(mcpSvc *mcp.Service, h *ReviewToolHandler) {
	mcpSvc.RegisterGitHubAppToolHandler(h)
}

// driveAccountEmailResolver reads the account email through a short-lived Drive
// client built from the exchange's access token.
func driveAccountEmailResolver(ctx context.Context, accessToken string) (string, error) {
	return gdrive.AccountEmailFromToken(ctx, accessToken)
}

// discoveryStarterAdapter adapts *discoveryjobs.Service to the DiscoveryStarter
// interface used by the sync worker, building a StartDiscoveryRequest from the
// newly ingested document ids.
type discoveryStarterAdapter struct {
	svc *discoveryjobs.Service
}

func (a *discoveryStarterAdapter) StartDiscovery(ctx context.Context, projectID uuid.UUID, documentIDs []uuid.UUID) error {
	if len(documentIDs) == 0 {
		return nil
	}
	_, err := a.svc.StartDiscovery(ctx, projectID, &discoveryjobs.StartDiscoveryRequest{
		DocumentIDs: documentIDs,
		Trigger:     discoveryjobs.DiscoveryTriggerSourceSync,
	})
	return err
}

// AutoDiscoveryEnabled reports the project's auto-discovery setting, read
// through the discovery domain so the worker needs no project repository.
func (a *discoveryStarterAdapter) AutoDiscoveryEnabled(ctx context.Context, projectID uuid.UUID) (bool, error) {
	return a.svc.AutoDiscoveryEnabled(ctx, projectID)
}

// NewDiscoveryStarter provides DiscoveryStarter backed by the discovery module.
func NewDiscoveryStarter(svc *discoveryjobs.Service) DiscoveryStarter {
	return &discoveryStarterAdapter{svc: svc}
}

// sourceExtractAdapter adapts *projects.Repository to SourceExtractReader so the
// sync worker can honour the project's extraction switch without importing the
// projects domain itself.
type sourceExtractAdapter struct {
	repo *projects.Repository
}

func (a *sourceExtractAdapter) AutoExtractSourceSyncs(ctx context.Context, projectID uuid.UUID) (bool, error) {
	return a.repo.AutoExtractSourceSyncs(ctx, projectID.String())
}

// NewSourceExtractReader provides SourceExtractReader backed by projects.
func NewSourceExtractReader(repo *projects.Repository) SourceExtractReader {
	return &sourceExtractAdapter{repo: repo}
}

// RegisterSyncWorkerLifecycle registers the sync worker with the fx lifecycle.
func RegisterSyncWorkerLifecycle(lc fx.Lifecycle, worker *SyncWorker) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Background context: the fx lifecycle context has a short timeout
			// and would cancel the worker goroutine after startup completes.
			return worker.Start(context.Background())
		},
		OnStop: func(ctx context.Context) error {
			return worker.Stop(ctx)
		},
	})
}

// RegisterSourceMCPBackfill provisions source-backed MCP servers for rows that
// predate the feature. Runs in the background so startup is not blocked.
func RegisterSourceMCPBackfill(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{OnStart: func(context.Context) error {
		go svc.BackfillSourceMCP(context.Background())
		return nil
	}})
}

// SourceEventsModule provides the App-scoped webhook ingress and the source
// event dispatcher. It depends on the agents repository (enqueue) so it is
// loaded only when FEATURE_AGENTS is on.
var SourceEventsModule = fx.Module("source-events",
	fx.Provide(
		NewSourceEventDispatcher,
		NewSourceEventHandler,
	),
	fx.Invoke(RegisterSourceEventRoutes, WireAgentRepository),
)
