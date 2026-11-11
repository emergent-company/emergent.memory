package mcp

import (
	"context"

	"github.com/google/uuid"
)

// ExtractionRunConfig carries the job-scoped defaults an agent-run extraction
// installs for the graph write tools. The agent never has to know the staging
// branch, the extraction job id or the provenance stamps: the tools read them
// from here when the corresponding argument is omitted, so a model mistake can
// neither write to the main graph nor leave an extraction unattributable.
//
// It is installed server-side by the extraction worker, never by a
// model-visible surface. The job's branch, status and provenance are FORCED on
// every write: the tools overwrite model-supplied values, so an agent cannot
// land output on main, change the review status, or attribute work to another
// job.
type ExtractionRunConfig struct {
	// JobID is the kb.object_extraction_jobs id stamped on every object this run
	// creates (graph_objects.extraction_job_id).
	JobID string
	// BranchRef is the staging branch name (or id) write tools fall back to when
	// the agent omits `branch`. Empty means no default (main).
	BranchRef string
	// Status is the status forced on created objects ("suggested") when the
	// agent omits one, so agent-written extraction output is reviewable exactly
	// like pipeline output.
	Status string
	// Provenance is merged into each created object's properties when the agent
	// did not set the key itself (_extraction_job_id, _source_id, _sources,
	// _document_version, _extraction_source).
	Provenance map[string]any
}

type extractionRunContextKey struct{}

// ContextWithExtractionRun installs the job defaults for an agent-run
// extraction.
func ContextWithExtractionRun(ctx context.Context, cfg ExtractionRunConfig) context.Context {
	return context.WithValue(ctx, extractionRunContextKey{}, cfg)
}

// ExtractionRunFromContext returns the installed extraction-run defaults.
func ExtractionRunFromContext(ctx context.Context) (ExtractionRunConfig, bool) {
	cfg, ok := ctx.Value(extractionRunContextKey{}).(ExtractionRunConfig)
	return cfg, ok
}

// extractionJobIDFromContext parses the installed job id for the graph write
// request's internal provenance field. A malformed id is ignored: the object is
// still written and the run's provenance properties remain the source of truth.
func extractionJobIDFromContext(ctx context.Context) *uuid.UUID {
	run, ok := ExtractionRunFromContext(ctx)
	if !ok || run.JobID == "" {
		return nil
	}
	id, err := uuid.Parse(run.JobID)
	if err != nil {
		return nil
	}
	return &id
}
