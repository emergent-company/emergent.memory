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

var rememberCmd = &cobra.Command{
	Use:     "remember <text>",
	Short:   "Store information in the knowledge graph using natural language",
	GroupID: "knowledge",
	Long: `Store information in the knowledge graph using natural language.

The graph-insert-agent understands your input, checks for existing entities to avoid
duplicates, creates a branch, writes structured data (entities + relationships), and
merges back to the main graph — all automatically.

Schema policy controls what happens when no matching entity type exists:
  auto        Create new schema types as needed (default)
  reuse_only  Never create new types; use the closest existing type
  ask         Prompt before creating any new type (requires interactive terminal)

Examples:
  memory remember "I have to buy toilet paper at Lidl"
  memory remember "Meeting with Sarah tomorrow at 3pm about the Q3 roadmap"
  memory remember "The API server is deployed on aws-eu-west-1, running Go 1.22"
  memory remember --schema-policy reuse_only "Task: fix login bug, priority high"
  memory remember --dry-run "Note: team offsite on 15 June in Berlin"
  memory remember --project abc123 "remember to call dentist next week"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRemember,
}

var (
	rememberProjectID    string
	rememberSchemaPolicy string
	rememberDryRun       bool
	rememberShowTools    bool
	rememberShowTime     bool
	rememberJSON         bool
	rememberSessionID    string
)

func init() {
	rootCmd.AddCommand(rememberCmd)

	rememberCmd.Flags().StringVar(&rememberProjectID, "project", "", "Project ID (uses default project if not specified)")
	rememberCmd.Flags().StringVar(&rememberSchemaPolicy, "schema-policy", "auto", "Schema creation policy: auto, reuse_only, ask")
	rememberCmd.Flags().BoolVar(&rememberDryRun, "dry-run", false, "Create branch and write data but do not merge to main")
	rememberCmd.Flags().BoolVar(&rememberShowTools, "show-tools", false, "Show tool calls made by the agent")
	rememberCmd.Flags().BoolVar(&rememberShowTime, "show-time", false, "Show elapsed time")
	rememberCmd.Flags().BoolVar(&rememberJSON, "json", false, "Output results as JSON")
	rememberCmd.Flags().StringVar(&rememberSessionID, "session", "", "Continue a previous remember session (use session ID printed after a run)")
}

func runRemember(cmd *cobra.Command, args []string) error {
	text := strings.Join(args, " ")

	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	projectID, err := resolveProjectContext(cmd, rememberProjectID)
	if err != nil {
		return err
	}

	c.SetContext("", projectID)

	return runRememberAgent(cmd.Context(), c, text, projectID)
}

// runRememberAgent posts to POST /api/projects/:projectId/remember and streams the SSE response.
func runRememberAgent(ctx context.Context, c *client.Client, text, projectID string) error {
	reqBody := map[string]interface{}{
		"message":       text,
		"schema_policy": rememberSchemaPolicy,
		"dry_run":       rememberDryRun,
	}
	if rememberSessionID != "" {
		reqBody["conversation_id"] = rememberSessionID
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := c.BaseURL() + "/api/projects/" + url.PathEscape(projectID) + "/remember"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if err := c.SDK.AuthenticateRequest(httpReq); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	start := time.Now()
	httpClient := &http.Client{Timeout: 0} // no timeout — SSE streams until server closes
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}

	// Parse SSE stream.
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
				if !rememberJSON && output != "json" {
					fmt.Print(token)
				}
			}
		case "mcp_tool":
			if status, ok := event["status"].(string); ok && status == "started" {
				if tool, ok := event["tool"].(string); ok {
					tools = append(tools, tool)
					if rememberShowTools {
						fmt.Printf("\n[Tool: %s]\n", tool)
					}
				}
			}
		case "error":
			if errMsg, ok := event["error"].(string); ok {
				streamErr = errMsg
				if !rememberJSON && output != "json" {
					fmt.Fprintf(os.Stderr, "\nError: %s\n", errMsg)
				}
			}
		}
	}

	elapsed := time.Since(start)

	if rememberJSON || output == "json" {
		out := map[string]interface{}{
			"text":          text,
			"projectId":     projectID,
			"schema_policy": rememberSchemaPolicy,
			"dry_run":       rememberDryRun,
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
	if rememberShowTools && len(tools) > 0 {
		fmt.Printf("Tools used: %s\n", strings.Join(tools, ", "))
	}
	if rememberShowTime {
		fmt.Printf("Time: %v\n", elapsed.Round(time.Millisecond))
	}
	if rememberDryRun {
		fmt.Printf("(dry run — branch not merged)\n")
	}
	if sessionID != "" {
		fmt.Printf("Session: %s  (use --session %s to continue)\n", sessionID, sessionID)
	}

	if streamErr != "" {
		return fmt.Errorf("%s", streamErr)
	}
	return nil
}
