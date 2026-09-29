package main

import (
	"context"
	"net/http"
	"strings"

	ui "github.com/emergent-company/go-daisy/components/ui"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// --- embedding status/progress ---

// EmbeddingQueueStats describes the queue counts for a single embedding job
// queue (GET /api/embeddings/progress). Failed counts only genuine failures;
// StaleFailed counts terminal rows left by the stale-job sweep, which are
// historical cleanup rather than current breakage.
type EmbeddingQueueStats struct {
	Pending     int64 `json:"pending"`
	Processing  int64 `json:"processing"`
	Completed   int64 `json:"completed"`
	Failed      int64 `json:"failed"`
	StaleFailed int64 `json:"staleFailed"`
	DeadLetter  int64 `json:"deadLetter"`
}

// EmbeddingProgress is the response for GET /api/embeddings/progress: per-queue
// job statistics for object and relationship embeddings.
type EmbeddingProgress struct {
	Objects       EmbeddingQueueStats `json:"objects"`
	Relationships EmbeddingQueueStats `json:"relationships"`
}

// EmbeddingCoverage carries the live-row embedding inventory for a single
// queue: how many rows already hold a vector (Embedded), how many still await
// one (Awaiting), and their live total. Rows are live (non-deleted) only.
type EmbeddingCoverage struct {
	Embedded int64 `json:"embedded"`
	Awaiting int64 `json:"awaiting"`
	Total    int64 `json:"total"`
}

// EmbeddingCoverageResponse is the response for GET /api/embeddings/coverage:
// the embedding inventory per queue. Unlike the job counters it is not subject
// to the terminal-row purge, so an idle, fully-embedded project still reads as
// embedded rather than absent.
type EmbeddingCoverageResponse struct {
	Objects       EmbeddingCoverage `json:"objects"`
	Relationships EmbeddingCoverage `json:"relationships"`
}

// TotalRows reports the combined live object and relationship row count.
func (c EmbeddingCoverageResponse) TotalRows() int64 {
	return c.Objects.Total + c.Relationships.Total
}

// AwaitingRows reports how many combined live rows still lack a vector.
func (c EmbeddingCoverageResponse) AwaitingRows() int64 {
	return c.Objects.Awaiting + c.Relationships.Awaiting
}

// EmbeddingWorkerStatus describes the running/paused state of a single worker.
type EmbeddingWorkerStatus struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
}

// EmbeddingConfig carries the dynamic worker configuration.
type EmbeddingConfig struct {
	BatchSize             int  `json:"batch_size"`
	Concurrency           int  `json:"concurrency"`
	IntervalMs            int  `json:"interval_ms"`
	StaleMinutes          int  `json:"stale_minutes"`
	EnableAdaptiveScaling bool `json:"enable_adaptive_scaling"`
	MinConcurrency        int  `json:"min_concurrency"`
	MaxConcurrency        int  `json:"max_concurrency"`
}

// EmbeddingStatus is the response for GET /api/embeddings/status: worker
// run/pause state plus the active configuration.
type EmbeddingStatus struct {
	Objects       EmbeddingWorkerStatus `json:"objects"`
	Relationships EmbeddingWorkerStatus `json:"relationships"`
	Sweep         EmbeddingWorkerStatus `json:"sweep"`
	Config        EmbeddingConfig       `json:"config"`
}

