package sources

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
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
		NewSyncWorker,
	),
	fx.Invoke(
		RegisterRoutes,
		RegisterSyncWorkerLifecycle,
		RegisterSyncSchedulerTask,
	),
)

// ProvideService builds the production source service. It wires the Drive
// account-email lookup used to label a source's credentials; unit tests build
// the service directly (NewService) and therefore never make the network call.
func ProvideService(repo *Repository, cfg *config.Config, log *slog.Logger, registry *Registry) *Service {
	return NewService(repo, cfg, log, registry, WithAccountEmailResolver(driveAccountEmailResolver))
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
	_, err := a.svc.StartDiscovery(ctx, projectID, &discoveryjobs.StartDiscoveryRequest{DocumentIDs: documentIDs})
	return err
}

// NewDiscoveryStarter provides DiscoveryStarter backed by the discovery module.
func NewDiscoveryStarter(svc *discoveryjobs.Service) DiscoveryStarter {
	return &discoveryStarterAdapter{svc: svc}
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
