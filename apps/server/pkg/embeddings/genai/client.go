// Package genai provides a Google Generative AI embeddings client.
package genai

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"google.golang.org/genai"

	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

const (
	// DefaultModel is the default embedding model
	DefaultModel = "gemini-embedding-2-preview"

	// DefaultDimension is the embedding dimension (gemini-embedding-2-preview supports MRL with recommended 768 dims)
	DefaultDimension = 768

	// DefaultMaxRetries is the default number of retries
	DefaultMaxRetries = 3

	// DefaultBaseDelay is the base delay for exponential backoff
	DefaultBaseDelay = 100 * time.Millisecond

	// DefaultMaxDelay is the maximum delay for exponential backoff
	DefaultMaxDelay = 10 * time.Second

	// DefaultBatchSize is the maximum batch size per request
	// Google Generative AI supports up to 100 texts per request
	DefaultBatchSize = 100
)

// Config holds the configuration for the Generative AI client
type Config struct {
	APIKey string
	Model  string
}

// Client is a Google Generative AI embeddings client
type Client struct {
	client *genai.Client
	model  string
	log    *slog.Logger

	// Retry configuration
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

// ClientOption configures the Client
type ClientOption func(*Client)

// WithMaxRetries sets the maximum number of retries
func WithMaxRetries(n int) ClientOption {
	return func(c *Client) {
		c.maxRetries = n
	}
}

// WithBaseDelay sets the base delay for exponential backoff
func WithBaseDelay(d time.Duration) ClientOption {
	return func(c *Client) {
		c.baseDelay = d
	}
}

// WithMaxDelay sets the maximum delay for exponential backoff
func WithMaxDelay(d time.Duration) ClientOption {
	return func(c *Client) {
		c.maxDelay = d
	}
}

// WithLogger sets the logger
func WithLogger(log *slog.Logger) ClientOption {
	return func(c *Client) {
		c.log = log
	}
}

// NewClient creates a new Google Generative AI embeddings client
func NewClient(ctx context.Context, cfg Config, opts ...ClientOption) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	// Create genai client with API key
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	c := &Client{
		client:     client,
		model:      cfg.Model,
		log:        slog.Default(),
		maxRetries: DefaultMaxRetries,
		baseDelay:  DefaultBaseDelay,
		maxDelay:   DefaultMaxDelay,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// EmbedQuery generates an embedding for a single query
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	result, err := c.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return result.Embedding, nil
}

// EmbedDocuments generates embeddings for multiple documents
func (c *Client) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	result, err := c.EmbedDocumentsWithUsage(ctx, documents)
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

// EmbedQueryWithUsage generates an embedding for a single query and reports
// the token usage returned by the API. Provider is "googleai" (the Gemini
// Developer API / Google Generative AI backend).
func (c *Client) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	embeddings, tokens, err := c.embedWithRetry(ctx, []string{query}, "RETRIEVAL_QUERY")
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return &vertex.EmbedResult{
		Embedding: embeddings[0],
		Usage:     &vertex.Usage{PromptTokens: tokens, TotalTokens: tokens},
		Model:     c.model,
		Provider:  "googleai",
	}, nil
}

// EmbedDocumentsWithUsage generates embeddings for multiple documents and
// reports the summed token usage returned by the API. Provider is "googleai".
func (c *Client) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	if len(documents) == 0 {
		return &vertex.BatchEmbedResult{
			Embeddings: [][]float32{},
			Usage:      &vertex.Usage{},
			Model:      c.model,
			Provider:   "googleai",
		}, nil
	}

	// Process in batches
	var allEmbeddings [][]float32
	totalTokens := 0

	for i := 0; i < len(documents); i += DefaultBatchSize {
		end := i + DefaultBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		batch := documents[i:end]

		embeddings, tokens, err := c.embedWithRetry(ctx, batch, "RETRIEVAL_DOCUMENT")
		if err != nil {
			return nil, fmt.Errorf("failed to embed batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
		totalTokens += tokens
	}

	return &vertex.BatchEmbedResult{
		Embeddings: allEmbeddings,
		Usage:      &vertex.Usage{PromptTokens: totalTokens, TotalTokens: totalTokens},
		Model:      c.model,
		Provider:   "googleai",
	}, nil
}

// embedWithRetry embeds a batch of texts with retry logic, returning the
// embeddings and the summed prompt-token count reported by the API (0 when
// the response carries no usage metadata).
func (c *Client) embedWithRetry(ctx context.Context, texts []string, taskType string) ([][]float32, int, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.calculateBackoff(attempt)
			c.log.Debug("retrying embedding request",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
			)
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(delay):
			}
		}

		embeddings, tokens, err := c.embedBatch(ctx, texts, taskType)
		if err == nil {
			return embeddings, tokens, nil
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}

		lastErr = err
		c.log.Warn("embedding request failed",
			slog.Int("attempt", attempt),
			slog.String("error", err.Error()),
		)
	}

	return nil, 0, fmt.Errorf("all retries exhausted: %w", lastErr)
}

func (c *Client) embedBatch(ctx context.Context, texts []string, taskType string) ([][]float32, int, error) {
	embeddings := make([][]float32, 0, len(texts))
	outputDim := int32(DefaultDimension)
	totalTokens := 0

	for _, text := range texts {
		result, err := c.client.Models.EmbedContent(
			ctx,
			c.model,
			genai.Text(text),
			&genai.EmbedContentConfig{
				TaskType:             taskType,
				OutputDimensionality: &outputDim,
			},
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to embed text: %w", err)
		}

		if len(result.Embeddings) == 0 {
			return nil, 0, fmt.Errorf("no embeddings returned for text")
		}

		emb := result.Embeddings[0]
		embeddings = append(embeddings, emb.Values)
		totalTokens += embeddingPromptTokens(emb)
	}

	return embeddings, totalTokens, nil
}

// embeddingPromptTokens extracts the prompt-token count reported for a single
// embedding result. The genai SDK exposes the per-input token count on
// ContentEmbedding.Statistics (ContentEmbeddingStatistics.TokenCount). It
// returns 0 when statistics are absent, so callers can still embed without
// usage data.
func embeddingPromptTokens(emb *genai.ContentEmbedding) int {
	if emb == nil || emb.Statistics == nil {
		return 0
	}
	return int(emb.Statistics.TokenCount)
}

// calculateBackoff calculates the backoff delay for a given attempt
func (c *Client) calculateBackoff(attempt int) time.Duration {
	delay := float64(c.baseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(c.maxDelay) {
		delay = float64(c.maxDelay)
	}
	return time.Duration(delay)
}

// Close closes the client
func (c *Client) Close() error {
	// The genai client doesn't have a Close method
	return nil
}
