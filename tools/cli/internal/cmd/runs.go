package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	agentssdk "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
)

// flags
var (
	runsProjectID  string
	runsLimit      int
	runsStatus     string
	runsJSONOutput bool
)

// runsCmd is the top-level "memory runs" command.
var runsCmd = &cobra.Command{
	Use:   "runs",
	Short: "View agent runs for a project",
	Long: `View agent runs for a project.

This includes runs triggered by /remember, /forget, /query, chat sessions,
and any scheduled or manually triggered agents.

Use "memory runs get <run-id>" to see messages and tool calls for a run.
Use "memory runs logs <run-id>" to stream execution logs for a run.`,
	Args: cobra.NoArgs,
	RunE: runRunsList,
}

var runsGetCmd = &cobra.Command{
	Use:   "get <run-id>",
	Short: "Get full details for a run",
	Long: `Get full details for a run: metadata, messages, and tool calls.

No --project flag is required — run IDs are globally unique.`,
	Args: cobra.ExactArgs(1),
	RunE: runRunsGet,
}

var runsLogsCmd = &cobra.Command{
	Use:   "logs <run-id>",
	Short: "Stream execution logs for a run",
	Long: `Stream the execution log for a run to stdout.

No --project flag is required — run IDs are globally unique.`,
	Args: cobra.ExactArgs(1),
	RunE: runRunsLogs,
}

// ── list ────────────────────────────────────────────────────────────────────

func runRunsList(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, runsProjectID)
	if err != nil {
		return err
	}
	if projectID == "" {
		return fmt.Errorf("project ID required: use --project or set a default project with 'memory projects use'")
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	opts := &agentssdk.ListRunsOptions{
		Limit:  runsLimit,
		Status: runsStatus,
	}

	result, err := c.SDK.Agents.ListProjectRuns(context.Background(), projectID, opts)
	if err != nil {
		return fmt.Errorf("failed to list runs: %w", err)
	}

	runs := result.Data.Items
	total := result.Data.TotalCount // int

	if runsJSONOutput {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result.Data)
	}

	if len(runs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No runs found.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Runs for project %s  (%d total)\n\n", projectID, total)
	for i, r := range runs {
		agentLabel := r.AgentName
		if agentLabel == "" {
			agentLabel = r.AgentID
		}
		// Truncate long agent names
		if len(agentLabel) > 32 {
			agentLabel = agentLabel[:29] + "..."
		}

		statusMark := statusSymbol(r.Status)
		when := timeAgoRun(r.StartedAt)

		fmt.Fprintf(cmd.OutOrStdout(), "%2d. %s  %-36s  %-32s  %s  %s\n",
			i+1, statusMark, r.ID, agentLabel, r.Status, when)

		if r.DurationMs != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "    Duration: %dms\n", *r.DurationMs)
		}
		if r.TokenUsage != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "    Tokens:   %d in / %d out   Cost: $%.6f\n",
				r.TokenUsage.TotalInputTokens, r.TokenUsage.TotalOutputTokens, r.TokenUsage.EstimatedCostUSD)
		}
		if r.ErrorMessage != nil && *r.ErrorMessage != "" {
			msg := *r.ErrorMessage
			if len(msg) > 120 {
				msg = msg[:117] + "..."
			}
			fmt.Fprintf(cmd.OutOrStdout(), "    Error:    %s\n", msg)
		}
		if r.TriggerSource != nil && *r.TriggerSource != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "    Source:   %s\n", *r.TriggerSource)
		}
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if total > len(runs) {
		fmt.Fprintf(cmd.OutOrStdout(), "Showing %d of %d runs. Use --limit to see more.\n", len(runs), total)
	}
	return nil
}

// ── get ─────────────────────────────────────────────────────────────────────

