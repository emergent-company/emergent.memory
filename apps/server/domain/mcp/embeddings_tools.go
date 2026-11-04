package mcp

import (
	"context"
	"fmt"
)

// EmbeddingControlHandler is the interface for controlling embedding workers.
// Implemented by extraction.EmbeddingControlHandler to avoid import cycles.
type EmbeddingControlHandler interface {
	CurrentStatus() EmbeddingStatusSnapshot
	PauseAll()
	ResumeAll()
	ApplyConfig(req EmbeddingConfigUpdate)
	ProjectQueueStats(ctx context.Context, projectID string) (EmbeddingProjectQueueStats, error)
	RetriggerFailed(ctx context.Context, projectID string) (EmbeddingRetriggerResult, error)
}

// EmbeddingQueueStats is the per-queue job breakdown for one embedding queue.
// Completed is included so a caller can tell "nothing to do" from "everything
// failed".
type EmbeddingQueueStats struct {
	Pending     int64 `json:"pending"`
	Processing  int64 `json:"processing"`
	Completed   int64 `json:"completed"`
	Failed      int64 `json:"failed"`
	StaleFailed int64 `json:"staleFailed"`
	DeadLetter  int64 `json:"deadLetter,omitempty"`
}

// EmbeddingProjectQueueStats is the project-scoped view of every embedding
// queue. Without this the operator could see the workers were running but not
// that tens of thousands of jobs had failed, so "documents are not searchable"
// had no actionable diagnosis.
type EmbeddingProjectQueueStats struct {
	Chunks        EmbeddingQueueStats `json:"chunks"`
	Objects       EmbeddingQueueStats `json:"objects"`
	Relationships EmbeddingQueueStats `json:"relationships"`
}

// EmbeddingRetriggerResult reports how many failed jobs were requeued.
type EmbeddingRetriggerResult struct {
	ObjectsReset int `json:"objects_reset"`
	ChunksReset  int `json:"chunks_reset"`
}

// EmbeddingStatusSnapshot mirrors extraction.EmbeddingStatusResponse for use in mcp package.
type EmbeddingStatusSnapshot struct {
	Objects       EmbeddingWorkerState `json:"objects"`
	Relationships EmbeddingWorkerState `json:"relationships"`
	Sweep         EmbeddingWorkerState `json:"sweep"`
	Config        EmbeddingConfigState `json:"config"`
}

// EmbeddingWorkerState mirrors extraction.EmbeddingWorkerStatus.
type EmbeddingWorkerState struct {
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
}

// EmbeddingConfigState mirrors extraction.EmbeddingConfigResponse.
type EmbeddingConfigState struct {
	BatchSize             int  `json:"batch_size"`
	Concurrency           int  `json:"concurrency"`
	IntervalMs            int  `json:"interval_ms"`
	StaleMinutes          int  `json:"stale_minutes"`
	EnableAdaptiveScaling bool `json:"enable_adaptive_scaling"`
	MinConcurrency        int  `json:"min_concurrency"`
	MaxConcurrency        int  `json:"max_concurrency"`
}

// EmbeddingConfigUpdate holds optional fields for updating embedding config.
// Pointer fields are only applied when non-nil.
type EmbeddingConfigUpdate struct {
	BatchSize             *int
	Concurrency           *int
	IntervalMs            *int
	StaleMinutes          *int
	EnableAdaptiveScaling *bool
	MinConcurrency        *int
	MaxConcurrency        *int
}

// ============================================================================
// Embeddings Tool Definitions
// ============================================================================

func embeddingsToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:           "embedding-status",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Get the current status of all embedding workers (objects, relationships, sweep). Returns running/paused state and active configuration — but NOT queue depth. Running workers with a backlog of failed jobs look identical to healthy workers here, so call embedding-progress for job counts before diagnosing unsearchable content.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:           "embedding-progress",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Get embedding job counts for this project per queue (documents/chunks, graph objects, graph relationships): pending, processing, completed, failed, staleFailed, deadLetter. Call this when content is missing from semantic search or a document reports few embedded chunks — a large failed count means the fix is embedding-retrigger, not more documents.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {
						Type:        "string",
						Description: "Project UUID. Optional when the call already carries a project context (e.g. an agent run); required from the superadmin endpoint.",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:           "embedding-retrigger",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Reset this project's failed and dead_letter graph-object and chunk embedding jobs back to pending so the workers retry them (the relationship queue is reported by embedding-progress but not requeued here). Use after fixing the cause of the failures (e.g. no embedding model configured, revoked credential). Returns objects_reset and chunks_reset. Does not re-embed chunks that never had a job — use document re-processing for those.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {
						Type:        "string",
						Description: "Project UUID. Optional when the call already carries a project context (e.g. an agent run); required from the superadmin endpoint.",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:           "embedding-pause",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Pause all embedding workers. Embedding jobs will stop being processed until resumed.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:           "embedding-resume",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Resume all embedding workers after they have been paused.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:           "embedding-config-update",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description: "Update embedding worker runtime configuration. All fields are optional — only provided fields are changed. " +
				"Returns the updated status.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"batch_size": {
						Type:        "integer",
						Description: "Number of items to process per batch",
					},
					"concurrency": {
						Type:        "integer",
						Description: "Number of concurrent workers (1–50)",
					},
					"interval_ms": {
						Type:        "integer",
						Description: "Worker polling interval in milliseconds",
					},
					"stale_minutes": {
						Type:        "integer",
						Description: "Minutes before a stale job is re-queued",
					},
					"enable_adaptive_scaling": {
						Type:        "boolean",
						Description: "Enable adaptive concurrency scaling based on error rate",
					},
					"min_concurrency": {
						Type:        "integer",
						Description: "Minimum concurrency when adaptive scaling is enabled",
					},
					"max_concurrency": {
						Type:        "integer",
						Description: "Maximum concurrency when adaptive scaling is enabled (1–50)",
					},
				},
				Required: []string{},
			},
		},
	}
}

