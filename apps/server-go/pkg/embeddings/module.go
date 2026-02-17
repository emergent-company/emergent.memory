// Package embeddings provides embedding generation functionality.
package embeddings

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/pkg/embeddings/genai"
	"github.com/emergent-company/emergent/pkg/embeddings/vertex"
)

// NewNoopService creates a service with a noop client (for testing)
func NewNoopService(log *slog.Logger) *Service {
	return &Service{
		client:  NewNoopClient(),
		log:     log,
		enabled: false,
	}
}

// Module provides the embeddings fx.Module
var Module = fx.Module("embeddings",
	fx.Provide(NewService),
)

// Service provides embedding generation with automatic client selection
type Service struct {
	client  Client
	log     *slog.Logger
	enabled bool
}

// NewService creates a new embeddings service
func NewService(lc fx.Lifecycle, cfg *config.Config, log *slog.Logger) *Service {
	embCfg := cfg.Embeddings

	if !embCfg.IsEnabled() {
		log.Info("embeddings service disabled - no configuration provided")
		return &Service{
			client:  NewNoopClient(),
			log:     log,
			enabled: false,
		}
	}

	svc := &Service{
		client:  NewNoopClient(), // Will be replaced on start
		log:     log,
		enabled: false,
	}

	// Initialize client on startup
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if embCfg.UseVertexAI() {
				log.Info("initializing Vertex AI embeddings client",
					slog.String("project", embCfg.GCPProjectID),
					slog.String("location", embCfg.VertexAILocation),
					slog.String("model", embCfg.Model),
				)

				client, err := vertex.NewClient(ctx, vertex.Config{
					ProjectID: embCfg.GCPProjectID,
					Location:  embCfg.VertexAILocation,
					Model:     embCfg.Model,
				}, vertex.WithLogger(log))
				if err != nil {
					log.Error("failed to initialize Vertex AI client", slog.String("error", err.Error()))
					// Keep noop client
					return nil // Don't fail startup
				}
				svc.client = client
				svc.enabled = true
				log.Info("Vertex AI embeddings client initialized")
			} else if embCfg.GoogleAPIKey != "" {
				log.Info("initializing Google Generative AI embeddings client",
					slog.String("model", embCfg.Model),
				)

				client, err := genai.NewClient(ctx, genai.Config{
					APIKey: embCfg.GoogleAPIKey,
					Model:  embCfg.Model,
				}, genai.WithLogger(log))
				if err != nil {
					log.Error("failed to initialize Generative AI client", slog.String("error", err.Error()))
					return nil
				}
				svc.client = client
				svc.enabled = true
				log.Info("Google Generative AI embeddings client initialized")
			}
			return nil
		},
	})

	return svc
}

// IsEnabled returns true if embeddings are available
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// EmbedQuery generates an embedding for a single query
func (s *Service) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return s.client.EmbedQuery(ctx, query)
}

// EmbedDocuments generates embeddings for multiple documents
func (s *Service) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	return s.client.EmbedDocuments(ctx, documents)
}

// EmbedQueryWithUsage generates an embedding with usage data (if supported by client)
func (s *Service) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	if c, ok := s.client.(*vertex.Client); ok {
		return c.EmbedQueryWithUsage(ctx, query)
	}
	// Fallback for clients without usage support
	embedding, err := s.client.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return &vertex.EmbedResult{Embedding: embedding}, nil
}

// EmbedDocumentsWithUsage generates embeddings with usage data (if supported by client)
func (s *Service) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	if c, ok := s.client.(*vertex.Client); ok {
		return c.EmbedDocumentsWithUsage(ctx, documents)
	}
	// Fallback for clients without usage support
	embeddings, err := s.client.EmbedDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	return &vertex.BatchEmbedResult{Embeddings: embeddings}, nil
}
