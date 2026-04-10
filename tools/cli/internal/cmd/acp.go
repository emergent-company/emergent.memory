package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/acp"
	sdkerrors "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/errors"
	"github.com/spf13/cobra"
)

// ── ACP client construction ──────────────────────────────────────────────────

// getACPClient creates an ACP SDK client from the current CLI config.
func getACPClient(cmd *cobra.Command) (*acp.Client, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, err
	}
	baseURL := c.BaseURL()
	token := c.AuthorizationHeader()
	if token == "" {
		return nil, fmt.Errorf("no authentication configured — run 'memory login' or set MEMORY_PROJECT_TOKEN")
	}
	// AuthorizationHeader returns "Bearer <token>"; we need just the token.
	token = strings.TrimPrefix(token, "Bearer ")
	return acp.NewClient(baseURL, token), nil
}

// ── Root command: memory acp ─────────────────────────────────────────────────

var acpCmd = &cobra.Command{
	Use:     "acp",
	Short:   "Agent Communication Protocol (ACP) operations",
	Long:    "Commands for interacting with agents via the Agent Communication Protocol (ACP) v1 API.",
	GroupID: "ai",
}

// ── memory acp ping ──────────────────────────────────────────────────────────

var acpPingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Check that the ACP endpoint is reachable",
	Long: `Ping the ACP v1 endpoint on the configured Memory server.

This command does not require authentication.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient(cmd)
		if err != nil {
			return err
		}
		// Ping doesn't need auth, but we still need the base URL.
		client := acp.NewClient(c.BaseURL(), "")
		ctx := context.Background()
		if err := client.Ping(ctx); err != nil {
			return fmt.Errorf("ACP endpoint is not reachable: %w", err)
		}
		fmt.Println("ACP endpoint is reachable")
		return nil
	},
}

// ── memory acp agents ────────────────────────────────────────────────────────

var acpAgentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage ACP agents",
	Long:  "Discover and inspect agents exposed via the Agent Communication Protocol.",
}

var acpAgentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List externally-visible ACP agents",
	Long: `List all agents with visibility='external' that are exposed via ACP.

Displays a table with NAME, DESCRIPTION, VERSION, and SUCCESS RATE columns.
Use --json to output the raw JSON response.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()
		agents, err := client.ListAgents(ctx)
		if err != nil {
			return fmt.Errorf("failed to list ACP agents: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(agents)
		}

		if len(agents) == 0 {
			fmt.Println("No externally-visible agents found")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDESCRIPTION\tVERSION\tSUCCESS RATE")
		for _, a := range agents {
			desc := truncate(a.Description, 50)
			version := a.Version
			if version == "" {
				version = "-"
			}
			successRate := "-"
			if a.Status != nil && a.Status.SuccessRate != nil {
				successRate = fmt.Sprintf("%.0f%%", *a.Status.SuccessRate*100)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Name, desc, version, successRate)
		}
		return w.Flush()
	},
}

var acpAgentsGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get an ACP agent manifest",
	Long: `Get the full manifest for a specific ACP agent by its slug name.

Displays the agent's name, description, capabilities, input/output modes,
and status metrics. Use --json for the raw JSON response.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()
		agent, err := client.GetAgent(ctx, args[0])
		if err != nil {
			if sdkerrors.IsNotFound(err) {
				return fmt.Errorf("Agent '%s' not found", args[0])
			}
			return fmt.Errorf("failed to get agent: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(agent)
		}

		// Formatted output
		fmt.Printf("Name:        %s\n", agent.Name)
		fmt.Printf("Description: %s\n", agent.Description)
		fmt.Printf("Version:     %s\n", agent.Version)
		if agent.Provider != nil {
			fmt.Printf("Provider:    %s\n", agent.Provider.Organization)
		}
		if agent.Framework != "" {
			fmt.Printf("Framework:   %s\n", agent.Framework)
		}
		if agent.Capabilities != nil {
			fmt.Println("Capabilities:")
			fmt.Printf("  Streaming:        %v\n", agent.Capabilities.Streaming)
			fmt.Printf("  Human-in-the-loop: %v\n", agent.Capabilities.HumanInTheLoop)
			fmt.Printf("  Session support:  %v\n", agent.Capabilities.SessionSupport)
		}
		if len(agent.DefaultInputModes) > 0 {
			fmt.Printf("Input modes:  %s\n", strings.Join(agent.DefaultInputModes, ", "))
		}
		if len(agent.DefaultOutputModes) > 0 {
			fmt.Printf("Output modes: %s\n", strings.Join(agent.DefaultOutputModes, ", "))
		}
		if len(agent.Tags) > 0 {
			fmt.Printf("Tags:        %s\n", strings.Join(agent.Tags, ", "))
		}
		if len(agent.Domains) > 0 {
			fmt.Printf("Domains:     %s\n", strings.Join(agent.Domains, ", "))
		}
		if agent.Status != nil {
			fmt.Println("Status:")
			if agent.Status.SuccessRate != nil {
				fmt.Printf("  Success rate:     %.1f%%\n", *agent.Status.SuccessRate*100)
			}
			if agent.Status.AvgRunTokens != nil {
				fmt.Printf("  Avg tokens/run:   %.0f\n", *agent.Status.AvgRunTokens)
			}
			if agent.Status.AvgRunTimeSeconds != nil {
				fmt.Printf("  Avg run time:     %.1fs\n", *agent.Status.AvgRunTimeSeconds)
			}
		}
		return nil
	},
}

// ── memory acp runs ──────────────────────────────────────────────────────────

var (
	acpRunMessage   string
	acpRunMode      string
	acpRunSessionID string
	acpResumeMsg    string
	acpResumeMode   string
)

var acpRunsCmd = &cobra.Command{
	Use:   "runs",
	Short: "Manage ACP agent runs",
	Long:  "Create, inspect, cancel, and resume agent runs via ACP.",
}

var acpRunsCreateCmd = &cobra.Command{
	Use:   "create <agent-name>",
	Short: "Create a new ACP agent run",
	Long: `Create a new run for an ACP agent.

Requires --message with the user's input text. The --mode flag controls
execution mode:
  sync   (default) — blocks until the run completes, prints output
  async  — returns immediately with the run ID and status
  stream — streams the agent's output to stdout in real-time

If a sync run pauses for human input (status: input-required), the CLI
will prompt you interactively and automatically resume the run.

Use --session to link the run to an existing ACP session.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		if acpRunMessage == "" {
			return fmt.Errorf("--message flag is required")
		}

		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		req := acp.CreateRunRequest{
			Message: []acp.MessagePart{
				{ContentType: "text/plain", Content: acpRunMessage},
			},
			Mode: acpRunMode,
		}
		if acpRunSessionID != "" {
			req.SessionID = &acpRunSessionID
		}

		// Stream mode
		if acpRunMode == "stream" {
			return streamRun(ctx, client, agentName, req)
		}

		// Sync or async mode
		run, err := client.CreateRun(ctx, agentName, req)
		if err != nil {
			return fmt.Errorf("failed to create run: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(run)
		}

		// Async mode: print ID and exit
		if acpRunMode == "async" {
			fmt.Printf("Run ID:  %s\n", run.ID)
			fmt.Printf("Status:  %s\n", run.Status)
			return nil
		}

		// Sync mode: handle input-required loop
		for run.Status == "input-required" {
			run, err = handleInputRequired(ctx, client, agentName, run)
			if err != nil {
				return err
			}
		}

		printRunOutput(run)
		return nil
	},
}

