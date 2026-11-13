// Package embeddings provides embedding generation functionality.
package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	embgenai "github.com/emergent-company/emergent.memory/pkg/embeddings/genai"
	embopenai "github.com/emergent-company/emergent.memory/pkg/embeddings/openai"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

// NewNoopService creates a service with a noop client (for testing)
func NewNoopService(log *slog.Logger) *Service {
	return &Service{
		client:  NewNoopClient(),
		log:     log,
		enabled: false,
	}
}

// NewResolverService creates an embeddings service backed by a per-project
// resolver, without requiring an fx lifecycle. Every embedding call resolves
// the project's provider and model from the resolver; there is no
// environment-configured client to fall back to.
func NewResolverService(resolver EmbeddingResolver, log *slog.Logger) *Service {
	return &Service{
		client:   NewNoopClient(), // only used when no resolver is wired (unit tests)
		resolver: resolver,
		log:      log,
		enabled:  true,
	}
}

// NewTestEmbeddingsService is the test-facing alias for NewResolverService. Used
// by integration tests that need semantic search (search-hybrid) to work with
// actual vectors resolved from each project's provider config.
func NewTestEmbeddingsService(resolver EmbeddingResolver, log *slog.Logger) *Service {
	return NewResolverService(resolver, log)
}

// Module provides the embeddings fx.Module
var Module = fx.Module("embeddings",
	fx.Provide(NewService),
)

// serviceParams allows optional injection of an EmbeddingResolver via fx.
type serviceParams struct {
	fx.In

	Log      *slog.Logger
	Resolver EmbeddingResolver `optional:"true"`
	TestLLM  TestLLMChecker    `optional:"true"`
}

// Service provides embedding generation. The client is always created
// per-request from the project's configured provider (see resolveClientWithMeta);
// embeddings are never sent to an environment-configured provider.
type Service struct {
	client   Client
	resolver EmbeddingResolver // nil only in unit tests that inject a client
	log      *slog.Logger
	testLLM  TestLLMChecker // optional; nil → no test-LLM mode
	enabled  bool

	// warned deduplicates the "no embedding model configured" warning per
	// project, so a search loop does not emit one line per query.
	warned sync.Map
}

// NewService creates a new embeddings service
func NewService(p serviceParams) *Service {
	svc := &Service{
		client:   NewNoopClient(),
		resolver: p.Resolver,
		log:      p.Log,
		testLLM:  p.TestLLM,
	}

	// Without a resolver (or test-LLM mode) every embedding call would have to
	// fail, so the service reports itself disabled and returns no vectors.
	if p.Resolver == nil && p.TestLLM == nil {
		p.Log.Info("embeddings service disabled - no embedding resolver configured")
		return svc
	}

	svc.enabled = true
	return svc
}

// IsEnabled returns true if embeddings are available
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// EmbedQuery generates an embedding for a single query.
// If an EmbeddingResolver is configured, per-request DB credentials are used.
func (s *Service) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if s.isTestLLM(ctx) {
		return cannedEmbedding(query), nil
	}
	client, err := s.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.EmbedQuery(ctx, query)
}

// EmbedDocuments generates embeddings for multiple documents.
// If an EmbeddingResolver is configured, per-request DB credentials are used.
func (s *Service) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	if s.isTestLLM(ctx) {
		vecs := make([][]float32, len(documents))
		for i, d := range documents {
			vecs[i] = cannedEmbedding(d)
		}
		return vecs, nil
	}
	client, err := s.resolveClient(ctx)
	if err != nil {
		return nil, err
	}
	return client.EmbedDocuments(ctx, documents)
}

// usageReportingClient is implemented by embedding clients whose responses
// include token usage: the vertex client, the OpenAI-compatible client
// (OpenAI direct or LiteLLM-style proxies), and the Google Generative AI
// (genai) client. Clients without usage support (noop) fall back to plain
// embedding results.
type usageReportingClient interface {
	EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error)
	EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error)
}

// Compile-time guarantees that the concrete clients selected by
// resolveClientWithMeta are usage-capable and will take the usage-reporting
// branch of EmbedQueryWithUsage / EmbedDocumentsWithUsage.
var (
	_ usageReportingClient = (*vertex.Client)(nil)
	_ usageReportingClient = (*embopenai.Client)(nil)
	_ usageReportingClient = (*embgenai.Client)(nil)
)

// EmbedQueryWithUsage generates an embedding with usage data (if supported by client)
func (s *Service) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	if s.isTestLLM(ctx) {
		return &vertex.EmbedResult{Embedding: cannedEmbedding(query), Model: "test-llm", Provider: "test-llm"}, nil
	}
	client, model, provider, err := s.resolveClientWithMeta(ctx)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(usageReportingClient); ok {
		return c.EmbedQueryWithUsage(ctx, query)
	}
	// Fallback for clients without usage support
	embedding, err := client.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return &vertex.EmbedResult{Embedding: embedding, Model: model, Provider: provider}, nil
}

// EmbedDocumentsWithUsage generates embeddings with usage data (if supported by client)
func (s *Service) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	if s.isTestLLM(ctx) {
		vecs := make([][]float32, len(documents))
		for i, d := range documents {
			vecs[i] = cannedEmbedding(d)
		}
		return &vertex.BatchEmbedResult{Embeddings: vecs, Model: "test-llm", Provider: "test-llm"}, nil
	}
	client, model, provider, err := s.resolveClientWithMeta(ctx)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(usageReportingClient); ok {
		return c.EmbedDocumentsWithUsage(ctx, documents)
	}
	// Fallback for clients without usage support
	embeddings, err := client.EmbedDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	return &vertex.BatchEmbedResult{Embeddings: embeddings, Model: model, Provider: provider}, nil
}

