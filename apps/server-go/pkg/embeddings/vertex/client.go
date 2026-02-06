// Package vertex provides a Google Vertex AI embeddings client.
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	// DefaultModel is the default embedding model
	DefaultModel = "text-embedding-004"

	// DefaultDimension is the embedding dimension for text-embedding-004
	DefaultDimension = 768

	// DefaultMaxRetries is the default number of retries
	DefaultMaxRetries = 3

	// DefaultBaseDelay is the base delay for exponential backoff
	DefaultBaseDelay = 100 * time.Millisecond

	// DefaultMaxDelay is the maximum delay for exponential backoff
	DefaultMaxDelay = 10 * time.Second

	// DefaultTimeout is the default HTTP timeout
	DefaultTimeout = 30 * time.Second

	// DefaultBatchSize is the maximum batch size per request
	DefaultBatchSize = 100
)

// Config holds the configuration for the Vertex AI client
type Config struct {
	ProjectID string
	Location  string
	Model     string
	Timeout   time.Duration
}

// Client is a Vertex AI embeddings client
type Client struct {
	projectID  string
	location   string
	model      string
	httpClient *http.Client
	tokenSrc   *google.Credentials
	log        *slog.Logger

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

// NewClient creates a new Vertex AI embeddings client
func NewClient(ctx context.Context, cfg Config, opts ...ClientOption) (*Client, error) {
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("project ID is required")
	}
	if cfg.Location == "" {
		return nil, fmt.Errorf("location is required")
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}

	// Get default credentials
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to find default credentials: %w", err)
	}

	c := &Client{
		projectID: cfg.ProjectID,
		location:  cfg.Location,
		model:     cfg.Model,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		tokenSrc:   creds,
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

// predictRequest is the request body for the predict API
type predictRequest struct {
	Instances []instance `json:"instances"`
}

type instance struct {
	Content  string `json:"content"`
	TaskType string `json:"task_type"`
}

// predictResponse is the response from the predict API
type predictResponse struct {
	Predictions []prediction `json:"predictions"`
}

type prediction struct {
	Embeddings embeddingResult `json:"embeddings"`
}

type embeddingResult struct {
	Values     []float32      `json:"values"`
	Statistics embeddingStats `json:"statistics"`
}

type embeddingStats struct {
	TokenCount int `json:"token_count"`
}

// EmbedResult contains the embedding result with usage data
type EmbedResult struct {
	Embedding []float32
	Usage     *Usage
}

// BatchEmbedResult contains batch embedding results with usage data
type BatchEmbedResult struct {
	Embeddings [][]float32
	Usage      *Usage
}

// Usage contains token usage information
type Usage struct {
	PromptTokens int
	TotalTokens  int
}

// EmbedQuery generates an embedding for a single query
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	result, err := c.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

// EmbedQueryWithUsage generates an embedding for a single query with usage data
func (c *Client) EmbedQueryWithUsage(ctx context.Context, query string) (*EmbedResult, error) {
	result, err := c.EmbedDocumentsWithUsage(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return &EmbedResult{
		Embedding: result.Embeddings[0],
		Usage:     result.Usage,
	}, nil
}

// EmbedDocuments generates embeddings for multiple documents
func (c *Client) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	result, err := c.EmbedDocumentsWithUsage(ctx, documents)
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

// EmbedDocumentsWithUsage generates embeddings for multiple documents with usage data
func (c *Client) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*BatchEmbedResult, error) {
	if len(documents) == 0 {
		return &BatchEmbedResult{
			Embeddings: [][]float32{},
			Usage:      &Usage{},
		}, nil
	}

	// Process in batches
	var allEmbeddings [][]float32
	var totalTokens int

	for i := 0; i < len(documents); i += DefaultBatchSize {
		end := i + DefaultBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		batch := documents[i:end]

		embs, tokens, err := c.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("failed to embed batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embs...)
		totalTokens += tokens
	}

	return &BatchEmbedResult{
		Embeddings: allEmbeddings,
		Usage: &Usage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

// embedBatch embeds a single batch of documents
func (c *Client) embedBatch(ctx context.Context, documents []string) ([][]float32, int, error) {
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		c.location, c.projectID, c.location, c.model,
	)

	// Build request
	instances := make([]instance, len(documents))
	for i, doc := range documents {
		instances[i] = instance{
			Content:  doc,
			TaskType: "RETRIEVAL_DOCUMENT",
		}
	}

	reqBody := predictRequest{Instances: instances}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Execute with retries
	var resp *predictResponse
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

		resp, lastErr = c.doRequest(ctx, url, reqBytes)
		if lastErr == nil {
			break
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}

		c.log.Warn("embedding request failed",
			slog.Int("attempt", attempt),
			slog.String("error", lastErr.Error()),
		)
	}

	if lastErr != nil {
		return nil, 0, fmt.Errorf("all retries exhausted: %w", lastErr)
	}

	// Extract embeddings and token counts
	embeddings := make([][]float32, len(resp.Predictions))
	totalTokens := 0

	for i, pred := range resp.Predictions {
		embeddings[i] = pred.Embeddings.Values
		totalTokens += pred.Embeddings.Statistics.TokenCount
	}

	return embeddings, totalTokens, nil
}

// doRequest executes a single HTTP request
func (c *Client) doRequest(ctx context.Context, url string, body []byte) (*predictResponse, error) {
	// Get access token
	token, err := c.tokenSrc.TokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Check if retryable
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode >= 500 {
			return nil, &retryableError{
				statusCode: resp.StatusCode,
				body:       string(respBody),
			}
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result predictResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

// calculateBackoff calculates the backoff delay for a given attempt
func (c *Client) calculateBackoff(attempt int) time.Duration {
	delay := float64(c.baseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(c.maxDelay) {
		delay = float64(c.maxDelay)
	}
	return time.Duration(delay)
}

// retryableError is an error that can be retried
type retryableError struct {
	statusCode int
	body       string
}

func (e *retryableError) Error() string {
	return fmt.Sprintf("retryable API error %d: %s", e.statusCode, e.body)
}