// streamRun handles the stream mode for run creation.
func streamRun(ctx context.Context, client *acp.Client, agentName string, req acp.CreateRunRequest) error {
	stream, err := client.CreateRunStream(ctx, agentName, req)
	if err != nil {
		return fmt.Errorf("failed to create streaming run: %w", err)
	}
	defer stream.Close()

	for {
		event, err := stream.Next()
		if err == io.EOF {
			fmt.Println() // final newline
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		switch event.Type {
		case "text_delta":
			if text, ok := event.Data["content"].(string); ok {
				fmt.Print(text)
			}
		case "status":
			// Could print status changes in debug mode
		case "error":
			if msg, ok := event.Data["message"].(string); ok {
				return fmt.Errorf("agent error: %s", msg)
			}
		case "done":
			fmt.Println()
			return nil
		}
	}
}

// streamResume handles the stream mode for run resume.
func streamResume(ctx context.Context, client *acp.Client, agentName, runID string, req acp.ResumeRunRequest) error {
	stream, err := client.ResumeRunStream(ctx, agentName, runID, req)
	if err != nil {
		return fmt.Errorf("failed to resume streaming run: %w", err)
	}
	defer stream.Close()

	for {
		event, err := stream.Next()
		if err == io.EOF {
			fmt.Println()
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		switch event.Type {
		case "text_delta":
			if text, ok := event.Data["content"].(string); ok {
				fmt.Print(text)
			}
		case "error":
			if msg, ok := event.Data["message"].(string); ok {
				return fmt.Errorf("agent error: %s", msg)
			}
		case "done":
			fmt.Println()
			return nil
		}
	}
}

// handleInputRequired prompts the user interactively for a question response
// and resumes the run.
func handleInputRequired(ctx context.Context, client *acp.Client, agentName string, run *acp.RunObject) (*acp.RunObject, error) {
	if run.AwaitRequest == nil {
		return nil, fmt.Errorf("run is in input-required status but has no await_request")
	}

	fmt.Printf("\nAgent is asking: %s\n", run.AwaitRequest.Question)

	if len(run.AwaitRequest.Options) > 0 {
		fmt.Println("Options:")
		for i, opt := range run.AwaitRequest.Options {
			if opt.Description != "" {
				fmt.Printf("  %d) %s — %s\n", i+1, opt.Label, opt.Description)
			} else {
				fmt.Printf("  %d) %s\n", i+1, opt.Label)
			}
		}
	}

	fmt.Print("\nYour response: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, fmt.Errorf("no input provided")
	}
	response := strings.TrimSpace(scanner.Text())
	if response == "" {
		return nil, fmt.Errorf("empty response — run remains in input-required state")
	}

	req := acp.ResumeRunRequest{
		Message: []acp.MessagePart{
			{ContentType: "text/plain", Content: response},
		},
		Mode: "sync",
	}

	resumed, err := client.ResumeRun(ctx, agentName, run.ID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to resume run: %w", err)
	}
	return resumed, nil
}

// printRunOutput prints the final output of a completed or failed run.
func printRunOutput(run *acp.RunObject) {
	fmt.Printf("Run ID:  %s\n", run.ID)
	fmt.Printf("Status:  %s\n", run.Status)

	if run.Error != nil {
		fmt.Printf("Error:   [%s] %s\n", run.Error.Code, run.Error.Message)
	}

	for _, msg := range run.Output {
		if msg.Role == "agent" {
			for _, part := range msg.Parts {
				if part.ContentType == "text/plain" || part.ContentType == "" {
					fmt.Println()
					fmt.Println(part.Content)
				}
			}
		}
	}
}

var acpRunsGetCmd = &cobra.Command{
	Use:   "get <agent-name> <run-id>",
	Short: "Get an ACP run by ID",
	Long: `Get the state of a specific ACP run.

Displays run status, output messages, and timing. If the run is paused
(input-required), shows the pending question. Use --json for raw JSON output.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		runID := args[1]

		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		run, err := client.GetRun(ctx, agentName, runID)
		if err != nil {
			if sdkerrors.IsNotFound(err) {
				return fmt.Errorf("Run '%s' not found for agent '%s'", runID, agentName)
			}
			return fmt.Errorf("failed to get run: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(run)
		}

		fmt.Printf("Run ID:     %s\n", run.ID)
		fmt.Printf("Agent:      %s\n", run.AgentName)
		fmt.Printf("Status:     %s\n", run.Status)
		fmt.Printf("Created:    %s\n", run.CreatedAt.Format("2006-01-02 15:04:05"))
		if run.UpdatedAt != nil {
			fmt.Printf("Updated:    %s\n", run.UpdatedAt.Format("2006-01-02 15:04:05"))
		}
		if run.SessionID != nil {
			fmt.Printf("Session ID: %s\n", *run.SessionID)
		}

		if run.Error != nil {
			fmt.Printf("Error:      [%s] %s\n", run.Error.Code, run.Error.Message)
		}

		if run.AwaitRequest != nil {
			fmt.Println()
			fmt.Printf("Pending question: %s\n", run.AwaitRequest.Question)
			if len(run.AwaitRequest.Options) > 0 {
				for i, opt := range run.AwaitRequest.Options {
					fmt.Printf("  %d) %s\n", i+1, opt.Label)
				}
			}
		}

		for _, msg := range run.Output {
			if msg.Role == "agent" {
				for _, part := range msg.Parts {
					if part.ContentType == "text/plain" || part.ContentType == "" {
						fmt.Println()
						fmt.Println(part.Content)
					}
				}
			}
		}

		return nil
	},
}

var acpRunsCancelCmd = &cobra.Command{
	Use:   "cancel <agent-name> <run-id>",
	Short: "Cancel an ACP run",
	Long: `Cancel a running or queued ACP run.

Requests cancellation of the specified run. The run transitions to
'cancelling' and eventually 'cancelled'.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		runID := args[1]

		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		run, err := client.CancelRun(ctx, agentName, runID)
		if err != nil {
			if sdkerrors.IsConflict(err) {
				return fmt.Errorf("Cannot cancel run %s: run has already completed", runID)
			}
			return fmt.Errorf("failed to cancel run: %w", err)
		}

		fmt.Printf("Run %s cancellation requested (status: %s)\n", run.ID, run.Status)
		return nil
	},
}

var acpRunsResumeCmd = &cobra.Command{
	Use:   "resume <agent-name> <run-id>",
	Short: "Resume a paused ACP run",
	Long: `Resume an ACP run that is waiting for human input (status: input-required).

Requires --message with the response to the agent's question.
Use --mode to control execution mode (sync, async, stream).`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := args[0]
		runID := args[1]

		if acpResumeMsg == "" {
			return fmt.Errorf("--message flag is required")
		}

		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		req := acp.ResumeRunRequest{
			Message: []acp.MessagePart{
				{ContentType: "text/plain", Content: acpResumeMsg},
			},
			Mode: acpResumeMode,
		}

		// Stream mode
		if acpResumeMode == "stream" {
			return streamResume(ctx, client, agentName, runID, req)
		}

		run, err := client.ResumeRun(ctx, agentName, runID, req)
		if err != nil {
			if sdkerrors.IsConflict(err) {
				return fmt.Errorf("Cannot resume run %s: run is not awaiting input", runID)
			}
			return fmt.Errorf("failed to resume run: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(run)
		}

		printRunOutput(run)
		return nil
	},
}

// ── memory acp sessions ─────────────────────────────────────────────────────

var acpSessionAgentFlag string

var acpSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage ACP sessions",
	Long:  "Create and inspect ACP sessions for grouping related agent runs.",
}

var acpSessionsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new ACP session",
	Long: `Create a new ACP session.

Sessions group related agent runs together. Use --agent to scope the
session to a specific agent.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		req := acp.CreateSessionRequest{}
		if acpSessionAgentFlag != "" {
			req.AgentName = &acpSessionAgentFlag
		}

		session, err := client.CreateSession(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(session)
		}

		fmt.Printf("Session ID: %s\n", session.ID)
		if session.AgentName != nil {
			fmt.Printf("Agent:      %s\n", *session.AgentName)
		}
		return nil
	},
}

var acpSessionsGetCmd = &cobra.Command{
	Use:   "get <session-id>",
	Short: "Get an ACP session",
	Long: `Get details of an ACP session including its run history.

Use --json for raw JSON output.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]

		client, err := getACPClient(cmd)
		if err != nil {
			return err
		}
		ctx := context.Background()

		session, err := client.GetSession(ctx, sessionID)
		if err != nil {
			if sdkerrors.IsNotFound(err) {
				return fmt.Errorf("Session '%s' not found", sessionID)
			}
			return fmt.Errorf("failed to get session: %w", err)
		}

		if output == "json" {
			return acpPrintJSON(session)
		}

		fmt.Printf("Session ID: %s\n", session.ID)
		if session.AgentName != nil {
			fmt.Printf("Agent:      %s\n", *session.AgentName)
		}
		fmt.Printf("Created:    %s\n", session.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("Updated:    %s\n", session.UpdatedAt.Format("2006-01-02 15:04:05"))

		if len(session.History) == 0 {
			fmt.Println("\nNo runs in this session")
			return nil
		}

		fmt.Printf("\nRuns (%d):\n", len(session.History))
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "  RUN ID\tAGENT\tSTATUS\tCREATED")
		for _, r := range session.History {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
				r.ID, r.AgentName, r.Status,
				r.CreatedAt.Format("2006-01-02 15:04:05"))
		}
		return w.Flush()
	},
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// acpPrintJSON encodes v as indented JSON to stdout.
func acpPrintJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ── Registration ─────────────────────────────────────────────────────────────

