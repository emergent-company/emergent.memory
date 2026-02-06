// Package client provides an HTTP client for API testing with metrics collection.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ServerType represents the type of server being tested.
type ServerType string

const (
	ServerGo     ServerType = "go"
	ServerNestJS ServerType = "nestjs"
)

// Client is an HTTP client for API testing.
type Client struct {
	baseURL    string
	serverType ServerType
	httpClient *http.Client
	metrics    *MetricsCollector
	mu         sync.Mutex
}

// Config holds client configuration.
type Config struct {
	BaseURL    string
	ServerType ServerType
	Timeout    time.Duration
}

// New creates a new API test client.
func New(cfg Config) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.ServerType == "" {
		cfg.ServerType = ServerGo
	}

	return &Client{
		baseURL:    strings.TrimSuffix(cfg.BaseURL, "/"),
		serverType: cfg.ServerType,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		metrics: NewMetricsCollector(),
	}
}

// Response wraps an HTTP response with convenience methods.
type Response struct {
	*http.Response
	body     []byte
	bodyRead bool
	duration time.Duration
}

// Body returns the response body as bytes.
func (r *Response) Body() []byte {
	if !r.bodyRead {
		r.body, _ = io.ReadAll(r.Response.Body)
		r.Response.Body.Close()
		r.bodyRead = true
	}
	return r.body
}

// BodyString returns the response body as a string.
func (r *Response) BodyString() string {
	return string(r.Body())
}

// JSON unmarshals the response body into the provided interface.
func (r *Response) JSON(v any) error {
	return json.Unmarshal(r.Body(), v)
}

// JSONMap returns the response body as a map.
func (r *Response) JSONMap() (map[string]any, error) {
	var m map[string]any
	err := r.JSON(&m)
	return m, err
}

// Duration returns how long the request took.
func (r *Response) Duration() time.Duration {
	return r.duration
}

// IsSuccess returns true if status code is 2xx.
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// GET performs a GET request.
func (c *Client) GET(path string, opts ...Option) (*Response, error) {
	return c.do(http.MethodGet, path, nil, opts...)
}

// POST performs a POST request.
func (c *Client) POST(path string, body any, opts ...Option) (*Response, error) {
	return c.do(http.MethodPost, path, body, opts...)
}

// PUT performs a PUT request.
func (c *Client) PUT(path string, body any, opts ...Option) (*Response, error) {
	return c.do(http.MethodPut, path, body, opts...)
}

// PATCH performs a PATCH request.
func (c *Client) PATCH(path string, body any, opts ...Option) (*Response, error) {
	return c.do(http.MethodPatch, path, body, opts...)
}

// DELETE performs a DELETE request.
func (c *Client) DELETE(path string, opts ...Option) (*Response, error) {
	return c.do(http.MethodDelete, path, nil, opts...)
}

// DELETEWithBody performs a DELETE request with a JSON body (for bulk operations).
func (c *Client) DELETEWithBody(path string, body any, opts ...Option) (*Response, error) {
	return c.do(http.MethodDelete, path, body, opts...)
}

// do performs the actual HTTP request.
func (c *Client) do(method, path string, body any, opts ...Option) (*Response, error) {
	// Apply options
	reqOpts := &requestOptions{
		headers: make(map[string]string),
		query:   make(map[string]string),
	}
	for _, opt := range opts {
		opt(reqOpts)
	}

	// Build URL
	url := c.baseURL + path
	if len(reqOpts.query) > 0 {
		params := make([]string, 0, len(reqOpts.query))
		for k, v := range reqOpts.query {
			params = append(params, fmt.Sprintf("%s=%s", k, v))
		}
		url += "?" + strings.Join(params, "&")
	}

	// Build body
	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create request
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Apply custom headers
	for k, v := range reqOpts.headers {
		req.Header.Set(k, v)
	}

	// Execute request with timing
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Record metrics
	c.metrics.Record(RequestMetric{
		Method:     method,
		Path:       path,
		StatusCode: resp.StatusCode,
		Duration:   duration,
		Timestamp:  start,
	})

	return &Response{
		Response: resp,
		duration: duration,
	}, nil
}

// Metrics returns the metrics collector.
func (c *Client) Metrics() *MetricsCollector {
	return c.metrics
}

// ResetMetrics clears all collected metrics.
func (c *Client) ResetMetrics() {
	c.metrics.Reset()
}

// ServerType returns the configured server type.
func (c *Client) ServerType() ServerType {
	return c.serverType
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}
