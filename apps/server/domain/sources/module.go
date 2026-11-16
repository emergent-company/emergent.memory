package sources

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/agents"
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
		WireReviewToolWorkObjectStore,
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
		provideSourceWorkObjectStore,
		NewSourceEventDispatcher,
		NewSourceEventHandler,
		NewSourceTriggerInvocationResolver,
		NewSchemaProvisioner,
	),
	fx.Invoke(RegisterSourceEventRoutes, WireAgentRepository, WireSourceWorkObjectStore, WireSourceCredentialMinter, WireWorkInvocationResolver, WireSourceObjectTypes, WireReviewToolReworker),
)

// WireSourceCredentialMinter lets the executor mint a source-scoped GitHub App
// token at run start. The source module owns the App key; the executor only
// names the source and requested permissions.
func WireSourceCredentialMinter(ae *agents.AgentExecutor, h *ReviewToolHandler) {
	if ae != nil && h != nil {
		ae.SetSourceCredentialMinter(h)
	}
}

// WireSourceObjectTypes gives the issue dispatcher the schema provisioner so
// pre-existing github_app sources get the GitHubIssue board type before the
// first issue item is written.
func WireSourceObjectTypes(d *SourceEventDispatcher, p ObjectTypeProvisioner) {
	if d != nil && p != nil {
		d.SetObjectTypes(p)
	}
}

// WireWorkInvocationResolver lets the board re-resolve a source trigger's
// invocation from its server-owned row instead of trusting graph properties.
func WireWorkInvocationResolver(ts *agents.TriggerService, rc *agents.WorkReconciler, wa *agents.WorkActionService, r *SourceTriggerInvocationResolver) {
	if ts != nil && r != nil {
		ts.SetWorkInvocationResolver(r)
	}
	if rc != nil && r != nil {
		rc.SetWorkInvocationResolver(r)
	}
	if wa != nil && r != nil {
		wa.SetWorkInvocationResolver(r)
	}
}

// provideSourceWorkObjectStore binds the graph service to the narrow interface
// the source workflow dispatcher consumes.
func provideSourceWorkObjectStore(svc *graph.Service) SourceWorkObjectStore { return svc }

// WireReviewToolWorkObjectStore lets the review tool finalize a board lane from
// the submitted verdict.
func WireReviewToolWorkObjectStore(h *ReviewToolHandler, svc *graph.Service) {
	if h != nil && svc != nil {
		h.SetWorkItemStatusWriter(svc)
		h.SetWorkObjectStore(svc)
	}
}

// WireReviewToolReworker lets a CHANGES_REQUESTED verdict transition the linked
// issue to revision and enqueue its coding agent with the review feedback.
func WireReviewToolReworker(h *ReviewToolHandler, wa *agents.WorkActionService) {
	if h != nil && wa != nil {
		h.SetWorkItemReworker(wa)
	}
}
