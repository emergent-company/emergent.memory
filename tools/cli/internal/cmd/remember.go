package cmd

import (
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
		return runRememberFile(cmd.Context(), cmd.OutOrStdout(), c, rememberFile, projectID)
	}

	if len(args) == 0 {
		return fmt.Errorf("text argument is required (or use --file to upload a file)")
	}
	text := strings.Join(args, " ")
	return runRememberAgent(cmd.Context(), cmd.OutOrStdout(), c, text, projectID)
}

// runRememberAgent posts to POST /api/projects/:projectId/remember and streams the SSE response.
func runRememberAgent(ctx context.Context, out io.Writer, c *client.Client, text, projectID string) error {
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

	return streamRememberSSE(out, httpReq, text, projectID)
}

// runRememberFile posts to POST /api/projects/:projectId/remember/file as multipart
// and streams the SSE response.
func runRememberFile(ctx context.Context, out io.Writer, c *client.Client, filePath, projectID string) error {
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
	return streamRememberSSE(out, httpReq, label, projectID)
}

// streamRememberSSE executes the request and parses the SSE stream, printing output.
func streamRememberSSE(out io.Writer, httpReq *http.Request, label, projectID string) error {
	jsonMode := rememberJSON || output == "json"
	result, err := StreamSSE(httpReq, SSEOptions{
		LivePrint: !jsonMode,
		ShowTools: rememberShowTools,
		JSONMode:  jsonMode,
	})
	if err != nil {
		return err
	}

	if jsonMode {
		jsonOut := map[string]interface{}{
			"label":         label,
			"projectId":     projectID,
			"schema_policy": rememberSchemaPolicy,
			"dry_run":       rememberDryRun,
			"response":      result.Response,
			"tools":         result.Tools,
			"elapsedMs":     result.Elapsed.Milliseconds(),
		}
		if rememberGuide != "" {
			jsonOut["guide"] = rememberGuide
		}
		if rememberFile != "" {
			jsonOut["file"] = rememberFile
		}
		if result.StreamErr != "" {
			jsonOut["error"] = result.StreamErr
		}
		if result.SessionID != "" {
			jsonOut["session_id"] = result.SessionID
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(jsonOut)
	}

	fmt.Printf("\n\n")
	if rememberShowTools && len(result.Tools) > 0 {
		fmt.Printf("Tools used: %s\n", strings.Join(result.Tools, ", "))
	}
	if rememberShowTime {
		fmt.Printf("Time: %v\n", result.Elapsed.Round(time.Millisecond))
	}
	if rememberDryRun {
		fmt.Printf("(dry run — branch not merged)\n")
	}
	if result.SessionID != "" {
		fmt.Printf("Session: %s  (use --session %s to continue)\n", result.SessionID, result.SessionID)
	}

	if result.StreamErr != "" {
		return fmt.Errorf("%s", result.StreamErr)
	}
	return nil
}
