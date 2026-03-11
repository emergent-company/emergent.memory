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
	"github.com/emergent-company/emergent.memory/tools/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var askCmd = &cobra.Command{
	Use:     "ask <question>",
	Short:   "Ask the Memory CLI assistant a question or request a task",
	GroupID: "ai",
	Long: `Ask the Memory CLI assistant a question or request a task.

The assistant is context-aware — it adapts its responses based on whether you
are authenticated and whether a project is configured:

  • Not authenticated     → documentation answers; explains how to log in
  • Auth, no project      → account-level tasks + documentation answers
  • Auth + project active → full task execution + documentation answers

The assistant fetches live documentation from the Memory docs site to answer
questions about the CLI, SDK, REST API, agents, and knowledge graph features.
It can also execute tasks on your behalf (list agents, query the graph, etc.).

Examples:
  memory ask "what are native tools?"
  memory ask "what agents do I have configured?"
  memory ask "how do I create a schema?"
  memory ask --project abc123 "list all agent runs from today"
  memory ask "what commands are available for managing API tokens?"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAsk,
}

var (
	askProjectID string
	askShowTools bool
	askShowTime  bool
	askJSON      bool
)

func init() {
	rootCmd.AddCommand(askCmd)

	askCmd.Flags().StringVar(&askProjectID, "project", "", "Project ID (optional — uses default project if configured)")
	askCmd.Flags().BoolVar(&askShowTools, "show-tools", false, "Show tool calls made by the assistant during reasoning")
	askCmd.Flags().BoolVar(&askShowTime, "show-time", false, "Show elapsed time at the end of the response")
	askCmd.Flags().BoolVar(&askJSON, "json", false, "Output result as JSON {question, response, tools, elapsedMs}")
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	// --- Resolve client (best-effort; auth is optional for `ask`) ---

	// Load config to get the server URL. getClient fails when no credentials are
	// set, but we still need the URL to send an unauthenticated request so the
	// server can return a helpful "please log in" response.
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = config.DiscoverPath("")
	}
	cfg, cfgErr := config.LoadWithEnv(configPath)

	// Apply global flag overrides (same logic as getClient in projects.go).
	if cfgErr == nil {
		if v := viper.GetString("server"); v != "" {
			cfg.ServerURL = v
		}
		if v := viper.GetString("project_token"); v != "" {
			cfg.ProjectToken = v
		}
	}

	// Determine the base server URL. Fall back to the default hosted API if none
	// is configured so the command is still useful right after installation.
	baseURL := ""
	if cfgErr == nil && cfg.ServerURL != "" {
		baseURL = cfg.ServerURL
	} else {
		baseURL = "https://api.dev.emergent-company.ai"
	}

	// Try to get a full authenticated client.
	var c *client.Client
	if cfgErr == nil && cfg.ServerURL != "" {
		if cl, err := client.New(cfg); err == nil {
			c = cl
		}
	}

	// --- Resolve project context (best-effort; non-blocking) ---

	projectID := ""
	if askProjectID != "" {
		projectID = askProjectID
	} else if c != nil {
		// resolveProjectContext may show an interactive picker; suppress errors so
		// `ask` never blocks when the user omits --project.
		if pid, err := resolveProjectContext(cmd, ""); err == nil {
			projectID = pid
		}
	}

	if c != nil && projectID != "" {
		c.SetContext("", projectID)
	}

	return runAskStream(cmd.Context(), c, baseURL, question, projectID)
}

// runAskStream posts to the appropriate ask endpoint and streams the SSE response.
// c may be nil when the caller has no valid credentials (unauthenticated path).
func runAskStream(ctx context.Context, c *client.Client, baseURL, question, projectID string) error {
	reqBody := map[string]interface{}{
		"message": question,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Choose endpoint: project-scoped when a project is available, user-level otherwise.
	var endpoint string
	if projectID != "" {
		endpoint = baseURL + "/api/projects/" + url.PathEscape(projectID) + "/ask"
	} else {
		endpoint = baseURL + "/api/ask"
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Attach auth headers (and context headers) using the SDK's auth provider.
	// This correctly handles all credential types: plain API keys send X-API-Key,
	// project tokens and OIDC tokens send Authorization: Bearer.
	if c != nil {
		if err := c.SDK.AuthenticateRequest(httpReq); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}

	start := time.Now()
	httpClient := &http.Client{Timeout: 180 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse and stream SSE events.
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
				if !askJSON {
					fmt.Print(token)
				}
			}
		case "mcp_tool":
			if status, ok := event["status"].(string); ok && status == "started" {
				if tool, ok := event["tool"].(string); ok {
					tools = append(tools, tool)
					if askShowTools {
						fmt.Fprintf(os.Stderr, "\n[Tool: %s]\n", tool)
					}
				}
			}
		case "error":
			if errMsg, ok := event["error"].(string); ok {
				if !askJSON {
					fmt.Fprintf(os.Stderr, "\nError: %s\n", errMsg)
				}
			}
		}
	}

	elapsed := time.Since(start)

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading response: %w", err)
	}

	if askJSON {
		output := map[string]interface{}{
			"question":  question,
			"response":  response.String(),
			"tools":     tools,
			"elapsedMs": elapsed.Milliseconds(),
		}
		if projectID != "" {
			output["projectId"] = projectID
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}

	fmt.Printf("\n\n")
	if askShowTools && len(tools) > 0 {
		fmt.Fprintf(os.Stderr, "Tools used: %s\n", strings.Join(tools, ", "))
	}
	if askShowTime {
		fmt.Fprintf(os.Stderr, "Time: %v\n", elapsed.Round(time.Millisecond))
	}

	return nil
}
