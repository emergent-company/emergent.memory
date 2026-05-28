package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
)

var rememberCmd = &cobra.Command{
	Use:     "remember [text]",
	Short:   "Store information in the knowledge graph using natural language",
	GroupID: "knowledge",
	Long: `Store information in the knowledge graph using natural language.

The domain-remember-agent understands your input, classifies the content,
discovers or reuses a schema pack, and queues structured extraction — all automatically.

You can remember plain text or upload a file (PDF, DOCX, TXT, etc.) which is
converted to plaintext first and then processed the same way.

Schema policy controls what happens when no matching entity type exists:
  reuse_only  Never create new types; use the closest existing type (default)
  auto        Create new schema types as needed
  ask         Prompt before creating any new type (requires interactive terminal)

Examples:
  memory remember "I have to buy toilet paper at Lidl"
  memory remember "Meeting with Sarah tomorrow at 3pm about the Q3 roadmap"
  memory remember --file notes.pdf
  memory remember --file report.docx --guide "this is my quarterly financial report"
  memory remember --guide "shopping list for birthday party" "milk, eggs, candles"
  memory remember --schema-policy reuse_only "Task: fix login bug, priority high"
  memory remember --dry-run "Note: team offsite on 15 June in Berlin"
  memory remember --project abc123 "remember to call dentist next week"`,
	Args: cobra.ArbitraryArgs,
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
	rememberFile         string
	rememberGuide        string
)

func init() {
	rootCmd.AddCommand(rememberCmd)

	rememberCmd.Flags().StringVar(&rememberProjectID, "project", "", "Project ID (uses default project if not specified)")
	rememberCmd.Flags().StringVar(&rememberSchemaPolicy, "schema-policy", "reuse_only", "Schema creation policy: auto, reuse_only, ask")
	rememberCmd.Flags().BoolVar(&rememberDryRun, "dry-run", false, "Create branch and write data but do not merge to main")
	rememberCmd.Flags().BoolVar(&rememberShowTools, "show-tools", false, "Show tool calls made by the agent")
	rememberCmd.Flags().BoolVar(&rememberShowTime, "show-time", false, "Show elapsed time")
	rememberCmd.Flags().BoolVar(&rememberJSON, "json", false, "Output results as JSON")
	rememberCmd.Flags().StringVar(&rememberSessionID, "session", "", "Continue a previous remember session (use session ID printed after a run)")
	rememberCmd.Flags().StringVar(&rememberFile, "file", "", "Path to a file to upload, convert, and remember (PDF, DOCX, TXT, etc.)")
	rememberCmd.Flags().StringVar(&rememberGuide, "guide", "", "Natural-language hint for the agent on how to interpret the content")
}

func runRemember(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	projectID, err := resolveProjectContext(cmd, rememberProjectID)
	if err != nil {
		return err
	}

	c.SetContext("", projectID)

	if rememberFile != "" {
		return runRememberFile(cmd.Context(), c, rememberFile, projectID)
	}

	if len(args) == 0 {
		return fmt.Errorf("text argument is required (or use --file to upload a file)")
	}
	text := strings.Join(args, " ")
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
	if rememberGuide != "" {
		reqBody["guide"] = rememberGuide
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

	return streamRememberSSE(ctx, httpReq, text, projectID)
}

// runRememberFile posts to POST /api/projects/:projectId/remember/file as multipart
// and streams the SSE response.
func runRememberFile(ctx context.Context, c *client.Client, filePath, projectID string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q: %w", filePath, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Add optional parameters.
	_ = mw.WriteField("schema_policy", rememberSchemaPolicy)
	if rememberDryRun {
		_ = mw.WriteField("dry_run", "true")
	}
	if rememberGuide != "" {
		_ = mw.WriteField("guide", rememberGuide)
	}
	if rememberSessionID != "" {
		_ = mw.WriteField("conversation_id", rememberSessionID)
	}

	if err := mw.Close(); err != nil {
		return fmt.Errorf("failed to finalise multipart form: %w", err)
	}

	endpoint := c.BaseURL() + "/api/projects/" + url.PathEscape(projectID) + "/remember/file"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, &buf)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", mw.FormDataContentType())
	if err := c.SDK.AuthenticateRequest(httpReq); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	label := filepath.Base(filePath)
	if rememberGuide != "" {
		label = rememberGuide
	}
	return streamRememberSSE(ctx, httpReq, label, projectID)
}

// streamRememberSSE executes the request and parses the SSE stream, printing output.
func streamRememberSSE(ctx context.Context, httpReq *http.Request, label, projectID string) error {
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
			"label":         label,
			"projectId":     projectID,
			"schema_policy": rememberSchemaPolicy,
			"dry_run":       rememberDryRun,
			"response":      response.String(),
			"tools":         tools,
			"elapsedMs":     elapsed.Milliseconds(),
		}
		if rememberGuide != "" {
			out["guide"] = rememberGuide
		}
		if rememberFile != "" {
			out["file"] = rememberFile
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