func init() {
	// Agents subcommands
	acpAgentsCmd.AddCommand(acpAgentsListCmd)
	acpAgentsCmd.AddCommand(acpAgentsGetCmd)

	// Runs subcommands and flags
	acpRunsCreateCmd.Flags().StringVar(&acpRunMessage, "message", "", "Input message for the agent (required)")
	acpRunsCreateCmd.Flags().StringVar(&acpRunMode, "mode", "sync", "Execution mode: sync, async, stream")
	acpRunsCreateCmd.Flags().StringVar(&acpRunSessionID, "session", "", "Link run to an existing ACP session ID")

	acpRunsGetCmd.Flags()    // no additional flags beyond --json (global)
	acpRunsCancelCmd.Flags() // no additional flags

	acpRunsResumeCmd.Flags().StringVar(&acpResumeMsg, "message", "", "Response message (required)")
	acpRunsResumeCmd.Flags().StringVar(&acpResumeMode, "mode", "sync", "Execution mode: sync, async, stream")

	acpRunsCmd.AddCommand(acpRunsCreateCmd)
	acpRunsCmd.AddCommand(acpRunsGetCmd)
	acpRunsCmd.AddCommand(acpRunsCancelCmd)
	acpRunsCmd.AddCommand(acpRunsResumeCmd)

	// Sessions subcommands and flags
	acpSessionsCreateCmd.Flags().StringVar(&acpSessionAgentFlag, "agent", "", "Scope session to a specific agent")
	acpSessionsCmd.AddCommand(acpSessionsCreateCmd)
	acpSessionsCmd.AddCommand(acpSessionsGetCmd)

	// Register top-level subcommands
	acpCmd.AddCommand(acpPingCmd)
	acpCmd.AddCommand(acpAgentsCmd)
	acpCmd.AddCommand(acpRunsCmd)
	acpCmd.AddCommand(acpSessionsCmd)

	// Register acp command on root
	rootCmd.AddCommand(acpCmd)
}
