package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// Query Knowledge Tool Definition
// ============================================================================

func queryToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name: "search-knowledge",
			Description: "Ask a question against the project's knowledge graph. The system finds relevant entities and relationships, " +
				"then generates a grounded answer using the connected LLM provider. " +
				"Returns the assembled answer text and a truncated flag if the response was cut short.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"question": {
						Type:        "string",
						Description: "The question to ask against the knowledge base",
					},
					"mode": {
						Type:        "string",
						Description: "Query mode (default: 'graph'). Use 'semantic' for embedding-based search, 'hybrid' for combined.",
						Enum:        []string{"graph", "semantic", "hybrid"},
					},
				},
				Required: []string{"question"},
			},
		},
	}
}

// ============================================================================
// Query Knowledge Tool Handler
// ============================================================================

const queryKnowledgeTimeout = 60 * time.Second

func (s *Service) executeQueryKnowledge(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	question, _ := args["question"].(string)
	if question == "" {
		return nil, fmt.Errorf("query_knowledge: 'question' is required")
	}

	// Build the request body
	body := map[string]any{"message": question}
	if mode, ok := args["mode"].(string); ok && mode != "" {
		body["mode"] = mode
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("query_knowledge: marshal request: %w", err)
	}

	// The query endpoint is on the same server — use the server's own listen address.
	url := fmt.Sprintf("http://localhost:%d/api/projects/%s/query", s.serverPort, projectID)

	// Apply 60-second timeout
	queryCtx, cancel := context.WithTimeout(ctx, queryKnowledgeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(queryCtx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("query_knowledge: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Forward auth headers from the incoming context if available
	if token := tokenFromContext(ctx); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if projectID != "" {
		req.Header.Set("X-Project-ID", projectID)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if queryCtx.Err() != nil {
			return s.wrapResult(map[string]any{
				"answer":    "",
				"truncated": true,
				"error":     "query timed out after 60s",
			})
		}
		return nil, fmt.Errorf("query_knowledge: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("query_knowledge: server returned %d", resp.StatusCode)
	}

	// Collect SSE data lines
	var parts []string
	truncated := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			// Try to decode as JSON with a "content" or "text" field
			var chunk map[string]any
			if json.Unmarshal([]byte(payload), &chunk) == nil {
				if text, ok := chunk["content"].(string); ok {
					parts = append(parts, text)
					continue
				}
				if text, ok := chunk["text"].(string); ok {
					parts = append(parts, text)
					continue
				}
				if text, ok := chunk["delta"].(string); ok {
					parts = append(parts, text)
					continue
				}
			}
			// Fall back to raw payload
			parts = append(parts, payload)
		}

		// Check for context timeout on each line
		if queryCtx.Err() != nil {
			truncated = true
			break
		}
	}

	if queryCtx.Err() != nil && !truncated {
		truncated = true
	}

	answer := strings.Join(parts, "")
	return s.wrapResult(map[string]any{
		"answer":    answer,
		"truncated": truncated,
	})
}

// tokenFromContext tries to extract a bearer token from context (e.g. set by the MCP auth middleware).
// Returns empty string if none is found.
func tokenFromContext(ctx context.Context) string {
	type ctxKey string
	if v := ctx.Value(ctxKey("bearer_token")); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
