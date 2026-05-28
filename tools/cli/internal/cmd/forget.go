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

	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
)

var forgetCmd = &cobra.Command{
	Use:     "forget [text]",
	Short:   "Soft-delete knowledge graph objects matching a natural-language query",
	GroupID: "knowledge",
	Long: `Find and soft-delete graph objects that match a natural-language query.

Deletes are REVERSIBLE — use 'memory graph restore <id>' to undo any deletion.

Strategy controls confirmation behaviour:
  confirm   Ask for approval before executing a batch of deletes (default)
  auto      Delete automatically without asking
  ask       Require per-delete confirmation (interactive terminals only)

Cascade depth controls how many hops of neighbors are also deleted:
  1   Direct matches only
  2   Direct matches + 1-hop neighbors (default)
  3   Direct matches + 2-hop neighbors

Examples:
  memory forget "all auth related nodes"
  memory forget --strategy auto "temporary test objects"
  memory forget --cascade 1 "the node named foo"
  memory forget --dry-run "everything tagged deprecated"
  memory forget --project abc123 "old migration objects"`,
	Args: cobra.ArbitraryArgs,
	RunE: runForget,
}

var (
	forgetProjectID string
	forgetStrategy  string
	forgetCascade   int
	forgetDryRun    bool
	forgetShowTools bool
	forgetShowTime  bool
	forgetJSON      bool
	forgetSessionID string
)

func init() {
	rootCmd.AddCommand(forgetCmd)

	forgetCmd.Flags().StringVar(&forgetProjectID, "project", "", "Project ID (uses default project if not specified)")
	forgetCmd.Flags().StringVar(&forgetStrategy, "strategy", "confirm", "Deletion strategy: auto, confirm, ask")
	forgetCmd.Flags().IntVar(&forgetCascade, "cascade", 2, "Cascade depth: 1 (direct only), 2 (1-hop), 3 (2-hop)")
	forgetCmd.Flags().BoolVar(&forgetDryRun, "dry-run", false, "Report what would be deleted without performing any deletes")
	forgetCmd.Flags().BoolVar(&forgetShowTools, "show-tools", false, "Show tool calls made by the agent")
	forgetCmd.Flags().BoolVar(&forgetShowTime, "show-time", false, "Show elapsed time")
	forgetCmd.Flags().BoolVar(&forgetJSON, "json", false, "Output results as JSON")
	forgetCmd.Flags().StringVar(&forgetSessionID, "session", "", "Continue a previous forget session")
}

func runForget(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	projectID, err := resolveProjectContext(cmd, forgetProjectID)
	if err != nil {
		return err
	}

	c.SetContext("", projectID)

	if len(args) == 0 {
		return fmt.Errorf("text argument is required — describe what to forget")
	}
	text := strings.Join(args, " ")
	return runForgetAgent(cmd.Context(), c, text, projectID)
}

// runForgetAgent posts to POST /api/projects/:projectId/forget and streams the SSE response.
func runForgetAgent(ctx context.Context, c *client.Client, text, projectID string) error {
	reqBody := map[string]interface{}{
		"message":       text,
		"strategy":      forgetStrategy,
		"cascade_depth": forgetCascade,
		"dry_run":       forgetDryRun,
	}
	if forgetSessionID != "" {
		reqBody["conversation_id"] = forgetSessionID
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := c.BaseURL() + "/api/projects/" + url.PathEscape(projectID) + "/forget"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.SDK.AuthenticateRequest(httpReq); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	return streamForgetSSE(httpReq, text, projectID)
}

// streamForgetSSE executes the request and parses the SSE stream, printing output.
func streamForgetSSE(httpReq *http.Request, label, projectID string) error {
	start := time.Now()
	httpClient := &http.Client{Timeout: 0}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}

	var response strings.Builder
	var tools []string
	var streamErr string
	var sessionID string
	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("error reading response: %w", err)
			}
			break
		}
		line = strings.TrimRight(line, "\r\n")
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
		case "meta":
			if id, ok := event["conversationId"].(string); ok && id != "" {
				sessionID = id
			}
		case "token":
			if token, ok := event["token"].(string); ok {
				response.WriteString(token)
				if !forgetJSON && output != "json" {
					fmt.Print(token)
				}
			}
		case "mcp_tool":
			if status, ok := event["status"].(string); ok && status == "started" {
				if tool, ok := event["tool"].(string); ok {
					tools = append(tools, tool)
					if forgetShowTools {
						fmt.Printf("\n[Tool: %s]\n", tool)
					}
				}
			}
		case "error":
			if errMsg, ok := event["error"].(string); ok {
				streamErr = errMsg
				if !forgetJSON && output != "json" {
					fmt.Fprintf(os.Stderr, "\nError: %s\n", errMsg)
				}
			}
		}
	}

	elapsed := time.Since(start)

	if forgetJSON || output == "json" {
		out := map[string]interface{}{
			"label":         label,
			"projectId":     projectID,
			"strategy":      forgetStrategy,
			"cascade_depth": forgetCascade,
			"dry_run":       forgetDryRun,
			"response":      response.String(),
			"tools":         tools,
			"elapsedMs":     elapsed.Milliseconds(),
		}
		if streamErr != "" {
			out["error"] = streamErr
		}
		if sessionID != "" {
			out["session_id"] = sessionID
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(out)
	}

	fmt.Printf("\n\n")
	if forgetShowTools && len(tools) > 0 {
		fmt.Printf("Tools used: %s\n", strings.Join(tools, ", "))
	}
	if forgetShowTime {
		fmt.Printf("Time: %v\n", elapsed.Round(time.Millisecond))
	}
	if forgetDryRun {
		fmt.Printf("(dry run — no deletes performed)\n")
	}
	if sessionID != "" {
		fmt.Printf("Session: %s  (use --session %s to continue)\n", sessionID, sessionID)
	}

	if streamErr != "" {
		return fmt.Errorf("%s", streamErr)
	}
	return nil
}
