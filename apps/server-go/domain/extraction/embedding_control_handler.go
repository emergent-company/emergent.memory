package extraction

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// EmbeddingControlHandler exposes HTTP endpoints to pause/resume/inspect
// all embedding workers. Intended for benchmarking and operational use.
type EmbeddingControlHandler struct {
	objectWorker *GraphEmbeddingWorker
	relWorker    *GraphRelationshipEmbeddingWorker
	sweepWorker  *EmbeddingSweepWorker
}

// NewEmbeddingControlHandler creates a new control handler.
func NewEmbeddingControlHandler(
	objectWorker *GraphEmbeddingWorker,
	relWorker *GraphRelationshipEmbeddingWorker,
	sweepWorker *EmbeddingSweepWorker,
) *EmbeddingControlHandler {
	return &EmbeddingControlHandler{
		objectWorker: objectWorker,
		relWorker:    relWorker,
		sweepWorker:  sweepWorker,
	}
}

// EmbeddingWorkerStatus describes the current state of a single worker.
type EmbeddingWorkerStatus struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
}

// EmbeddingStatusResponse is the response for GET /api/embeddings/status.
type EmbeddingStatusResponse struct {
	Objects       EmbeddingWorkerStatus `json:"objects"`
	Relationships EmbeddingWorkerStatus `json:"relationships"`
	Sweep         EmbeddingWorkerStatus `json:"sweep"`
}

func (h *EmbeddingControlHandler) currentStatus() EmbeddingStatusResponse {
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
