// Package openai provides an OpenAI-compatible embeddings client.
// It works with OpenAI directly and with any OpenAI-compatible proxy
// such as LiteLLM, which can serve Google Gemini embedding models.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/embeddings/vertex"
)

const (
	// DefaultBaseURL is the OpenAI API base URL.
	DefaultBaseURL = "https://api.openai.com/v1"

	// DefaultModel is the default OpenAI embedding model.
	DefaultModel = "text-embedding-3-small"

	// DefaultBatchSize is the maximum number of texts per request.
	DefaultBatchSize = 100

	// DefaultTimeout is the HTTP client timeout.
	DefaultTimeout = 60 * time.Second
)

// Config holds client configuration.
type Config struct {
	APIKey     string
	BaseURL    string // defaults to DefaultBaseURL
	Model      string // the model name passed to the API
	Dimensions int    // output dimension; 0 = provider default
	// Auth is the credential-injection style used for /embeddings requests.
	// Empty defaults to bearer. Azure OpenAI declares "api-key"; other
	// OpenAI-compatible vendors use bearer. Mirrors pkg/adk's ApplyAuth.
	Auth adk.AuthStyle
}

// Client is an OpenAI-compatible embeddings client.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	dimensions int // 0 = provider default
	authStyle  adk.AuthStyle
	httpClient *http.Client
	log        *slog.Logger
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithLogger sets the logger.
func WithLogger(log *slog.Logger) ClientOption {
	return func(c *Client) { c.log = log }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.httpClient.Timeout = d }
}

// NewClient creates a new OpenAI-compatible embeddings client.
func NewClient(cfg Config, opts ...ClientOption) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai embeddings: APIKey is required")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	c := &Client{
		apiKey:     cfg.APIKey,
		baseURL:    baseURL,
		model:      model,
		dimensions: cfg.Dimensions,
		authStyle:  cfg.Auth,
		httpClient: &http.Client{Timeout: DefaultTimeout},
		log:        slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// EmbedQuery generates an embedding for a single query string.
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	result, err := c.EmbedQueryWithUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Embedding) == 0 {
		return nil, fmt.Errorf("openai embeddings: no embedding returned")
	}
	return result.Embedding, nil
}

// EmbedDocuments generates embeddings for multiple documents.
func (c *Client) EmbedDocuments(ctx context.Context, documents []string) ([][]float32, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	var all [][]float32
	for i := 0; i < len(documents); i += DefaultBatchSize {
		end := i + DefaultBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		batch, _, err := c.embed(ctx, documents[i:end])
		if err != nil {
			return nil, fmt.Errorf("openai embeddings batch %d-%d: %w", i, end, err)
		}
		all = append(all, batch...)
	}
	return all, nil
}

// EmbedQueryWithUsage generates an embedding for a single query and reports
// the token usage returned by the API. Provider is "openai", which covers
// OpenAI directly and OpenAI-compatible proxies such as LiteLLM.
func (c *Client) EmbedQueryWithUsage(ctx context.Context, query string) (*vertex.EmbedResult, error) {
	embeddings, tokens, err := c.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("openai embeddings: no embedding returned")
	}
	return &vertex.EmbedResult{
		Embedding: embeddings[0],
		Usage:     &vertex.Usage{PromptTokens: tokens, TotalTokens: tokens},
		Model:     c.model,
		Provider:  "openai",
	}, nil
}

// EmbedDocumentsWithUsage generates embeddings for multiple documents and
// reports the summed token usage returned by the API. Provider is "openai".
func (c *Client) EmbedDocumentsWithUsage(ctx context.Context, documents []string) (*vertex.BatchEmbedResult, error) {
	if len(documents) == 0 {
		return &vertex.BatchEmbedResult{
			Embeddings: nil,
			Usage:      nil,
			Model:      c.model,
			Provider:   "openai",
		}, nil
	}

	var all [][]float32
	totalTokens := 0
	for i := 0; i < len(documents); i += DefaultBatchSize {
		end := i + DefaultBatchSize
		if end > len(documents) {
			end = len(documents)
		}
		batch, tokens, err := c.embed(ctx, documents[i:end])
		if err != nil {
			return nil, fmt.Errorf("openai embeddings batch %d-%d: %w", i, end, err)
		}
		all = append(all, batch...)
		totalTokens += tokens
	}

	return &vertex.BatchEmbedResult{
		Embeddings: all,
		Usage:      &vertex.Usage{PromptTokens: totalTokens, TotalTokens: totalTokens},
		Model:      c.model,
		Provider:   "openai",
	}, nil
}

type embedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// embedUsage mirrors the usage block OpenAI-compatible /embeddings responses
// return (OpenAI and LiteLLM both include prompt_tokens/total_tokens).
type embedUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage *embedUsage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// embed posts a single batch to /embeddings and returns the vectors plus the
// prompt-token count reported in the response usage block (0 when absent).
func (c *Client) embed(ctx context.Context, texts []string) ([][]float32, int, error) {
	payload, err := json.Marshal(embedRequest{Model: c.model, Input: texts, Dimensions: c.dimensions})
	if err != nil {
		return nil, 0, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Inject the credential using the vendor's declared auth style. Azure
	// OpenAI expects "api-key"; OpenAI-compatible vendors expect bearer.
	if err := adk.ApplyAuth(req, c.authStyle, c.apiKey); err != nil {
		return nil, 0, fmt.Errorf("auth: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read body: %w", err)
	}

	var result embedResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("unmarshal (status=%d body=%s): %w", resp.StatusCode, body, err)
	}
	if result.Error != nil {
		return nil, 0, fmt.Errorf("api error %d: %s", resp.StatusCode, result.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("api error %d: %s", resp.StatusCode, body)
	}

	// Re-order by index (API may return out of order).
	embeddings := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index < len(embeddings) {
			embeddings[d.Index] = d.Embedding
		}
	}
	for i, e := range embeddings {
		if e == nil {
			return nil, 0, fmt.Errorf("openai embeddings: missing embedding for index %d", i)
		}
	}

	tokens := 0
	if result.Usage != nil {
		tokens = result.Usage.PromptTokens
	}
	return embeddings, tokens, nil
}
