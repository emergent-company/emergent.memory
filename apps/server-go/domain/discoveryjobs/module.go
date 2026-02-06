package discoveryjobs

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/llm"
	"github.com/emergent/emergent-core/pkg/llm/vertex"
	"github.com/emergent/emergent-core/pkg/logger"
)

var Module = fx.Module("discoveryjobs",
	fx.Provide(
		NewRepository,
		NewLLMProvider,
		NewService,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)

// NewLLMProvider creates an LLM provider for discovery jobs
func NewLLMProvider(cfg *config.Config, log *slog.Logger) llm.Provider {
	scopedLog := log.With(logger.Scope("discoveryjobs.llm"))

	if !cfg.LLM.IsEnabled() {
		scopedLog.Warn("LLM provider disabled or not configured - discovery jobs will not be functional")
		return nil
	}

	client, err := vertex.NewClient(context.Background(), vertex.Config{
		ProjectID:       cfg.LLM.GCPProjectID,
		Location:        cfg.LLM.VertexAILocation,
		Model:           cfg.LLM.Model,
		Timeout:         cfg.LLM.Timeout,
		Temperature:     0.0, // Deterministic for discovery
		MaxOutputTokens: 65535,
	}, vertex.WithLogger(scopedLog))

	if err != nil {
		scopedLog.Error("failed to create LLM provider", slog.String("error", err.Error()))
		return nil
	}

	scopedLog.Info("LLM provider initialized for discovery",
		slog.String("project", cfg.LLM.GCPProjectID),
		slog.String("location", cfg.LLM.VertexAILocation),
		slog.String("model", cfg.LLM.Model),
	)

	return client
}
