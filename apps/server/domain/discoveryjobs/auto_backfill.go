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

// maybeAutoIngest extracts documents that have never been extracted, in auto
// mode. Auto-backfill only runs after a *successful* schema apply, so a new
// document that introduces no new types (auto-apply declines) would otherwise
// never reach the graph and its entities would be silently missing. This runs
// after every discovery in auto mode and enqueues any unextracted document, so
// the graph keeps growing regardless of whether the schema changed. Best-effort
// and nil-safe.
func (s *Service) maybeAutoIngest(ctx context.Context, projectID uuid.UUID) {
	if s.backfill == nil || s.policyReader == nil {
		return
	}
	pol, err := s.policyReader.Policy(ctx, projectID)
	if err != nil {
		s.log.Warn("auto-ingest: policy lookup failed",
			slog.String("project_id", projectID.String()), logger.Error(err))
		return
	}
	if pol.Mode != pipeline.ModeAuto {
		return
	}
	limit := pol.Extract.BackfillLimitPerRun
	if limit <= 0 {
		limit = 50
	}
	res, err := s.backfill.Backfill(ctx, projectID.String(), extraction.BackfillNever, limit, false)
	if err != nil {
		s.log.Warn("auto-ingest: failed to enqueue never-extracted documents",
			slog.String("project_id", projectID.String()), logger.Error(err))
		return
	}
	if res.Enqueued > 0 {
		s.log.Info("auto-ingest: enqueued never-extracted documents",
			slog.String("project_id", projectID.String()),
			slog.Int("enqueued", res.Enqueued))
	}
}
