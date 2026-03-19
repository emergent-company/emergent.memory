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
	fx.Provide(newServiceParams),
)

// serviceParams holds optional fx dependencies for Service.
type serviceParams struct {
	fx.In

	LC       fx.Lifecycle
	Cfg      *config.Config
	Log      *slog.Logger
	Resolver EmbeddingResolver `optional:"true"`
}

// newServiceParams wires Service from fx params (supports optional resolver).
func newServiceParams(p serviceParams) *Service {
	return NewService(p.LC, p.Cfg, p.Log, p.Resolver)
}

// Service provides embedding generation with automatic client selection
type Service struct {
	client   Client
	resolver EmbeddingResolver // nil when no multi-tenant resolver available
	log      *slog.Logger
	enabled  bool
	cfg      *config.EmbeddingsConfig // kept for per-request client creation
}

// NewService creates a new embeddings service
func NewService(lc fx.Lifecycle, cfg *config.Config, log *slog.Logger, resolver EmbeddingResolver) *Service {
	embCfg := cfg.Embeddings

	svc := &Service{
		client:   NewNoopClient(),
		resolver: resolver,
		log:      log,
		enabled:  false,
		cfg:      &embCfg,
	}

	if !embCfg.IsEnabled() {
		if resolver == nil {
			// No env config, no resolver — truly disabled
			log.Info("embeddings service disabled - no configuration provided")
		} else {
			// No env fallback but resolver exists — per-request clients will be created
			log.Info("embeddings service running in multi-tenant mode - no env fallback configured")
			svc.enabled = true // resolver can provide credentials
		}
		return svc
	}

	// Initialize startup client from env config (used as fallback when resolver
	// returns no credentials for a request).
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

// clientForContext returns the appropriate embedding client for the request context.
//
// When a resolver is configured:
//  1. Attempt to resolve credentials from the context (project/org hierarchy).
//  2. If found, build a per-request client from those credentials.
//  3. Fall back to the startup-configured env client if no credentials are resolved.
//
// When no resolver is configured, the startup client is always used.
func (s *Service) clientForContext(ctx context.Context) (Client, error) {
	if s.resolver == nil {
		return s.client, nil
	}

	cred, err := s.resolver.ResolveEmbedding(ctx)
	if err != nil {
		s.log.Debug("embedding resolver returned error, falling back to env client",
			slog.String("error", err.Error()),
		)
		return s.client, nil
	}
	if cred == nil {
		// No per-tenant credential — use the env fallback
		return s.client, nil
	}

	// Build a per-request client from resolved credentials
	return s.buildClientFromCredential(ctx, cred)
}

// buildClientFromCredential creates a transient embedding client from resolved credentials.
func (s *Service) buildClientFromCredential(ctx context.Context, cred *ResolvedEmbeddingCredential) (Client, error) {
	model := cred.EmbeddingModel
	if model == "" && s.cfg != nil {
		model = s.cfg.Model
	}

	if cred.IsVertexAI {
		c, err := vertex.NewClient(ctx, vertex.Config{
			ProjectID: cred.GCPProject,
			Location:  cred.Location,
			Model:     model,
		}, vertex.WithLogger(s.log))
		if err != nil {
			return nil, err
		}
		return c, nil
	}

	if cred.IsGoogleAI && cred.APIKey != "" {
		c, err := genai.NewClient(ctx, genai.Config{
			APIKey: cred.APIKey,
			Model:  model,
		}, genai.WithLogger(s.log))
		if err != nil {
			return nil, err
		}
		return c, nil
	}

	return nil, nil // credential present but incomplete — fall through
}

// IsEnabled returns true if embeddings are available
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// EmbedQuery generates an embedding for a single query
func (s *Service) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	c, err := s.clientForContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.EmbedQuery(ctx, query)
}

// EmbedDocuments generates embeddings for multiple documents
func (s *Service) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	c, err := s.clientForContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.EmbedDocuments(ctx, documents)
}

// EmbedQueryWithUsage generates an embedding with usage data (if supported by client)
func (s *Service) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	c, err := s.clientForContext(ctx)
	if err != nil {
		return nil, err
	}
	if vc, ok := c.(*vertex.Client); ok {
		return vc.EmbedQueryWithUsage(ctx, query)
	}
	// Fallback for clients without usage support
	embedding, err := c.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return &vertex.EmbedResult{Embedding: embedding}, nil
}

// EmbedDocumentsWithUsage generates embeddings with usage data (if supported by client)
func (s *Service) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	c, err := s.clientForContext(ctx)
	if err != nil {
		return nil, err
	}
	if vc, ok := c.(*vertex.Client); ok {
		return vc.EmbedDocumentsWithUsage(ctx, documents)
	}
	// Fallback for clients without usage support
	embeddings, err := c.EmbedDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	return &vertex.BatchEmbedResult{Embeddings: embeddings}, nil
}
