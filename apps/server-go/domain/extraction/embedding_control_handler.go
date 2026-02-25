package extraction

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent/domain/scheduler"
)

// EmbeddingControlHandler exposes HTTP endpoints to pause/resume/inspect
// all embedding workers. Intended for benchmarking and operational use.
type EmbeddingControlHandler struct {
	objectWorker *GraphEmbeddingWorker
	relWorker    *GraphRelationshipEmbeddingWorker
	sweepWorker  *EmbeddingSweepWorker
	staleTask    *scheduler.StaleJobCleanupTask
}

// NewEmbeddingControlHandler creates a new control handler.
func NewEmbeddingControlHandler(
	objectWorker *GraphEmbeddingWorker,
	relWorker *GraphRelationshipEmbeddingWorker,
	sweepWorker *EmbeddingSweepWorker,
	staleTask *scheduler.StaleJobCleanupTask,
) *EmbeddingControlHandler {
	return &EmbeddingControlHandler{
		objectWorker: objectWorker,
		relWorker:    relWorker,
		sweepWorker:  sweepWorker,
		staleTask:    staleTask,
	}
}

// EmbeddingWorkerStatus describes the current state of a single worker.
type EmbeddingWorkerStatus struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
}

// EmbeddingConfigResponse describes the current dynamic config.
type EmbeddingConfigResponse struct {
	BatchSize    int `json:"batch_size"`
	Concurrency  int `json:"concurrency"`
	IntervalMs   int `json:"interval_ms"`
	StaleMinutes int `json:"stale_minutes"`
}

// EmbeddingStatusResponse is the response for GET /api/embeddings/status.
type EmbeddingStatusResponse struct {
	Objects       EmbeddingWorkerStatus   `json:"objects"`
	Relationships EmbeddingWorkerStatus   `json:"relationships"`
	Sweep         EmbeddingWorkerStatus   `json:"sweep"`
	Config        EmbeddingConfigResponse `json:"config"`
}

func (h *EmbeddingControlHandler) currentStatus() EmbeddingStatusResponse {
	cfg := h.objectWorker.GetConfig()
	staleMinutes := 30
	if h.staleTask != nil {
		staleMinutes = h.staleTask.GetStaleMinutes()
	}
	return EmbeddingStatusResponse{
		Objects: EmbeddingWorkerStatus{
			Running: h.objectWorker.IsRunning(),
			Paused:  h.objectWorker.IsPaused(),
		},
		Relationships: EmbeddingWorkerStatus{
			Running: h.relWorker.IsRunning(),
			Paused:  h.relWorker.IsPaused(),
		},
		Sweep: EmbeddingWorkerStatus{
			Running: h.sweepWorker.IsRunning(),
			Paused:  h.sweepWorker.IsPaused(),
		},
		Config: EmbeddingConfigResponse{
			BatchSize:    cfg.WorkerBatchSize,
			Concurrency:  cfg.WorkerConcurrency,
			IntervalMs:   cfg.WorkerIntervalMs,
			StaleMinutes: staleMinutes,
		},
	}
}

// Status returns the current pause/run state of all embedding workers.
// @Router /api/embeddings/status [get]
func (h *EmbeddingControlHandler) Status(c echo.Context) error {
	return c.JSON(http.StatusOK, h.currentStatus())
}

// Pause pauses all embedding workers (object, relationship, sweep).
// @Router /api/embeddings/pause [post]
func (h *EmbeddingControlHandler) Pause(c echo.Context) error {
	h.objectWorker.Pause()
	h.relWorker.Pause()
	h.sweepWorker.Pause()
	return c.JSON(http.StatusOK, map[string]any{
		"message": "all embedding workers paused",
		"status":  h.currentStatus(),
	})
}

// Resume resumes all embedding workers.
// @Router /api/embeddings/resume [post]
func (h *EmbeddingControlHandler) Resume(c echo.Context) error {
	h.objectWorker.Resume()
	h.relWorker.Resume()
	h.sweepWorker.Resume()
	return c.JSON(http.StatusOK, map[string]any{
		"message": "all embedding workers resumed",
		"status":  h.currentStatus(),
	})
}

// EmbeddingConfigRequest is the body for PATCH /api/embeddings/config.
type EmbeddingConfigRequest struct {
	BatchSize    *int `json:"batch_size"`
	Concurrency  *int `json:"concurrency"`
	IntervalMs   *int `json:"interval_ms"`
	StaleMinutes *int `json:"stale_minutes"`
}

// Config updates embedding worker configuration at runtime.
// All fields are optional — only provided fields are changed.
// @Router /api/embeddings/config [patch]
func (h *EmbeddingControlHandler) Config(c echo.Context) error {
	var req EmbeddingConfigRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
	}

	// Apply to both workers (they share the same config shape)
	objCfg := h.objectWorker.GetConfig()
	relCfg := h.relWorker.GetConfig()

	if req.BatchSize != nil {
		objCfg.WorkerBatchSize = *req.BatchSize
		relCfg.WorkerBatchSize = *req.BatchSize
	}
	if req.Concurrency != nil {
		objCfg.WorkerConcurrency = *req.Concurrency
		relCfg.WorkerConcurrency = *req.Concurrency
	}
	if req.IntervalMs != nil {
		objCfg.WorkerIntervalMs = *req.IntervalMs
		relCfg.WorkerIntervalMs = *req.IntervalMs
	}

	h.objectWorker.SetConfig(objCfg)
	h.relWorker.SetConfig(relCfg)

	if req.StaleMinutes != nil && h.staleTask != nil {
		h.staleTask.SetStaleMinutes(*req.StaleMinutes)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message": "embedding worker config updated",
		"status":  h.currentStatus(),
	})
}
