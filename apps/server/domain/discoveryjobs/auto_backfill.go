package discoveryjobs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/pipeline"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// BackfillTrigger re-extracts documents after a schema change. Implemented by
// *extraction.ObjectExtractionJobsService; discoveryjobs already imports
// extraction (never the reverse), so the dependency is a small interface in the
// same style as PipelinePolicyReader.
type BackfillTrigger interface {
	Backfill(ctx context.Context, projectID string, filter extraction.BackfillFilter, limit int, dryRun bool) (*extraction.BackfillResult, error)
}

// SetBackfillTrigger injects the extraction backfill trigger. Nil-safe: without
// it, auto-apply never re-extracts, matching the "off" policy and the manual
// finalize-only discovery path.
func (s *Service) SetBackfillTrigger(t BackfillTrigger) {
	s.backfill = t
}

// maybeAutoBackfill re-extracts documents after a successful auto-apply, driven
// by Extract.ReExtractOnSchemaChange: off → nothing, incremental → stale-schema
// documents only, full → every document. It is best-effort and never fails the
// enclosing job; a backfill failure is logged and the apply is already durable.
func (s *Service) maybeAutoBackfill(ctx context.Context, projectID uuid.UUID, pol pipeline.Policy) {
	if s.backfill == nil {
		return
	}
	var filter extraction.BackfillFilter
	switch pol.Extract.ReExtractOnSchemaChange {
	case pipeline.ReExtractIncremental:
		filter = extraction.BackfillStaleSchema
	case pipeline.ReExtractFull:
		filter = extraction.BackfillAll
	default: // ReExtractOff or empty → no re-extraction
		return
	}

	limit := pol.Extract.BackfillLimitPerRun
	if limit <= 0 {
		limit = 50
	}

	res, err := s.backfill.Backfill(ctx, projectID.String(), filter, limit, false)
	if err != nil {
		s.log.Warn("auto-backfill: failed",
			slog.String("project_id", projectID.String()),
			slog.String("filter", string(filter)),
			logger.Error(err))
		return
	}
	s.log.Info("auto-backfill: enqueued re-extraction",
		slog.String("project_id", projectID.String()),
		slog.String("filter", string(filter)),
		slog.Int("selected", res.Selected),
		slog.Int("enqueued", res.Enqueued))
}
