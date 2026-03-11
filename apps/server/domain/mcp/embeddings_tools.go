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
			Name:        "get_embedding_status",
			Description: "Get the current status of all embedding workers (objects, relationships, sweep). Returns running/paused state and active configuration.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "pause_embeddings",
			Description: "Pause all embedding workers. Embedding jobs will stop being processed until resumed.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name:        "resume_embeddings",
			Description: "Resume all embedding workers after they have been paused.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{},
				Required:   []string{},
			},
		},
		{
			Name: "update_embedding_config",
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
