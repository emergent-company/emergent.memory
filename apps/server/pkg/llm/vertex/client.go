// Package vertex provides a Google Vertex AI chat completion client with streaming support.
package vertex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

const (
	// DefaultModel is the default chat model
	DefaultModel = "gemini-3-flash-preview"

	// DefaultMaxRetries is the default number of retries
	DefaultMaxRetries = 3

	// DefaultBaseDelay is the base delay for exponential backoff
	DefaultBaseDelay = 100 * time.Millisecond

	// DefaultMaxDelay is the maximum delay for exponential backoff
	DefaultMaxDelay = 10 * time.Second

	// DefaultTimeout is the default HTTP timeout for streaming requests
	DefaultTimeout = 120 * time.Second

	// DefaultTemperature is the default temperature for generation
	DefaultTemperature = 0.0

	// DefaultMaxOutputTokens is the default max output tokens (65536 for thinking models)
	DefaultMaxOutputTokens = 65536
)

// Config holds the configuration for the Vertex AI chat client
type Config struct {
	ProjectID       string
	Location        string
	Model           string
	Timeout         time.Duration
	Temperature     float64
	MaxOutputTokens int
}

// Client is a Vertex AI chat completion client with streaming support
type Client struct {
	projectID       string
	location        string
	model           string
	httpClient      *http.Client
	tokenSrc        *google.Credentials
	log             *slog.Logger
	temperature     float64
	maxOutputTokens int

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

// NewClient creates a new Vertex AI chat completion client
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
	if cfg.Temperature == 0 {
		cfg.Temperature = DefaultTemperature
	}
	if cfg.MaxOutputTokens == 0 {
		cfg.MaxOutputTokens = DefaultMaxOutputTokens
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
		tokenSrc:        creds,
		log:             slog.Default(),
		temperature:     cfg.Temperature,
		maxOutputTokens: cfg.MaxOutputTokens,
		maxRetries:      DefaultMaxRetries,
		baseDelay:       DefaultBaseDelay,
		maxDelay:        DefaultMaxDelay,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// GenerateRequest is the request for content generation
type GenerateRequest struct {
	// Prompt is the user prompt (required)
	Prompt string

	// SystemPrompt is an optional system prompt
	SystemPrompt string

	// Temperature overrides the default temperature (0.0-1.0)
	Temperature *float64

	// MaxOutputTokens overrides the default max tokens
	MaxOutputTokens *int
}

// streamGenerateRequest is the API request body for streamGenerateContent
type streamGenerateRequest struct {
	Contents          []content        `json:"contents"`
	SystemInstruction *content         `json:"systemInstruction,omitempty"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}

// streamGenerateResponse is the streaming API response
type streamGenerateResponse struct {
	Candidates    []candidate    `json:"candidates"`
	UsageMetadata *usageMetadata `json:"usageMetadata,omitempty"`
}

type candidate struct {
	Content       candidateContent `json:"content"`
	FinishReason  string           `json:"finishReason,omitempty"`
	SafetyRatings []safetyRating   `json:"safetyRatings,omitempty"`
}

type candidateContent struct {
	Parts []candidatePart `json:"parts"`
	Role  string          `json:"role"`
}

type candidatePart struct {
	Text string `json:"text"`
}

type safetyRating struct {
	Category    string `json:"category"`
	Probability string `json:"probability"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// Usage contains token usage information
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// GenerateResult contains the generation result
type GenerateResult struct {
	Content string
	Usage   *Usage
}

// Generate generates content (non-streaming)
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error) {
	var fullContent strings.Builder
	var usage *Usage

	err := c.GenerateStreaming(ctx, req, func(token string) {
		fullContent.WriteString(token)
	})
	if err != nil {
		return nil, err
	}

	return &GenerateResult{
		Content: fullContent.String(),
		Usage:   usage,
	}, nil
}

// GenerateStreaming generates content with streaming, calling onToken for each token
func (c *Client) GenerateStreaming(ctx context.Context, req GenerateRequest, onToken func(string)) error {
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
		c.location, c.projectID, c.location, c.model,
	)

	// Build request body
	temperature := c.temperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}
	maxTokens := c.maxOutputTokens
	if req.MaxOutputTokens != nil {
		maxTokens = *req.MaxOutputTokens
	}

	apiReq := streamGenerateRequest{
		Contents: []content{
			{
				Role:  "user",
				Parts: []part{{Text: req.Prompt}},
			},
		},
		GenerationConfig: generationConfig{
			Temperature:     temperature,
			MaxOutputTokens: maxTokens,
		},
	}

	// Add system instruction if provided
	if req.SystemPrompt != "" {
		apiReq.SystemInstruction = &content{
			Parts: []part{{Text: req.SystemPrompt}},
		}
	}

	reqBytes, err := json.Marshal(apiReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Execute with retries
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.calculateBackoff(attempt)
			c.log.Debug("retrying chat request",
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay),
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		lastErr = c.doStreamRequest(ctx, url, reqBytes, onToken)
		if lastErr == nil {
			return nil
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Check if error is retryable
		if _, ok := lastErr.(*retryableError); !ok {
			// Non-retryable error, return immediately
			return lastErr
		}

		c.log.Warn("chat request failed",
			slog.Int("attempt", attempt),
			slog.String("error", lastErr.Error()),
		)
	}

	return fmt.Errorf("all retries exhausted: %w", lastErr)
}

// doStreamRequest executes a single streaming HTTP request
func (c *Client) doStreamRequest(ctx context.Context, url string, body []byte, onToken func(string)) error {
	// Get access token
	token, err := c.tokenSrc.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-streaming error responses
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)

		// Check if retryable
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode >= 500 {
			return &retryableError{
				statusCode: resp.StatusCode,
				body:       string(respBody),
			}
		}
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Read SSE stream
	return c.parseSSEStream(resp.Body, onToken)
}

// parseSSEStream parses the SSE stream from Vertex AI
func (c *Client) parseSSEStream(reader io.Reader, onToken func(string)) error {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for potentially large responses
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Skip [DONE] marker if present
			if data == "[DONE]" {
				continue
			}

			var streamResp streamGenerateResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				c.log.Warn("failed to parse streaming response",
					slog.String("data", data),
					slog.String("error", err.Error()),
				)
				continue
			}

			// Extract text from candidates
			for _, candidate := range streamResp.Candidates {
				// Check for safety blocks
				if candidate.FinishReason == "SAFETY" {
					return fmt.Errorf("response blocked due to safety filters")
				}
				if candidate.FinishReason == "RECITATION" {
					return fmt.Errorf("response blocked due to recitation/copyright detection")
				}

				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						onToken(part.Text)
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	return nil
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

// IsAvailable checks if the client is properly configured
func (c *Client) IsAvailable() bool {
	return c.projectID != "" && c.location != "" && c.tokenSrc != nil
}

// IsConfigured implements llm.Provider
func (c *Client) IsConfigured() bool {
	return c.IsAvailable()
}

// Complete implements llm.Provider - generates a completion for the given prompt
func (c *Client) Complete(ctx context.Context, prompt string) (string, error) {
	result, err := c.Generate(ctx, GenerateRequest{Prompt: prompt})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// Model returns the configured model name
func (c *Client) Model() string {
	return c.model
}
