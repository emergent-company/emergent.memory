package journal

import (
	"context"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// Module provides the journal domain.
var Module = fx.Module("journal",
	fx.Provide(NewRepository),
	fx.Provide(NewService),
	fx.Provide(NewHandler),
	// Provide the journal-backed EventSink so domain/graph can log mutations
	// without importing domain/journal.
	fx.Provide(func(svc *Service) graph.EventSink {
		return NewGraphEventSinkAdapter(svc)
	}),
	fx.Invoke(RegisterRoutes),
	// Stop the async writer pool and drain any queued entries on shutdown.
	fx.Invoke(func(lc fx.Lifecycle, svc *Service) {
		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				svc.Stop(ctx)
				return nil
			},
		})
	}),
)
