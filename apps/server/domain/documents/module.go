package documents

import (
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/storage"
)

var Module = fx.Module("documents",
	fx.Provide(
		// Adapt *storage.Service to the private uploadStorage interface.
		func(s *storage.Service) uploadStorage { return s },
		NewRepository,
		NewService,
		NewHandler,
		NewUploadHandler,
	),
	fx.Invoke(RegisterRoutes),
)
