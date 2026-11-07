package skills

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
)

// Module provides the skills domain.
var Module = fx.Module("skills",
	fx.Provide(
		func(db bun.IDB, log *slog.Logger, embSvc *embeddings.Service, cfg *config.Config) *Repository {
			return NewRepository(db, log,
				WithEmbedder(embSvc),
				WithMaxContentSize(cfg.Skills.MaxContentSizeBytes),
			)
		},
	),
	fx.Provide(NewProposalService),
	fx.Provide(NewCurator),
	fx.Provide(func(repo *Repository, mf *adk.ModelFactory, proposals *ProposalService, log *slog.Logger) *CaptureService {
		cs := NewCaptureService(repo, mf, log)
		cs.SetProposalService(proposals)
		return cs
	}),
	// superadmin is optional: when the superadmin feature module is not loaded,
	// NewHandler receives nil and global skill creation is not superadmin-gated.
	fx.Provide(
		fx.Annotate(
			NewHandler,
			fx.ParamTags(``, ``, `optional:"true"`),
		),
	),
	fx.Invoke(RegisterRoutes),
	fx.Invoke(registerCaptureWorker),
)

// registerCaptureWorker runs the single background review poller for the
// process. Multiple replicas are safe: the claim query uses
// FOR UPDATE SKIP LOCKED.
func registerCaptureWorker(lc fx.Lifecycle, cs *CaptureService) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			cs.Start(context.Background())
			return nil
		},
		OnStop: func(context.Context) error {
			cs.Stop()
			return nil
		},
	})
}
