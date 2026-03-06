package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	braveSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"
	braveDefaultCount   = 10
	braveMaxCount       = 20
)

// braveSearchRequest holds the parameters for a Brave Search API call
type braveSearchRequest struct {
	Query     string
	Count     int
	Offset    int
	Freshness string // pd (past day), pw (past week), pm (past month), py (past year)
}

// braveSearchAPIResponse represents the top-level Brave Search API response
type braveSearchAPIResponse struct {
	Query braveQueryInfo   `json:"query"`
	Web   *braveWebResults `json:"web,omitempty"`
	Mixed *json.RawMessage `json:"mixed,omitempty"`
}

// braveQueryInfo contains metadata about the query
type braveQueryInfo struct {
	Original string `json:"original"`
}

// braveWebResults wraps the web search results
type braveWebResults struct {
	Results []braveWebResult `json:"results"`
}

// braveWebResult represents a single web search result
type braveWebResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	ExtraSnippets []string `json:"extra_snippets,omitempty"`
	Age           string   `json:"age,omitempty"`
}

// braveSearchToolResult is the formatted result returned by the MCP tool
type braveSearchToolResult struct {
	Query       string                   `json:"query"`
	ResultCount int                      `json:"resultCount"`
	Results     []braveSearchResultEntry `json:"results"`
}

// braveSearchResultEntry is a single result in the tool output
type braveSearchResultEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Age         string `json:"age,omitempty"`
}

// executeBraveWebSearch executes a Brave Search API web search
func (s *Service) executeBraveWebSearch(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if s.braveSearchAPIKey == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Brave Search is not configured. Set the BRAVE_SEARCH_API_KEY environment variable."}},
			IsError: true,
		}, nil
	}

	// Parse arguments
	query, _ := args["query"].(string)
	if query == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required argument: query"}},
			IsError: true,
		}, nil
	}

	count := braveDefaultCount
	if c, ok := args["count"].(float64); ok {
		count = int(c)
	}
	if count < 1 {
		count = 1
	}
	if count > braveMaxCount {
		count = braveMaxCount
	}

	offset := 0
	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}
	if offset < 0 {
		offset = 0
	}

	freshness, _ := args["freshness"].(string)

	req := braveSearchRequest{
		Query:     query,
		Count:     count,
		Offset:    offset,
		Freshness: freshness,
	}

	// Execute the search
	results, err := s.callBraveSearchAPI(ctx, req)
	if err != nil {
		s.log.Error("brave search API call failed", "error", err, "query", query)
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Brave Search API error: %s", err.Error())}},
			IsError: true,
		}, nil
	}

	return s.wrapResult(results)
}

// callBraveSearchAPI makes the HTTP request to the Brave Search API
func (s *Service) callBraveSearchAPI(ctx context.Context, req braveSearchRequest) (*braveSearchToolResult, error) {
	// Build URL with query parameters
	params := url.Values{}
	params.Set("q", req.Query)
	params.Set("count", fmt.Sprintf("%d", req.Count))
	if req.Offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", req.Offset))
	}
	if req.Freshness != "" {
		// Validate freshness value
		validFreshness := map[string]bool{"pd": true, "pw": true, "pm": true, "py": true}
		if validFreshness[req.Freshness] {
			params.Set("freshness", req.Freshness)
		}
	}

	reqURL := braveSearchEndpoint + "?" + params.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Encoding", "gzip")
	httpReq.Header.Set("X-Subscription-Token", s.braveSearchAPIKey)

	client := &http.Client{
		Timeout: s.braveSearchTimeout,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Read and decode response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp braveSearchAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Transform API response into tool result
	result := &braveSearchToolResult{
		Query:       req.Query,
		ResultCount: 0,
		Results:     []braveSearchResultEntry{},
	}

	if apiResp.Web != nil {
		for _, r := range apiResp.Web.Results {
			entry := braveSearchResultEntry{
				Title:       r.Title,
				URL:         r.URL,
				Description: r.Description,
				Age:         r.Age,
			}
			// Append extra snippets to description if available
			if len(r.ExtraSnippets) > 0 {
				entry.Description += "\n\n" + strings.Join(r.ExtraSnippets, "\n")
			}
			result.Results = append(result.Results, entry)
		}
		result.ResultCount = len(result.Results)
	}

	return result, nil
}

// getBraveSearchToolDefinition returns the MCP tool definition for Brave web search
func getBraveSearchToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "brave_web_search",
		Description: "Search the web using the Brave Search API. Returns web search results with titles, URLs, descriptions, and snippets. Use this to find current information, research topics, or verify facts from the web.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"query": {
					Type:        "string",
					Description: "The search query string",
				},
				"count": {
					Type:        "number",
					Description: "Number of results to return (default: 10, max: 20)",
					Minimum:     intPtr(1),
					Maximum:     intPtr(20),
					Default:     10,
				},
				"offset": {
					Type:        "number",
					Description: "Pagination offset for results (default: 0)",
					Minimum:     intPtr(0),
					Default:     0,
				},
				"freshness": {
					Type:        "string",
					Description: "Filter results by freshness: pd (past day), pw (past week), pm (past month), py (past year)",
					Enum:        []string{"pd", "pw", "pm", "py"},
				},
			},
			Required: []string{"query"},
		},
	}
}

// braveSearchDefaultTimeout is used when no config timeout is set
var braveSearchDefaultTimeout = 15 * time.Second
