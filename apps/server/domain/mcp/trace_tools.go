package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// Trace Tool Definitions
// ============================================================================

func traceToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "trace-list",
			Description: "List recent traces from Tempo. Returns trace IDs, root span names, durations, and timestamps. Returns an empty list when tracing is not configured.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of traces to return (default 20, max 100)",
					},
					"service_name": {
						Type:        "string",
						Description: "Filter traces by service name",
					},
					"tags": {
						Type:        "string",
						Description: "Filter by tags in key=value format, comma-separated (e.g. 'http.status_code=200,service.name=memory-server')",
					},
					"min_duration": {
						Type:        "string",
						Description: "Minimum trace duration filter (e.g. '100ms', '1s')",
					},
					"start": {
						Type:        "string",
						Description: "Start time for the search window (RFC3339, default 1 hour ago)",
					},
					"end": {
						Type:        "string",
						Description: "End time for the search window (RFC3339, default now)",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "trace-get",
			Description: "Get the full span tree for a specific trace by ID. Returns all spans with their operation names, durations, tags, and parent/child relationships.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"trace_id": {
						Type:        "string",
						Description: "The trace ID to retrieve",
					},
				},
				Required: []string{"trace_id"},
			},
		},
	}
}

// ============================================================================
// Trace Tool Handlers
// ============================================================================

func (s *Service) executeListTraces(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if s.tempoBaseURL == "" {
		return s.wrapResult(map[string]any{
			"traces":  []any{},
			"message": "tracing not enabled — set OTEL_EXPORTER_OTLP_ENDPOINT to enable",
		})
	}

	// Build query parameters
	params := make([]string, 0)

	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 100 {
			limit = 100
		}
	}
	params = append(params, fmt.Sprintf("limit=%d", limit))

	if v, ok := args["service_name"].(string); ok && v != "" {
		params = append(params, fmt.Sprintf("tags=service.name%%3D%s", v))
	} else if v, ok := args["tags"].(string); ok && v != "" {
		params = append(params, fmt.Sprintf("tags=%s", v))
	}

	if v, ok := args["min_duration"].(string); ok && v != "" {
		params = append(params, fmt.Sprintf("minDuration=%s", v))
	}

	if v, ok := args["start"].(string); ok && v != "" {
		params = append(params, fmt.Sprintf("start=%s", v))
	}
	if v, ok := args["end"].(string); ok && v != "" {
		params = append(params, fmt.Sprintf("end=%s", v))
	}

	url := s.tempoBaseURL + "/api/search"
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	body, err := s.tempoGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("list_traces: %w", err)
	}
	defer body.Close()

	var result any
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list_traces: decode response: %w", err)
	}
	return s.wrapResult(result)
}

func (s *Service) executeGetTrace(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if s.tempoBaseURL == "" {
		return nil, fmt.Errorf("get_trace: tracing not enabled — set OTEL_EXPORTER_OTLP_ENDPOINT to enable")
	}

	traceID, _ := args["trace_id"].(string)
	if traceID == "" {
		return nil, fmt.Errorf("get_trace: 'trace_id' is required")
	}

	url := s.tempoBaseURL + "/api/traces/" + traceID
	body, err := s.tempoGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get_trace: %w", err)
	}
	defer body.Close()

	var result any
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get_trace: decode response: %w", err)
	}
	return s.wrapResult(result)
}

// tempoGet makes a GET request to the Tempo query API and returns the response body.
// The caller is responsible for closing the body.
func (s *Service) tempoGet(ctx context.Context, url string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tempo unreachable: %w", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway {
		resp.Body.Close()
		return nil, fmt.Errorf("tempo returned %d — tracing backend may be down", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("tempo returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	return resp.Body, nil
}

// tempoGetSSE is a variant of tempoGet that reads SSE lines (used for streaming queries).
// Returns lines as a slice of data payloads.
func (s *Service) tempoGetSSE(ctx context.Context, url string) ([]string, bool, error) {
	body, err := s.tempoGet(ctx, url)
	if err != nil {
		return nil, false, err
	}
	defer body.Close()

	var lines []string
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return lines, false, scanner.Err()
}
