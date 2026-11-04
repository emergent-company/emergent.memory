package documents

import (
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/internal/storage"
)

var Module = fx.Module("documents",
	fx.Provide(
		// Adapt *storage.Service to the private uploadStorage interface.
		func(s *storage.Service) uploadStorage { return s },
		NewRepository,
		provideService,
		NewHandler,
		NewUploadHandler,
	),
	fx.Invoke(RegisterRoutes),
)

// provideService wires the chunking and extraction pipeline hooks (provided by
// the extraction module) into the documents service so the ingestion seam can
// chunk and trigger extraction. NewService alone has nil hooks; the handler path
// injects them directly and the seam requires this explicit wiring.
func provideService(
	repo *Repository,
	chunking ChunkingService,
	extractionJobs ExtractionJobCreator,
	notifier DocumentEventNotifier,
	log *slog.Logger,
) *Service {
	svc := NewService(repo, log)
	svc.AttachPipeline(chunking, extractionJobs)
	svc.WithDocumentEventNotifier(notifier)
	return svc
}
