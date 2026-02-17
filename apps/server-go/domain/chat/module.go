package chat

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/llm/vertex"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Module provides chat functionality
var Module = fx.Module("chat",
	fx.Provide(
		NewRepository,
		NewService,
		NewLLMClient,
		NewHandler,
	),
	fx.Invoke(RegisterRoutes),
)

// NewLLMClient creates a Vertex AI chat client if configured
func NewLLMClient(cfg *config.Config, log *slog.Logger) (*vertex.Client, error) {
	scopedLog := log.With(logger.Scope("chat.llm"))

	if !cfg.LLM.IsEnabled() {
		scopedLog.Warn("LLM client disabled or not configured")
		return nil, nil
	}

	client, err := vertex.NewClient(context.Background(), vertex.Config{
		ProjectID:       cfg.LLM.GCPProjectID,
		Location:        cfg.LLM.VertexAILocation,
		Model:           cfg.LLM.Model,
		Timeout:         cfg.LLM.Timeout,
		Temperature:     cfg.LLM.Temperature,
		MaxOutputTokens: cfg.LLM.MaxOutputTokens,
	}, vertex.WithLogger(scopedLog))

	if err != nil {
		scopedLog.Error("failed to create LLM client", slog.String("error", err.Error()))
		// Return nil client instead of error to allow server to start without LLM
		return nil, nil
	}

	scopedLog.Info("LLM client initialized",
		slog.String("project", cfg.LLM.GCPProjectID),
		slog.String("location", cfg.LLM.VertexAILocation),
		slog.String("model", cfg.LLM.Model),
	)

	return client, nil
}
