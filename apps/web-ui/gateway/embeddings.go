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
// Progress/Status carry the successful responses; ProgressErr/StatusErr carry
// per-section fetch failures. The page renders each section independently so a
// failure in one section (queue counts vs worker state) does not hide the other.
//
// EmbeddingModel/EmbeddingModelMissing carry the effective model config's
// embedding state. Missing is true only when the config fetch succeeded and
// returned an empty embedding model: a failed fetch is "unknown" and shows no
// warning, matching the independent-degradation rule.
type embeddingPageData struct {
	Progress    *EmbeddingProgress
	Status      *EmbeddingStatus
	ProgressErr error
	StatusErr   error
	Empty       bool

	EmbeddingModel        string
	EmbeddingModelMissing bool
}

// uiEmbeddings renders the embeddings status/progress page.
func (s *Server) uiEmbeddings(c echo.Context) error {
	ctx := c.Request().Context()
	var (
		progress *EmbeddingProgress
		status   *EmbeddingStatus
		modelCfg *EffectiveModelConfig
		progErr  error
		statErr  error
		modelErr error
	)
	var g errgroup.Group
	g.Go(func() error { progress, progErr = s.memory.GetEmbeddingProgress(ctx); return nil })
	g.Go(func() error { status, statErr = s.memory.GetEmbeddingStatus(ctx); return nil })
	g.Go(func() error { modelCfg, modelErr = s.memory.GetEffectiveModelConfig(ctx); return nil })
	_ = g.Wait()

	// Report failures to Sentry, but keep rendering: each section degrades
	// independently instead of one failed endpoint blanking the whole page.
	captureError(progErr)
	captureError(statErr)
	captureError(modelErr)

	data := embeddingPageData{
		Progress:    progress,
		Status:      status,
		ProgressErr: progErr,
		StatusErr:   statErr,
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
