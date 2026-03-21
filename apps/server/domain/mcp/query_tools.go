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

	"github.com/emergent-company/emergent.memory/pkg/auth"
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
				"Returns the assembled answer text, a truncated flag if the response was cut short, and a session_id to continue the conversation. " +
				"Pass session_id from a prior call to continue a previous conversation.",
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
					"session_id": {
						Type:        "string",
						Description: "Optional session ID to continue a previous query conversation. Use the session_id returned from a prior search-knowledge call.",
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
	if sessionID, ok := args["session_id"].(string); ok && sessionID != "" {
		body["conversation_id"] = sessionID
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

	// Collect SSE token events and capture the session ID from the meta event.
	var parts []string
	var returnedSessionID string
	truncated := false
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		switch chunk["type"] {
		case "token":
			if token, ok := chunk["token"].(string); ok {
				parts = append(parts, token)
			}
		case "meta":
			if id, ok := chunk["conversationId"].(string); ok && id != "" {
				returnedSessionID = id
			}
		case "error":
			if errMsg, ok := chunk["error"].(string); ok && errMsg != "" {
				return nil, fmt.Errorf("query_knowledge: agent error: %s", errMsg)
			}
		}
		if queryCtx.Err() != nil {
			truncated = true
			break
		}
	}

	if queryCtx.Err() != nil && !truncated {
		truncated = true
	}

	answer := strings.Join(parts, "")
	result := map[string]any{
		"answer":    answer,
		"truncated": truncated,
	}
	if returnedSessionID != "" {
		result["session_id"] = returnedSessionID
	}
	return s.wrapResult(result)
}

// tokenFromContext extracts the raw bearer/API token stored by the auth middleware.
// Returns empty string if none is found.
func tokenFromContext(ctx context.Context) string {
	return auth.RawTokenFromContext(ctx)
}