func runRunsGet(cmd *cobra.Command, args []string) error {
	runID := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	// First get the run to find its project ID.
	runResult, err := c.SDK.Agents.GetRunByID(context.Background(), runID)
	if err != nil {
		return fmt.Errorf("failed to get run: %w", err)
	}
	run := runResult.Data

	// Now get the full version (messages + tool calls).
	// We need a project ID — extract from the agent or try the run's project field.
	// GetProjectRunFull requires projectID. Fall back to just showing the basic run
	// if we can't determine project.
	projectID := runsProjectID
	if projectID == "" {
		projectID, _ = resolveProjectContext(cmd, "")
	}

	if runsJSONOutput {
		if projectID != "" {
			fullResult, err := c.SDK.Agents.GetProjectRunFull(context.Background(), projectID, runID)
			if err == nil {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(fullResult.Data)
			}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(run)
	}

	// Print run header
	fmt.Fprintf(cmd.OutOrStdout(), "Run: %s\n", run.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "  Agent:     %s\n", coalesce(run.AgentName, run.AgentID))
	fmt.Fprintf(cmd.OutOrStdout(), "  Status:    %s %s\n", statusSymbol(run.Status), run.Status)
	fmt.Fprintf(cmd.OutOrStdout(), "  Started:   %s\n", fmtTime(run.StartedAt))
	if run.CompletedAt != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Completed: %s\n", fmtTimePTime(run.CompletedAt))
	}
	if run.DurationMs != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Duration:  %dms\n", *run.DurationMs)
	}
	if run.TokenUsage != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Tokens:    %d in / %d out\n",
			run.TokenUsage.TotalInputTokens, run.TokenUsage.TotalOutputTokens)
		fmt.Fprintf(cmd.OutOrStdout(), "  Cost:      $%.6f\n", run.TokenUsage.EstimatedCostUSD)
	}
	if run.Model != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Model:     %s\n", *run.Model)
	}
	if run.TriggerSource != nil && *run.TriggerSource != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Source:    %s\n", *run.TriggerSource)
	}
	if run.TraceID != nil && *run.TraceID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Trace:     %s\n", *run.TraceID)
	}
	if run.ErrorMessage != nil && *run.ErrorMessage != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Error:     %s\n", *run.ErrorMessage)
	}

	// Try to get full details if we have a project ID.
	if projectID == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "\nTip: use --project <id> to also show messages and tool calls.")
		return nil
	}

	full, err := c.SDK.Agents.GetProjectRunFull(context.Background(), projectID, runID)
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "\n(Could not fetch messages/tool calls: %v)\n", err)
		return nil
	}

	// Messages
	if len(full.Data.Messages) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nMessages (%d):\n", len(full.Data.Messages))
		for _, m := range full.Data.Messages {
			content := extractMessageText(m.Content)
			if len(content) > 200 {
				content = content[:197] + "..."
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  [step %d] %s: %s\n", m.StepNumber, m.Role, content)
		}
	}

	// Tool calls
	if len(full.Data.ToolCalls) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nTool Calls (%d):\n", len(full.Data.ToolCalls))
		for _, tc := range full.Data.ToolCalls {
			dur := ""
			if tc.DurationMs != nil {
				dur = fmt.Sprintf("  %dms", *tc.DurationMs)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  [step %d] %s  %s  %s\n",
				tc.StepNumber, tc.ToolName, statusSymbol(tc.Status), dur)
		}
	}

	return nil
}

// ── logs ────────────────────────────────────────────────────────────────────

func runRunsLogs(cmd *cobra.Command, args []string) error {
	runID := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	stream, err := c.SDK.Agents.GetRunLogsText(context.Background(), runID)
	if err != nil {
		return fmt.Errorf("failed to get run logs: %w", err)
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		fmt.Fprintln(cmd.OutOrStdout(), scanner.Text())
	}
	return scanner.Err()
}

// ── helpers ──────────────────────────────────────────────────────────────────

func statusSymbol(status string) string {
	switch strings.ToLower(status) {
	case "completed", "success":
		return "✓"
	case "failed", "error":
		return "✗"
	case "running":
		return "▶"
	case "pending", "queued":
		return "○"
	case "cancelled":
		return "–"
	default:
		return "·"
	}
}

func timeAgoRun(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// extractMessageText pulls the text content from a message content map.
func extractMessageText(content map[string]any) string {
	if content == nil {
		return ""
	}
	// Common patterns: {"text": "..."} or {"parts": [...]}
	if text, ok := content["text"].(string); ok {
		return text
	}
	if parts, ok := content["parts"].([]any); ok {
		var sb strings.Builder
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if t, ok := pm["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	}
	// Fallback: marshal to JSON string
	b, _ := json.Marshal(content)
	return string(b)
}

// ── init ─────────────────────────────────────────────────────────────────────

func init() {
	// list flags (on runsCmd itself — it IS the list command)
	runsCmd.Flags().StringVar(&runsProjectID, "project", "", "Project ID (overrides default project)")
	runsCmd.Flags().IntVar(&runsLimit, "limit", 20, "Maximum number of runs to return (max 100)")
	runsCmd.Flags().StringVar(&runsStatus, "status", "", "Filter by status: running, completed, failed")
	runsCmd.Flags().BoolVar(&runsJSONOutput, "json", false, "Output as JSON")

	// get flags
	runsGetCmd.Flags().StringVar(&runsProjectID, "project", "", "Project ID (needed for messages/tool calls)")
	runsGetCmd.Flags().BoolVar(&runsJSONOutput, "json", false, "Output as JSON")

	runsCmd.AddCommand(runsGetCmd)
	runsCmd.AddCommand(runsLogsCmd)

	rootCmd.AddCommand(runsCmd)
}
