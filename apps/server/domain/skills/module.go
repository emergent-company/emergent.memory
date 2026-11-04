package skills

import (
	"log/slog"

	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/config"
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
	// superadmin is optional: when the superadmin feature module is not loaded,
	// NewHandler receives nil and global skill creation is not superadmin-gated.
	fx.Provide(
		fx.Annotate(
			NewHandler,
			fx.ParamTags(``, ``, `optional:"true"`),
		),
	),
	fx.Invoke(RegisterRoutes),
)