// EmbedDocumentWithUsage embeds one piece of *stored* text with the document
// task, returning it in the single-result shape the ingest workers use.
//
// The distinction matters for the Google clients: a document and a query of the
// same text are deliberately embedded differently (a task_type field, or for
// gemini-embedding-2 a text prefix), so the two are not interchangeable. Ingest
// writes must use this; only a search string goes through EmbedQueryWithUsage.
func (s *Service) EmbedDocumentWithUsage(ctx context.Context, text string) (*vertex.EmbedResult, error) {
	result, err := s.EmbedDocumentsWithUsage(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return &vertex.EmbedResult{
		Embedding: result.Embeddings[0],
		Usage:     result.Usage,
		Model:     result.Model,
		Provider:  result.Provider,
	}, nil
}

// EmbedDocument embeds stored corpus text with the document task and returns the
// vector alone. It is the document-side twin of EmbedQuery, for callers that only
// need the vector (the graph domain embeds relationship triplets this way).
func (s *Service) EmbedDocument(ctx context.Context, text string) ([]float32, error) {
	result, err := s.EmbedDocumentWithUsage(ctx, text)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.Embedding, nil
}

// resolveClient returns the appropriate embeddings Client for this request.
// If a resolver is configured, a transient client is created from the project's
// provider credential. There is no environment-configured fallback.
func (s *Service) resolveClient(ctx context.Context) (Client, error) {
	client, _, _, err := s.resolveClientWithMeta(ctx)
	return client, err
}

// warnNotConfigured logs, once per project, that no embedding provider/model is
// configured so semantic vectors will not be generated. The warning is emitted
// alongside the error the call returns; the error is what callers act on.
func (s *Service) warnNotConfigured(ctx context.Context, reason string) {
	projectID := auth.ProjectIDFromContext(ctx)
	key := projectID
	if key == "" {
		key = "-"
	}
	if _, loaded := s.warned.LoadOrStore(key, struct{}{}); loaded {
		return
	}

	attrs := []any{slog.String("reason", reason)}
	if projectID != "" {
		attrs = append(attrs, slog.String("project_id", projectID))
	}
	s.log.Warn("embeddings: embedding model was not set for this project — semantic vectors will not be generated; configure one with 'memory projects set-models --embedding <provider>/<model>'", attrs...)
}

// resolveClientWithMeta returns the embeddings Client for this request along
// with the model name and provider string used for usage tracking. The client is
// always built from the project credential the resolver returned: embeddings are
// only ever sent to the provider the user configured on the project.
func (s *Service) resolveClientWithMeta(ctx context.Context) (Client, string, string, error) {
	if s.resolver == nil {
		// Only unit tests that inject a client reach this path — the server always
		// wires a resolver, so there is nothing to fall back to.
		return s.client, "", "", nil
	}

	cred, err := s.resolver.ResolveEmbedding(ctx)
	if err != nil {
		if IsNotConfigured(err) {
			s.warnNotConfigured(ctx, err.Error())
		}
		return nil, "", "", fmt.Errorf("embedding credential resolver failed: %w", err)
	}
	if cred == nil {
		err := fmt.Errorf("%w for this request", ErrEmbeddingModelNotSet)
		s.warnNotConfigured(ctx, err.Error())
		return nil, "", "", err
	}

	model := cred.EmbeddingModel
	if model == "" {
		err := fmt.Errorf("%w for provider %q", ErrEmbeddingModelNotSet, cred.ProviderSlug)
		s.warnNotConfigured(ctx, err.Error())
		return nil, "", "", err
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
			return nil, "", "", err
		}
		return client, model, "vertex", nil
	}

	if cred.IsGoogleAI && cred.APIKey != "" {
		client, err := embgenai.NewClient(ctx, embgenai.Config{
			APIKey: cred.APIKey,
			Model:  model,
		}, embgenai.WithLogger(s.log))
		if err != nil {
			return nil, "", "", err
		}
		return client, model, "googleai", nil
	}

	// OpenAI-compatible provider (includes LiteLLM proxies).
	// APIKey + BaseURL are set for openai/deepseek providers.
	if cred.APIKey != "" && cred.BaseURL != "" {
		client, err := embopenai.NewClient(embopenai.Config{
			APIKey:       cred.APIKey,
			BaseURL:      cred.BaseURL,
			Model:        model,
			Dimensions:   EmbeddingDimension, // request 768-dim vectors to match DB vector(768)
			Auth:         adk.AuthStyle(cred.Auth),
			Extra:        cred.Extra,
			ExtraHeaders: cred.ExtraHeaders,
		}, embopenai.WithLogger(s.log))
		if err != nil {
			return nil, "", "", err
		}
		return client, model, "openai", nil
	}

	// The project provider resolved, but the credential is unusable (e.g. a
	// Google provider with no API key). Fail closed rather than quietly sending
	// the request to some other credential.
	return nil, "", "", fmt.Errorf("%w: provider %q has no usable credentials — configure it with 'memory provider configure-project %s --api-key ...'", ErrEmbeddingCredentialUnusable, cred.ProviderSlug, cred.ProviderSlug)
}
