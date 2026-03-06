package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/search"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/client"
	"github.com/spf13/cobra"
)

var queryCmd = &cobra.Command{
	Use:   "query <question>",
	Short: "Query a project using natural language",
	Long: `Query a project using natural language.

By default, uses the graph-query-agent — an AI agent that reasons over the knowledge
graph using search, traversal, and entity tools. The agent is managed server-side;
no agent ID is needed.

Use --mode=search for direct hybrid search without AI reasoning.

Examples:
  emergent query "what are the main services and how do they relate?"
  emergent query --mode=search "auth service"
  emergent query --project abc123 "list all requirements"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runQuery,
}

var (
	queryProjectID      string
	queryLimit          int
	queryResultTypes    string
	queryFusionStrategy string
	queryJSON           bool
	queryDebug          bool
	queryMode           string
	queryShowTools      bool
	queryShowScores     bool
	queryShowTime       bool
)

func init() {
	rootCmd.AddCommand(queryCmd)

	queryCmd.Flags().StringVar(&queryProjectID, "project", "", "Project ID to query (uses default project if not specified)")
	queryCmd.Flags().StringVar(&queryMode, "mode", "agent", "Query mode: agent (default, AI reasoning) or search (direct hybrid search)")
	queryCmd.Flags().BoolVar(&queryShowTools, "show-tools", false, "Show tool calls made by the agent (agent mode only)")
	queryCmd.Flags().IntVar(&queryLimit, "limit", 10, "Maximum number of results to return (search mode only)")
	queryCmd.Flags().StringVar(&queryResultTypes, "result-types", "both", "Types of results: graph, text, or both (search mode only)")
	queryCmd.Flags().StringVar(&queryFusionStrategy, "fusion-strategy", "weighted", "Fusion strategy: weighted, rrf, interleave, graph_first, text_first (search mode only)")
	queryCmd.Flags().BoolVar(&queryJSON, "json", false, "Output results as JSON")
	queryCmd.Flags().BoolVar(&queryDebug, "debug", false, "Include debug information in output")
	queryCmd.Flags().BoolVar(&queryShowScores, "show-scores", false, "Show relevance scores for each result (search mode only)")
	queryCmd.Flags().BoolVar(&queryShowTime, "show-time", false, "Show elapsed query time")
}

func runQuery(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")

	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	projectID, err := resolveProjectContext(cmd, queryProjectID)
	if err != nil {
		return err
	}

	c.SetContext("", projectID)

	if queryMode == "agent" {
		return runAgentQuery(cmd.Context(), c, query, projectID)
	} else if queryMode == "search" {
		return runSearchQuery(cmd.Context(), c, query, projectID)
	}
	return fmt.Errorf("invalid mode %q (must be 'agent' or 'search')", queryMode)
}

// runAgentQuery posts to POST /api/projects/:projectId/query.
// The server manages the graph-query-agent entirely — no agent ID needed client-side.
func runAgentQuery(ctx context.Context, c *client.Client, query, projectID string) error {
	reqBody := map[string]interface{}{
		"message": query,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := c.BaseURL() + "/api/projects/" + url.PathEscape(projectID) + "/query"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if auth := c.AuthorizationHeader(); auth != "" {
		httpReq.Header.Set("Authorization", auth)
	}
	if projectID != "" {
		httpReq.Header.Set("X-Project-ID", projectID)
	}

	start := time.Now()
	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE stream
	var response strings.Builder
	var tools []string
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		switch eventType {
		case "token":
			if token, ok := event["token"].(string); ok {
				response.WriteString(token)
				if !queryJSON {
					fmt.Print(token)
				}
			}
		case "mcp_tool":
			if status, ok := event["status"].(string); ok && status == "started" {
				if tool, ok := event["tool"].(string); ok {
					tools = append(tools, tool)
					if queryShowTools {
						fmt.Printf("\n[Tool: %s]\n", tool)
					}
				}
			}
		}
	}

	elapsed := time.Since(start)

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if queryJSON {
		output := map[string]interface{}{
			"query":     query,
			"projectId": projectID,
			"response":  response.String(),
			"tools":     tools,
			"elapsedMs": elapsed.Milliseconds(),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	fmt.Printf("\n\n")
	if queryShowTools && len(tools) > 0 {
		fmt.Printf("Tools used: %s\n", strings.Join(tools, ", "))
	}
	if queryShowTime {
		fmt.Printf("Time: %v\n", elapsed.Round(time.Millisecond))
	}

	return nil
}

func runSearchQuery(ctx context.Context, c *client.Client, query, projectID string) error {
	start := time.Now()
	response, err := c.SDK.Search.Search(ctx, &search.SearchRequest{
		Query:          query,
		Limit:          queryLimit,
		ResultTypes:    queryResultTypes,
		FusionStrategy: queryFusionStrategy,
		IncludeDebug:   queryDebug,
	})
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if queryJSON {
		output := map[string]interface{}{
			"query":     query,
			"projectId": projectID,
			"total":     response.Total,
			"results":   response.Results,
			"elapsedMs": elapsed.Milliseconds(),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	fmt.Printf("Query: %s\n", query)
	if queryShowTime {
		fmt.Printf("Results: %d  Time: %v\n\n", response.Total, elapsed.Round(time.Millisecond))
	} else {
		fmt.Printf("Results: %d\n\n", response.Total)
	}

	if response.Total == 0 {
		fmt.Println("No results found.")
		fmt.Println("\nTips:")
		fmt.Println("  - Try broader search terms")
		fmt.Println("  - Check that the project has data indexed")
		fmt.Println("  - Use --debug flag to see more information")
		return nil
	}

	if queryShowScores {
		fmt.Printf("| # | type | details | id | score |\n")
		fmt.Printf("|---|------|---------|-----|-------|\n")
	} else {
		fmt.Printf("| # | type | details | id |\n")
		fmt.Printf("|---|------|---------|----|\n")
	}

	for i, result := range response.Results {
		num := fmt.Sprintf("%d", i+1)
		var resultType, details, id string

		switch result.Type {
		case "graph":
			resultType = result.ObjectType
			label := result.Key
			if label == "" {
				if v, ok := result.Fields["name"].(string); ok {
					label = v
				}
			}
			details = label
			var extras []string
			for key, value := range result.Fields {
				if key == "name" {
					continue
				}
				var vs string
				switch v := value.(type) {
				case string:
					vs = v
				case []interface{}:
					vs = fmt.Sprintf("[%d items]", len(v))
				case map[string]interface{}:
					vs = fmt.Sprintf("{%d fields}", len(v))
				default:
					vs = fmt.Sprintf("%v", v)
				}
				extras = append(extras, key+"="+vs)
			}
			if len(extras) > 0 {
				extra := strings.Join(extras, ", ")
				full := details + " (" + extra + ")"
				if len(full) > 80 {
					full = full[:77] + "..."
				}
				details = full
			}
			id = result.ObjectID

		case "text":
			resultType = "chunk"
			details = result.Content
			if len(details) > 80 {
				details = details[:77] + "..."
			}
			id = result.DocumentID

		case "relationship":
			resultType = "relationship"
			srcLabel := ""
			if result.SrcKey != nil && *result.SrcKey != "" {
				srcLabel = *result.SrcKey
			} else if result.SrcObjectID != "" {
				srcLabel = result.SrcObjectID
			}
			dstLabel := ""
			if result.DstKey != nil && *result.DstKey != "" {
				dstLabel = *result.DstKey
			} else if result.DstObjectID != "" {
				dstLabel = result.DstObjectID
			}
			details = fmt.Sprintf("%s(%s) -[%s]-> %s(%s)",
				result.SrcObjectType, srcLabel,
				result.RelationshipType,
				result.DstObjectType, dstLabel)
			id = result.SrcObjectID
		}

		details = strings.ReplaceAll(details, "|", "\\|")

		if queryShowScores {
			fmt.Printf("| %s | %s | %s | %s | %.4f |\n", num, resultType, details, id, result.Score)
		} else {
			fmt.Printf("| %s | %s | %s | %s |\n", num, resultType, details, id)
		}
	}
	fmt.Println()

	return nil
}