// ============================================================================
// Embeddings Tool Handlers
// ============================================================================

func (s *Service) executeGetEmbeddingStatus(_ context.Context) (*ToolResult, error) {
	if s.embeddingCtl == nil {
		return nil, fmt.Errorf("get_embedding_status: embedding control not available")
	}
	return s.wrapResult(s.embeddingCtl.CurrentStatus())
}

// embeddingProjectID resolves the project a queue operation targets: the
// caller's project context when present, otherwise an explicit project_id
// argument (the superadmin endpoint dispatches without a project context).
func embeddingProjectID(projectID string, args map[string]any) (string, error) {
	if projectID != "" {
		return projectID, nil
	}
	if v, ok := args["project_id"].(string); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("project_id is required: this call has no project context")
}

func (s *Service) executeGetEmbeddingProgress(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.embeddingCtl == nil {
		return nil, fmt.Errorf("get_embedding_progress: embedding control not available")
	}
	target, err := embeddingProjectID(projectID, args)
	if err != nil {
		return nil, fmt.Errorf("get_embedding_progress: %w", err)
	}
	stats, err := s.embeddingCtl.ProjectQueueStats(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("get_embedding_progress: %w", err)
	}
	return s.wrapResult(stats)
}

func (s *Service) executeRetriggerEmbeddings(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.embeddingCtl == nil {
		return nil, fmt.Errorf("retrigger_embeddings: embedding control not available")
	}
	target, err := embeddingProjectID(projectID, args)
	if err != nil {
		return nil, fmt.Errorf("retrigger_embeddings: %w", err)
	}
	res, err := s.embeddingCtl.RetriggerFailed(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("retrigger_embeddings: %w", err)
	}
	return s.wrapResult(map[string]any{
		"message":       "failed embedding jobs reset to pending",
		"objects_reset": res.ObjectsReset,
		"chunks_reset":  res.ChunksReset,
	})
}

func (s *Service) executePauseEmbeddings(_ context.Context) (*ToolResult, error) {
	if s.embeddingCtl == nil {
		return nil, fmt.Errorf("pause_embeddings: embedding control not available")
	}
	s.embeddingCtl.PauseAll()
	return s.wrapResult(map[string]any{
		"message": "all embedding workers paused",
		"status":  s.embeddingCtl.CurrentStatus(),
	})
}

func (s *Service) executeResumeEmbeddings(_ context.Context) (*ToolResult, error) {
	if s.embeddingCtl == nil {
		return nil, fmt.Errorf("resume_embeddings: embedding control not available")
	}
	s.embeddingCtl.ResumeAll()
	return s.wrapResult(map[string]any{
		"message": "all embedding workers resumed",
		"status":  s.embeddingCtl.CurrentStatus(),
	})
}

func (s *Service) executeUpdateEmbeddingConfig(_ context.Context, args map[string]any) (*ToolResult, error) {
	if s.embeddingCtl == nil {
		return nil, fmt.Errorf("update_embedding_config: embedding control not available")
	}

	req := EmbeddingConfigUpdate{}
	if v, ok := args["batch_size"].(float64); ok {
		vi := int(v)
		req.BatchSize = &vi
	}
	if v, ok := args["concurrency"].(float64); ok {
		vi := int(v)
		req.Concurrency = &vi
	}
	if v, ok := args["interval_ms"].(float64); ok {
		vi := int(v)
		req.IntervalMs = &vi
	}
	if v, ok := args["stale_minutes"].(float64); ok {
		vi := int(v)
		req.StaleMinutes = &vi
	}
	if v, ok := args["enable_adaptive_scaling"].(bool); ok {
		req.EnableAdaptiveScaling = &v
	}
	if v, ok := args["min_concurrency"].(float64); ok {
		vi := int(v)
		req.MinConcurrency = &vi
	}
	if v, ok := args["max_concurrency"].(float64); ok {
		vi := int(v)
		req.MaxConcurrency = &vi
	}

	s.embeddingCtl.ApplyConfig(req)
	return s.wrapResult(map[string]any{
		"message": "embedding worker config updated",
		"status":  s.embeddingCtl.CurrentStatus(),
	})
}