// GetEmbeddingProgress fetches the per-queue embedding job counts.
func (m *MemoryClient) GetEmbeddingProgress(ctx context.Context) (*EmbeddingProgress, error) {
	var out EmbeddingProgress
	if err := m.doH(ctx, http.MethodGet, "/api/embeddings/progress", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetEmbeddingCoverage fetches the per-queue embedding inventory.
func (m *MemoryClient) GetEmbeddingCoverage(ctx context.Context) (*EmbeddingCoverageResponse, error) {
	var out EmbeddingCoverageResponse
	if err := m.doH(ctx, http.MethodGet, "/api/embeddings/coverage", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetEmbeddingStatus fetches the embedding worker state and configuration.
func (m *MemoryClient) GetEmbeddingStatus(ctx context.Context) (*EmbeddingStatus, error) {
	var out EmbeddingStatus
	if err := m.doH(ctx, http.MethodGet, "/api/embeddings/status", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// embeddingStatusBadge maps a per-object embedding_status to its display label
// and badge intent for the object row and detail header. An empty/unknown
// status yields an empty label (callers render no badge in that case).
func embeddingStatusBadge(status string) (string, ui.BadgeIntent) {
	switch status {
	case "embedded":
		return "Embedded", ui.BadgeSuccess
	case "pending":
		return "Embedding queued", ui.BadgeNeutral
	case "processing":
		return "Embedding…", ui.BadgePrimary
	case "failed":
		return "Embedding failed", ui.BadgeError
	case "dead_letter":
		return "Embedding failed", ui.BadgeWarning
	case "missing":
		return "Not embedded", ui.BadgeGhost
	default:
		return "", ui.BadgeGhost
	}
}

// embeddingStatsZero reports whether a queue has no jobs in any state.
func embeddingStatsZero(s EmbeddingQueueStats) bool {
	return s.Pending == 0 && s.Processing == 0 && s.Completed == 0 && s.Failed == 0 && s.StaleFailed == 0 && s.DeadLetter == 0
}

// workerStateLabel maps a worker's running/paused flags to a display label and
// badge intent for the embeddings status page.
func workerStateLabel(s EmbeddingWorkerStatus) (string, ui.BadgeIntent) {
	switch {
	case s.Paused:
		return "paused", ui.BadgeWarning
	case s.Running:
		return "running", ui.BadgeSuccess
	default:
		return "idle", ui.BadgeNeutral
	}
}

// embeddingProgressZero reports whether every queue is empty (the page's
// "no statistics yet" state).
func embeddingProgressZero(p EmbeddingProgress) bool {
	return embeddingStatsZero(p.Objects) && embeddingStatsZero(p.Relationships)
}

// embeddingPageData carries everything the embeddings status page renders.
//
// Progress/Status/Coverage carry the successful responses; ProgressErr/
// StatusErr/CoverageErr carry per-section fetch failures. The page renders each
// section independently so a failure in one section (queue counts vs worker
// state vs coverage) does not hide the others.
//
// Empty means every queue counter is zero. The template resolves that case
// against Coverage: coverageComplete distinguishes an idle, fully-embedded
// project from one with no embedding data at all.
//
// EmbeddingModel/EmbeddingModelMissing carry the effective model config's
// embedding state. Missing is true only when the config fetch succeeded and
// returned an empty embedding model: a failed fetch is "unknown" and shows no
// warning, matching the independent-degradation rule.
type embeddingPageData struct {
	Progress    *EmbeddingProgress
	Status      *EmbeddingStatus
	Coverage    *EmbeddingCoverageResponse
	ProgressErr error
	StatusErr   error
	CoverageErr error
	Empty       bool

	EmbeddingModel        string
	EmbeddingModelMissing bool
}

// coverageComplete reports whether coverage positively shows no pending
// embedding work: it is available, live rows exist, and none await a vector.
// Nil or errored coverage is not evidence of completion.
func (d embeddingPageData) coverageComplete() bool {
	return d.CoverageErr == nil && d.Coverage != nil &&
		d.Coverage.TotalRows() > 0 && d.Coverage.AwaitingRows() == 0
}

// uiEmbeddings renders the embeddings status/progress page.
func (s *Server) uiEmbeddings(c echo.Context) error {
	ctx := c.Request().Context()
	var (
		progress *EmbeddingProgress
		status   *EmbeddingStatus
		modelCfg *EffectiveModelConfig
		coverage *EmbeddingCoverageResponse
		progErr  error
		statErr  error
		modelErr error
		covErr   error
	)
	var g errgroup.Group
	g.Go(func() error { progress, progErr = s.memory.GetEmbeddingProgress(ctx); return nil })
	g.Go(func() error { status, statErr = s.memory.GetEmbeddingStatus(ctx); return nil })
	g.Go(func() error { modelCfg, modelErr = s.memory.GetEffectiveModelConfig(ctx); return nil })
	g.Go(func() error { coverage, covErr = s.memory.GetEmbeddingCoverage(ctx); return nil })
	_ = g.Wait()

	// Report failures to Sentry, but keep rendering: each section degrades
	// independently instead of one failed endpoint blanking the whole page.
	captureError(progErr)
	captureError(statErr)
	captureError(modelErr)
	captureError(covErr)

	data := embeddingPageData{
		Progress:    progress,
		Status:      status,
		Coverage:    coverage,
		ProgressErr: progErr,
		StatusErr:   statErr,
		CoverageErr: covErr,
	}
	if progErr == nil && (progress == nil || embeddingProgressZero(*progress)) {
		data.Empty = true
	}
	// Warn only when we positively know no embedding model is configured. A
	// failed model-config fetch is "unknown" — show no warning and blank
	// nothing.
	if modelErr == nil && modelCfg != nil {
		data.EmbeddingModel = modelCfg.EmbeddingModel
		data.EmbeddingModelMissing = strings.TrimSpace(modelCfg.EmbeddingModel) == ""
	}
	return s.page(c, pageTitle("Embeddings"), EmbeddingsPage(data))
}
