package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Long: `Query a project using natural language search across graph objects and documents.

By default, uses an AI agent that can reason about complex queries and traverse the graph.
Use --mode=search for direct hybrid search without reasoning.

Examples:
  emergent query "who directed fight club and what are their other movies?"
  emergent query --mode=search "fight club"
  emergent query --project-id abc123 "list all requirements"
  emergent query --agent-id <id> "complex question"`,
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
	queryAgentID        string
	queryShowTools      bool
)

func init() {
	rootCmd.AddCommand(queryCmd)

	queryCmd.Flags().StringVar(&queryProjectID, "project-id", "", "Project ID to query (uses default project if not specified)")
	queryCmd.Flags().StringVar(&queryMode, "mode", "agent", "Query mode: agent (default, uses AI reasoning) or search (direct hybrid search)")
	queryCmd.Flags().StringVar(&queryAgentID, "agent-id", "70356e5f-2c97-4ce4-9754-ec14e15a2a13", "Agent definition ID to use (only for agent mode)")
	queryCmd.Flags().BoolVar(&queryShowTools, "show-tools", false, "Show tool calls made by the agent (only for agent mode)")
	queryCmd.Flags().IntVar(&queryLimit, "limit", 10, "Maximum number of results to return (only for search mode)")
	queryCmd.Flags().StringVar(&queryResultTypes, "result-types", "both", "Types of results: graph, text, or both (only for search mode)")
	queryCmd.Flags().StringVar(&queryFusionStrategy, "fusion-strategy", "weighted", "Fusion strategy: weighted, rrf, interleave, graph_first, text_first (only for search mode)")
	queryCmd.Flags().BoolVar(&queryJSON, "json", false, "Output results as JSON")
	queryCmd.Flags().BoolVar(&queryDebug, "debug", false, "Include debug information in output")
}

func runQuery(cmd *cobra.Command, args []string) error {
	// Join all args as the query
	query := strings.Join(args, " ")

	// Get client
	client, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	projectID, err := resolveProjectContext(cmd, queryProjectID)
	if err != nil {
		return err
	}

	// Set context (also updates org ID if needed)
	orgID := "" // Will use default from config
	client.SetContext(orgID, projectID)

	// Route to appropriate query mode
	if queryMode == "agent" {
		return runAgentQuery(cmd.Context(), client, query, projectID)
	} else if queryMode == "search" {
		return runSearchQuery(cmd.Context(), client, query, projectID)
	} else {
		return fmt.Errorf("invalid mode: %s (must be 'agent' or 'search')", queryMode)
	}
}

func runAgentQuery(ctx context.Context, c *client.Client, query, projectID string) error {
	// Build request
	reqBody := map[string]interface{}{
		"message":           query,
		"agentDefinitionId": queryAgentID,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL()+"/api/chat/stream", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Project-ID", projectID)

	// Add authentication
	if apiKey := c.APIKey(); apiKey != "" {
		httpReq.Header.Set("X-API-Key", apiKey)
	}

	// Execute request
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

	// Human-readable output already printed during streaming
	fmt.Printf("\n\n")
	if queryShowTools && len(tools) > 0 {
		fmt.Printf("Tools used: %s\n", strings.Join(tools, ", "))
	}
	fmt.Printf("Time: %v\n", elapsed.Round(time.Millisecond))

	return nil
}

func runSearchQuery(ctx context.Context, client *client.Client, query, projectID string) error {

	start := time.Now()
	response, err := client.SDK.Search.Search(ctx, &search.SearchRequest{
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

	// Output results
	if queryJSON {
		// JSON output
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

	// Human-readable output
	fmt.Printf("Query: %s\n", query)
	fmt.Printf("Project ID: %s\n", projectID)
	fmt.Printf("Results: %d (showing up to %d)\n", response.Total, queryLimit)
	fmt.Printf("Time: %v\n\n", elapsed.Round(time.Millisecond))

	if response.Total == 0 {
		fmt.Println("No results found.")
		fmt.Println("\nTips:")
		fmt.Println("  - Try broader search terms")
		fmt.Println("  - Check that the project has data indexed")
		fmt.Println("  - Use --debug flag to see more information")
		return nil
	}

	// Print results
	for i, result := range response.Results {
		fmt.Printf("─────────────────────────────────────────────────────────────\n")
		fmt.Printf("Result %d (score: %.4f)\n", i+1, result.Score)
		fmt.Printf("─────────────────────────────────────────────────────────────\n")

		// Handle different result types
		switch result.Type {
		case "graph":
			// Graph result
			fmt.Printf("Type: Graph Object\n")
			fmt.Printf("Object Type: %s\n", result.ObjectType)
			if result.Key != "" {
				fmt.Printf("Key: %s\n", result.Key)
			}
			if result.ObjectID != "" {
				fmt.Printf("ID: %s\n", result.ObjectID)
			}

			// Print fields
			if len(result.Fields) > 0 {
				fmt.Printf("\nFields:\n")
				for key, value := range result.Fields {
					// Format value
					var valueStr string
					switch v := value.(type) {
					case string:
						if len(v) > 200 {
							valueStr = v[:200] + "..."
						} else {
							valueStr = v
						}
					case []interface{}:
						valueStr = fmt.Sprintf("[%d items]", len(v))
					case map[string]interface{}:
						valueStr = fmt.Sprintf("{%d fields}", len(v))
					default:
						valueStr = fmt.Sprintf("%v", v)
					}
					fmt.Printf("  %s: %s\n", key, valueStr)
				}
			}

			// Print scores breakdown if available
			if result.LexicalScore != nil || result.VectorScore != nil {
				fmt.Printf("\nScore Breakdown:\n")
				if result.LexicalScore != nil {
					fmt.Printf("  Lexical: %.4f\n", *result.LexicalScore)
				}
				if result.VectorScore != nil {
					fmt.Printf("  Vector: %.4f\n", *result.VectorScore)
				}
			}

		case "text":
			// Text result
			fmt.Printf("Type: Document Chunk\n")
			fmt.Printf("Document ID: %s\n", result.DocumentID)
			if result.ChunkID != "" {
				fmt.Printf("Chunk ID: %s\n", result.ChunkID)
			}
			if result.Content != "" {
				fmt.Printf("\nContent:\n%s\n", result.Content)
			}

		case "relationship":
			// Relationship result
			fmt.Printf("Type: Relationship\n")
			if result.RelationshipType != "" {
				fmt.Printf("Relationship Type: %s\n", result.RelationshipType)
			}
			if result.SrcObjectType != "" && result.DstObjectType != "" {
				srcKey := ""
				if result.SrcKey != nil {
					srcKey = *result.SrcKey
				}
				dstKey := ""
				if result.DstKey != nil {
					dstKey = *result.DstKey
				}
				fmt.Printf("Source: %s (%s)\n", result.SrcObjectType, srcKey)
				fmt.Printf("Target: %s (%s)\n", result.DstObjectType, dstKey)
			}
		}

		fmt.Println()
	}

	return nil
}
