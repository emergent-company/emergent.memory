// Package embeddings provides embedding generation functionality.
package embeddings

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent/internal/config"
	embgenai "github.com/emergent-company/emergent/pkg/embeddings/genai"
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

// serviceParams allows optional injection of an EmbeddingResolver via fx.
type serviceParams struct {
	fx.In

	Lc       fx.Lifecycle
	Cfg      *config.Config
	Log      *slog.Logger
	Resolver EmbeddingResolver `optional:"true"`
}

// Service provides embedding generation with automatic client selection
type Service struct {
	client   Client
	resolver EmbeddingResolver // optional; nil → static config only
	cfg      *config.Config    // kept for per-request transient client creation
	log      *slog.Logger
	enabled  bool
}

// NewService creates a new embeddings service
func NewService(p serviceParams) *Service {
	embCfg := p.Cfg.Embeddings

	if !embCfg.IsEnabled() && p.Resolver == nil {
		p.Log.Info("embeddings service disabled - no configuration provided")
		return &Service{
			client:  NewNoopClient(),
			log:     p.Log,
			enabled: false,
		}
	}

	svc := &Service{
		client:   NewNoopClient(), // Will be replaced on start
		resolver: p.Resolver,
		cfg:      p.Cfg,
		log:      p.Log,
		enabled:  false,
	}

	// Initialize static client on startup (used when resolver returns nil or is absent)
	if embCfg.IsEnabled() {
		p.Lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				if embCfg.UseVertexAI() {
					p.Log.Info("initializing Vertex AI embeddings client",
						slog.String("project", embCfg.GCPProjectID),
						slog.String("location", embCfg.VertexAILocation),
						slog.String("model", embCfg.Model),
					)

					client, err := vertex.NewClient(ctx, vertex.Config{
						ProjectID: embCfg.GCPProjectID,
						Location:  embCfg.VertexAILocation,
						Model:     embCfg.Model,
					}, vertex.WithLogger(p.Log))
					if err != nil {
						p.Log.Error("failed to initialize Vertex AI client", slog.String("error", err.Error()))
						// Keep noop client
						return nil // Don't fail startup
					}
					svc.client = client
					svc.enabled = true
					p.Log.Info("Vertex AI embeddings client initialized")
				} else if embCfg.GoogleAPIKey != "" {
					p.Log.Info("initializing Google Generative AI embeddings client",
						slog.String("model", embCfg.Model),
					)

					client, err := embgenai.NewClient(ctx, embgenai.Config{
						APIKey: embCfg.GoogleAPIKey,
						Model:  embCfg.Model,
					}, embgenai.WithLogger(p.Log))
					if err != nil {
						p.Log.Error("failed to initialize Generative AI client", slog.String("error", err.Error()))
						return nil
					}
					svc.client = client
					svc.enabled = true
					p.Log.Info("Google Generative AI embeddings client initialized")
				}
				return nil
			},
		})
	}

	// If we have a resolver, the service is considered enabled even without static config,
	// because it can resolve credentials per-request.
	if p.Resolver != nil {
		svc.enabled = true
	}

	return svc
}

// IsEnabled returns true if embeddings are available
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// EmbedQuery generates an embedding for a single query.
// If an EmbeddingResolver is configured, per-request DB credentials are used.
func (s *Service) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	client, err := s.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.EmbedQuery(ctx, query)
}

// EmbedDocuments generates embeddings for multiple documents.
// If an EmbeddingResolver is configured, per-request DB credentials are used.
func (s *Service) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	client, err := s.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.EmbedDocuments(ctx, documents)
}

// EmbedQueryWithUsage generates an embedding with usage data (if supported by client)
func (s *Service) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	client, err := s.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(*vertex.Client); ok {
		return c.EmbedQueryWithUsage(ctx, query)
	}
	// Fallback for clients without usage support
	embedding, err := client.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return &vertex.EmbedResult{Embedding: embedding}, nil
}

// EmbedDocumentsWithUsage generates embeddings with usage data (if supported by client)
func (s *Service) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	client, err := s.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(*vertex.Client); ok {
		return c.EmbedDocumentsWithUsage(ctx, documents)
	}
	// Fallback for clients without usage support
	embeddings, err := client.EmbedDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	return &vertex.BatchEmbedResult{Embeddings: embeddings}, nil
}

// resolveClient returns the appropriate embeddings Client for this request.
// If a resolver is configured and returns DB credentials, a transient client is created.
// Otherwise, falls back to the static startup client.
func (s *Service) resolveClient(ctx context.Context) (Client, error) {
	if s.resolver == nil {
		return s.client, nil
	}

	cred, err := s.resolver.ResolveEmbedding(ctx)
	if err != nil {
		s.log.Warn("embedding resolver returned error, falling back to static client",
			slog.String("error", err.Error()),
		)
		return s.client, nil
	}
	if cred == nil {
		// No DB credential — use static client
		return s.client, nil
	}

	// Build a transient client from the resolved credential
	model := cred.EmbeddingModel
	if model == "" && s.cfg != nil {
		model = s.cfg.Embeddings.Model
	}
	if model == "" {
		model = vertex.DefaultModel
	}

	if cred.IsVertexAI {
		opts := []vertex.ClientOption{vertex.WithLogger(s.log)}
		if cred.ServiceAccountJSON != "" {
			opts = append(opts, vertex.WithCredentialsJSON([]byte(cred.ServiceAccountJSON)))
		}
		client, err := vertex.NewClient(ctx, vertex.Config{
			ProjectID: cred.GCPProject,
			Location:  cred.Location,
			Model:     model,
		}, opts...)
		if err != nil {
			return nil, err
		}
		return client, nil
	}

	if cred.IsGoogleAI && cred.APIKey != "" {
		client, err := embgenai.NewClient(ctx, embgenai.Config{
			APIKey: cred.APIKey,
			Model:  model,
		}, embgenai.WithLogger(s.log))
		if err != nil {
			return nil, err
		}
		return client, nil
	}

	// Resolved credential doesn't have usable fields — fall back to static client
	return s.client, nil
}
