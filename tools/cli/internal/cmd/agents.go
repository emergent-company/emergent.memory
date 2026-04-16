package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agents"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:     "agents",
	Short:   "Manage runtime agents",
	Long:    "Commands for managing runtime agents (scheduling, triggers, execution state)",
	GroupID: "ai",
}

var listAgentsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents",
	Long: `List all agents configured for the current project.

Prints a numbered list with each agent's Name, ID, Enabled status, Trigger
Type, Cron schedule (if any), Description (if set), Last Run timestamp, and
Last Run Status. Use --project to specify a project other than the active one.`,
	RunE: runListAgents,
}

var getAgentCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get agent details",
	Long: `Get full details for a specific agent by its ID.

Prints Name, ID, Project ID, Strategy Type, Enabled status, Trigger Type,
Execution Mode, Cron Schedule (if set), Description (if set), Prompt (if set),
Reaction Config (Object Types and Events), Last Run At, Last Run Status,
Created At, Updated At, and any extra Config JSON.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGetAgent,
}

var createAgentCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent",
	Long: `Create a new runtime agent for the current project.

Examples:
  memory agents create --name "my-agent" --project <id>
  memory agents create --name "cron-agent" --trigger-type schedule --cron "0 */5 * * * *"
  memory agents create --name "reaction-agent" --trigger-type reaction --reaction-events created,updated --reaction-object-types document`,
	RunE: runCreateAgent,
}

var updateAgentCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an agent",
	Long:  "Update an existing agent (partial update)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runUpdateAgent,
}

var deleteAgentCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete an agent",
	Long: `Delete an agent by ID.

Prints "Agent <id> deleted successfully." on success.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDeleteAgent,
}

var triggerAgentCmd = &cobra.Command{
	Use:   "trigger [id]",
	Short: "Trigger an agent run",
	Long: `Trigger an immediate run of an agent.

Prints "Agent triggered successfully!" with an optional message on success, or
"Agent trigger failed." with an error message on failure.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTriggerAgent,
}

var runsAgentCmd = &cobra.Command{
	Use:   "runs [id]",
	Short: "List agent runs",
	Long: `List recent runs for an agent. Each run entry shows:
  - Run ID and status (running, completed, failed)
  - Start time and duration
  - Token usage: input tokens / output tokens
  - Estimated cost in USD (e.g. "Cost: $0.001234")

Use --limit to control how many runs are returned (default 10).
Use "memory agents get-run [run-id]" to get the full breakdown for a specific run.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGetAgentRuns,
}

var getRunCmd = &cobra.Command{
	Use:   "get-run [run-id]",
	Short: "Get details for a specific run",
	Long: `Get full details for a specific agent run by its run ID. Output includes:
  - Run ID, agent ID, status, start/end times
  - Token usage: total input tokens, total output tokens
  - Estimated cost in USD
  - Root run ID (for sub-runs triggered by a parent run)
  - Any output or error message from the run

No --project flag is required — run IDs are globally unique.

This is the primary command to check the cost of a specific agent run.`,
	Args: cobra.ExactArgs(1),
	RunE: runGetRunByID,
}

// Flags for agents
var (
	agentProjectID        string
	agentName             string
	agentStrategyType     string
	agentPrompt           string
	agentCronSchedule     string
	agentEnabled          string
	agentTriggerType      string
	agentExecutionMode    string
	agentDescription      string
	agentReactionEvents   string
	agentReactionObjTypes string
	agentRunsLimit        int
	getRunJSONOutput      bool
	agentListLimit        int
	agentListPage         int
	triggerInputFlag      string
	triggerModelFlag      string
	triggerEnvVarsFlag    []string
)

// resolveAgentArgOrPick resolves an agent ID from args[0], or, when args is
// empty and stdin is a terminal, lists agents in the current project and shows
// an interactive picker. Returns the resolved agent ID.
func resolveAgentArgOrPick(cmd *cobra.Command, c *client.Client, args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	if isNonInteractive() {
		return "", fmt.Errorf("agent ID is required — pass an ID or run interactively to pick from a list")
	}

	result, err := c.SDK.Agents.List(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to list agents: %w", err)
	}
	agentList := result.Data
	if len(agentList) == 0 {
		return "", fmt.Errorf("no agents found in the current project")
	}

	items := make([]PickerItem, len(agentList))
	for i, a := range agentList {
		label := a.Name
		if a.Description != nil && *a.Description != "" {
			desc := *a.Description
			if len(desc) > 55 {
				desc = desc[:52] + "…"
			}
			label = a.Name + "  " + desc
		}
		items[i] = PickerItem{ID: a.ID, Name: label}
	}

	id, _, err := promptResourcePicker("Select an agent", items)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("agent ID is required")
	}
	return id, nil
}

func runListAgents(cmd *cobra.Command, args []string) error {
	// Resolve project first — this triggers the interactive picker when no
	// project is configured and the terminal is interactive.
	projectID, err := resolveProjectContext(cmd, agentProjectID)
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.Agents.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	if len(result.Data) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	total := len(result.Data)
	data := paginate(result.Data, agentListLimit, agentListPage)

	if compact {
		for _, a := range data {
			fmt.Printf("%-40s  %s\n", a.Name, a.ID)
		}
		return nil
	}

	if h := paginationHeader(total, agentListLimit, agentListPage); h != "" {
		fmt.Printf("%s:\n\n", h)
	} else {
		fmt.Printf("Found %d agent(s):\n\n", total)
	}
	for i, a := range data {
		fmt.Printf("%d. %s\n", i+1, a.Name)
		fmt.Printf("   ID:           %s\n", a.ID)
		fmt.Printf("   Enabled:      %v\n", a.Enabled)
		fmt.Printf("   Trigger Type: %s\n", a.TriggerType)
		if a.CronSchedule != "" {
			fmt.Printf("   Cron:         %s\n", a.CronSchedule)
		}
		if a.Description != nil && *a.Description != "" {
			fmt.Printf("   Description:  %s\n", *a.Description)
		}
		if a.LastRunAt != nil {
			fmt.Printf("   Last Run:     %s\n", a.LastRunAt.Format("2006-01-02 15:04:05"))
		}
		if a.LastRunStatus != nil {
			fmt.Printf("   Last Status:  %s\n", *a.LastRunStatus)
		}
		fmt.Println()
	}

	return nil
}

func runGetAgent(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	result, err := c.SDK.Agents.Get(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}

	a := result.Data
	fmt.Printf("Agent: %s\n", a.Name)
	fmt.Printf("  ID:             %s\n", a.ID)
	fmt.Printf("  Project ID:     %s\n", a.ProjectID)
	fmt.Printf("  Strategy Type:  %s\n", a.StrategyType)
	fmt.Printf("  Enabled:        %v\n", a.Enabled)
	fmt.Printf("  Trigger Type:   %s\n", a.TriggerType)
	fmt.Printf("  Execution Mode: %s\n", a.ExecutionMode)
	if a.CronSchedule != "" {
		fmt.Printf("  Cron Schedule:  %s\n", a.CronSchedule)
	}
	if a.Description != nil && *a.Description != "" {
		fmt.Printf("  Description:    %s\n", *a.Description)
	}
	if a.Prompt != nil && *a.Prompt != "" {
		fmt.Printf("  Prompt:         %s\n", *a.Prompt)
	}
	if a.ReactionConfig != nil {
		fmt.Printf("  Reaction Config:\n")
		if len(a.ReactionConfig.ObjectTypes) > 0 {
			fmt.Printf("    Object Types: %s\n", strings.Join(a.ReactionConfig.ObjectTypes, ", "))
		}
		if len(a.ReactionConfig.Events) > 0 {
			fmt.Printf("    Events:       %s\n", strings.Join(a.ReactionConfig.Events, ", "))
		}
	}
	if a.LastRunAt != nil {
		fmt.Printf("  Last Run At:    %s\n", a.LastRunAt.Format("2006-01-02 15:04:05"))
	}
	if a.LastRunStatus != nil {
		fmt.Printf("  Last Run Status: %s\n", *a.LastRunStatus)
	}
	fmt.Printf("  Created At:     %s\n", a.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Updated At:     %s\n", a.UpdatedAt.Format("2006-01-02 15:04:05"))

	if len(a.Config) > 0 {
		configJSON, _ := json.MarshalIndent(a.Config, "  ", "  ")
		fmt.Printf("  Config:         %s\n", string(configJSON))
	}

	return nil
}

func runCreateAgent(cmd *cobra.Command, args []string) error {
	if agentName == "" {
		return fmt.Errorf("agent name is required. Use --name flag")
	}

	projectID, err := resolveProjectContext(cmd, agentProjectID)
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	createReq := &agents.CreateAgentRequest{
		ProjectID: projectID,
		Name:      agentName,
	}

	if agentStrategyType != "" {
		createReq.StrategyType = agentStrategyType
	}
	if agentPrompt != "" {
		createReq.Prompt = &agentPrompt
	}
	if agentCronSchedule != "" {
		createReq.CronSchedule = agentCronSchedule
	}
	if agentTriggerType != "" {
		createReq.TriggerType = agentTriggerType
	}
	if agentExecutionMode != "" {
		createReq.ExecutionMode = agentExecutionMode
	}
	if agentDescription != "" {
		createReq.Description = &agentDescription
	}
	if agentEnabled != "" {
		val := agentEnabled == "true"
		createReq.Enabled = &val
	}
	if agentReactionEvents != "" || agentReactionObjTypes != "" {
		createReq.ReactionConfig = &agents.ReactionConfig{}
		if agentReactionEvents != "" {
			createReq.ReactionConfig.Events = strings.Split(agentReactionEvents, ",")
		}
		if agentReactionObjTypes != "" {
			createReq.ReactionConfig.ObjectTypes = strings.Split(agentReactionObjTypes, ",")
		}
	}

	result, err := c.SDK.Agents.Create(context.Background(), createReq)
	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	a := result.Data
	fmt.Println("Agent created successfully!")
	fmt.Printf("  ID:   %s\n", a.ID)
	fmt.Printf("  Name: %s\n", a.Name)

	return nil
}

func runUpdateAgent(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	updateReq := &agents.UpdateAgentRequest{}
	hasUpdate := false

	if cmd.Flags().Changed("name") {
		updateReq.Name = &agentName
		hasUpdate = true
	}
	if cmd.Flags().Changed("prompt") {
		updateReq.Prompt = &agentPrompt
		hasUpdate = true
	}
	if cmd.Flags().Changed("cron") {
		updateReq.CronSchedule = &agentCronSchedule
		hasUpdate = true
	}
	if cmd.Flags().Changed("trigger-type") {
		updateReq.TriggerType = &agentTriggerType
		hasUpdate = true
	}
	if cmd.Flags().Changed("execution-mode") {
		updateReq.ExecutionMode = &agentExecutionMode
		hasUpdate = true
	}
	if cmd.Flags().Changed("description") {
		updateReq.Description = &agentDescription
		hasUpdate = true
	}
	if cmd.Flags().Changed("enabled") {
		val := agentEnabled == "true"
		updateReq.Enabled = &val
		hasUpdate = true
	}

	if !hasUpdate {
		return fmt.Errorf("no update flags specified. Use --name, --enabled, --cron, --trigger-type, etc.")
	}

	result, err := c.SDK.Agents.Update(context.Background(), agentID, updateReq)
	if err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	a := result.Data
	fmt.Println("Agent updated successfully!")
	fmt.Printf("  ID:      %s\n", a.ID)
	fmt.Printf("  Name:    %s\n", a.Name)
	fmt.Printf("  Enabled: %v\n", a.Enabled)

	return nil
}

func runDeleteAgent(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	err = c.SDK.Agents.Delete(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	fmt.Printf("Agent %s deleted successfully.\n", agentID)
	return nil
}

func runTriggerAgent(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	// Parse --env KEY=VALUE flags into a map
	var envVars map[string]string
	if len(triggerEnvVarsFlag) > 0 {
		envVars = make(map[string]string, len(triggerEnvVarsFlag))
		for _, kv := range triggerEnvVarsFlag {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 || parts[0] == "" {
				return fmt.Errorf("invalid --env value %q: expected KEY=VALUE format", kv)
			}
			envVars[parts[0]] = parts[1]
		}
	}

	var result *agents.TriggerResponse
	if triggerInputFlag != "" || triggerModelFlag != "" || len(envVars) > 0 {
		result, err = c.SDK.Agents.TriggerWithInput(context.Background(), agentID, agents.TriggerRequest{
			Input:   triggerInputFlag,
			Model:   triggerModelFlag,
			EnvVars: envVars,
		})
	} else {
		result, err = c.SDK.Agents.Trigger(context.Background(), agentID)
	}
	if err != nil {
		return fmt.Errorf("failed to trigger agent: %w", err)
	}

	if result.Success {
		fmt.Println("Agent triggered successfully!")
		if result.Message != nil {
			fmt.Printf("  %s\n", *result.Message)
		}
	} else {
		fmt.Println("Agent trigger failed.")
		if result.Error != nil {
			fmt.Printf("  Error: %s\n", *result.Error)
		}
	}

	return nil
}

func runGetAgentRuns(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	// Fetch agent name for a helpful header (best effort).
	agentLabel := agentID
	if a, err := c.SDK.Agents.Get(context.Background(), agentID); err == nil && a != nil {
		agentLabel = fmt.Sprintf("%s (%s)", a.Data.Name, agentID)
	}

	// Resolve project ID so we can both call GetRuns and fetch token usage per run.
	// Try --project flag first, then fall back to looking it up from the run IDs.
	projectID, _ := resolveProjectContext(cmd, agentProjectID)
	if projectID == "" {
		// Best-effort: derive project from the agent via the admin endpoint.
		projectID, _ = fetchProjectIDFromRunID(cmd, agentID)
	}
	if projectID != "" {
		c.SDK.Agents.SetContext("", projectID)
	}

	result, err := c.SDK.Agents.GetRuns(context.Background(), agentID, agentRunsLimit)
	if err != nil {
		return fmt.Errorf("failed to get agent runs: %w", err)
	}

	if len(result.Data) == 0 {
		fmt.Printf("No runs found for agent %s.\n", agentLabel)
		return nil
	}

	// Concurrently fetch token usage for each run (best effort).
	type usageResult struct {
		idx   int
		usage *runTokenUsage
	}
	usageCh := make(chan usageResult, len(result.Data))
	if projectID != "" {
		var wg sync.WaitGroup
		for i, r := range result.Data {
			wg.Add(1)
			go func(idx int, runID string) {
				defer wg.Done()
				info := fetchRunInfo(cmd, projectID, runID)
				if info != nil {
					usageCh <- usageResult{idx: idx, usage: info.Usage}
				} else {
					usageCh <- usageResult{idx: idx, usage: nil}
				}
			}(i, r.ID)
		}
		wg.Wait()
	}
	close(usageCh)

	usageByIdx := make(map[int]*runTokenUsage, len(result.Data))
	for ur := range usageCh {
		usageByIdx[ur.idx] = ur.usage
	}

	fmt.Printf("Runs for agent: %s\n\n", agentLabel)
	for i, r := range result.Data {
		fmt.Printf("%d. Run %s\n", i+1, r.ID)
		fmt.Printf("   Status:    %s\n", r.Status)
		fmt.Printf("   Started:   %s\n", r.StartedAt.Format("2006-01-02 15:04:05"))
		if r.CompletedAt != nil {
			fmt.Printf("   Completed: %s\n", r.CompletedAt.Format("2006-01-02 15:04:05"))
		}
		if r.DurationMs != nil {
			fmt.Printf("   Duration:  %dms\n", *r.DurationMs)
		}
		if u := usageByIdx[i]; u != nil {
			fmt.Printf("   Tokens:    %d in / %d out\n", u.TotalInputTokens, u.TotalOutputTokens)
			fmt.Printf("   Cost:      $%.6f\n", u.EstimatedCostUSD)
		}
		if r.TraceID != nil && *r.TraceID != "" {
			fmt.Printf("   Trace:     %s\n", *r.TraceID)
		}
		if r.RootRunID != nil && *r.RootRunID != "" && *r.RootRunID != r.ID {
			fmt.Printf("   Root Run:  %s\n", *r.RootRunID)
		}
		if r.ErrorMessage != nil {
			fmt.Printf("   Error:     %s\n", *r.ErrorMessage)
		}
		if r.SkipReason != nil {
			fmt.Printf("   Skipped:   %s\n", *r.SkipReason)
		}
		fmt.Println()
	}

	return nil
}

func runGetRunByID(cmd *cobra.Command, args []string) error {
	runID := args[0]

	c, clientErr := getClient(cmd)
	if clientErr != nil {
		return clientErr
	}

	// Use the global endpoint — run IDs are globally unique UUIDs, no project needed.
	result, runErr := c.SDK.Agents.GetRunByID(context.Background(), runID)
	if runErr != nil {
		return fmt.Errorf("failed to get run: %w", runErr)
	}

	r := result.Data

	// AgentName is now returned by the server in the global run response.
	// Fall back to the ID if the server is older and doesn't populate it.
	agentDisplayName := r.AgentID
	if r.AgentName != "" {
		agentDisplayName = fmt.Sprintf("%s (%s)", r.AgentName, r.AgentID)
	}

	// Consolidate token usage from the run's TokenUsage field (populated by GetRunByID).
	var usage *runTokenUsage
	if r.TokenUsage != nil {
		usage = &runTokenUsage{
			TotalInputTokens:  r.TokenUsage.TotalInputTokens,
			TotalOutputTokens: r.TokenUsage.TotalOutputTokens,
			EstimatedCostUSD:  r.TokenUsage.EstimatedCostUSD,
		}
	}

	if getRunJSONOutput {
		type jsonOut struct {
			ID           string         `json:"id"`
			AgentID      string         `json:"agentId"`
			AgentName    string         `json:"agentName,omitempty"`
			Status       string         `json:"status"`
			StartedAt    string         `json:"startedAt"`
			CompletedAt  string         `json:"completedAt,omitempty"`
			DurationMs   *int64         `json:"durationMs,omitempty"`
			TokenUsage   *runTokenUsage `json:"tokenUsage,omitempty"`
			TraceID      string         `json:"traceId,omitempty"`
			RootRunID    string         `json:"rootRunId,omitempty"`
			ErrorMessage string         `json:"errorMessage,omitempty"`
			SkipReason   string         `json:"skipReason,omitempty"`
		}
		out := jsonOut{
			ID:        r.ID,
			AgentID:   r.AgentID,
			AgentName: r.AgentName,
			Status:    r.Status,
			StartedAt: r.StartedAt.Format(time.RFC3339),
		}
		if r.CompletedAt != nil {
			out.CompletedAt = r.CompletedAt.Format(time.RFC3339)
		}
		if r.DurationMs != nil {
			ms := int64(*r.DurationMs)
			out.DurationMs = &ms
		}
		if usage != nil {
			out.TokenUsage = usage
		}
		if r.TraceID != nil {
			out.TraceID = *r.TraceID
		}
		if r.RootRunID != nil && *r.RootRunID != r.ID {
			out.RootRunID = *r.RootRunID
		}
		if r.ErrorMessage != nil {
			out.ErrorMessage = *r.ErrorMessage
		}
		if r.SkipReason != nil {
			out.SkipReason = *r.SkipReason
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Printf("Run: %s\n", r.ID)
	fmt.Printf("  Agent:     %s\n", agentDisplayName)
	fmt.Printf("  Status:    %s\n", r.Status)
	fmt.Printf("  Started:   %s\n", r.StartedAt.Format("2006-01-02 15:04:05"))
	if r.CompletedAt != nil {
		fmt.Printf("  Completed: %s\n", r.CompletedAt.Format("2006-01-02 15:04:05"))
	}
	if r.DurationMs != nil {
		fmt.Printf("  Duration:  %dms\n", *r.DurationMs)
	}
	if usage != nil {
		fmt.Printf("  Tokens:    %d in / %d out\n", usage.TotalInputTokens, usage.TotalOutputTokens)
		fmt.Printf("  Cost:      $%.6f\n", usage.EstimatedCostUSD)
	}
	if r.TraceID != nil && *r.TraceID != "" {
		fmt.Printf("  Trace:     %s\n", *r.TraceID)
	}
	if r.RootRunID != nil && *r.RootRunID != "" && *r.RootRunID != r.ID {
		fmt.Printf("  Root Run:  %s\n", *r.RootRunID)
	}
	if r.ErrorMessage != nil {
		fmt.Printf("  Error:     %s\n", *r.ErrorMessage)
	}
	if r.SkipReason != nil {
		fmt.Printf("  Skipped:   %s\n", *r.SkipReason)
	}

	return nil
}

var questionsCmd = &cobra.Command{
	Use:   "questions",
	Short: "Manage agent questions",
	Long:  "Commands for listing and responding to agent questions",
}

var listQuestionsCmd = &cobra.Command{
	Use:   "list [run-id]",
	Short: "List questions for a run",
	Long: `List all questions asked by the agent during a specific run.

Outputs the full question list as indented JSON, including each question's ID,
status, prompt text, and response (if already answered).`,
	Args: cobra.ExactArgs(1),
	RunE: runListQuestions,
}

var listProjectQuestionsCmd = &cobra.Command{
	Use:   "list-project",
	Short: "List questions for a project",
	Long: `List all agent questions for the current project.

Outputs the full question list as indented JSON. Use --status to filter by
question status (e.g. pending, answered).`,
	RunE: runListProjectQuestions,
}

var respondToQuestionCmd = &cobra.Command{
	Use:   "respond [question-id] [response]",
	Short: "Respond to a question",
	Long: `Respond to a pending agent question and resume the paused agent run.

Sends the response text as the answer to the specified question. Outputs the
updated question record as indented JSON on success.`,
	Args: cobra.ExactArgs(2),
	RunE: runRespondToQuestion,
}

// Flags for questions
var (
	questionStatus string
)

// --- Webhook Hooks Commands ---

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage agent webhook hooks",
	Long:  "Commands for managing webhook hooks on agents (create, list, delete)",
}

var listHooksCmd = &cobra.Command{
	Use:   "list [agent-id]",
	Short: "List webhook hooks",
	Long: `List all webhook hooks configured for an agent.

Prints a numbered list with each hook's Label, ID, Enabled status, Rate Limit
configuration (requests/minute and burst size, if set), and Created timestamp.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runListHooks,
}

var createHookCmd = &cobra.Command{
	Use:   "create [agent-id]",
	Short: "Create a webhook hook",
	Long: `Create a new webhook hook for an agent. The plaintext token is only shown once.

Examples:
  memory agents hooks create <agent-id> --label "CI/CD Pipeline"
  memory agents hooks create <agent-id> --label "Staging" --rate-limit 30 --burst-size 5`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCreateHook,
}

var deleteHookCmd = &cobra.Command{
	Use:   "delete [agent-id] [hook-id]",
	Short: "Delete a webhook hook",
	Long:  "Delete a webhook hook from an agent",
	Args:  cobra.ExactArgs(2),
	RunE:  runDeleteHook,
}

// Flags for hooks
var (
	hookLabel     string
	hookRateLimit int
	hookBurstSize int
)

func runListHooks(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	result, err := c.SDK.Agents.ListWebhookHooks(context.Background(), agentID)
	if err != nil {
		return fmt.Errorf("failed to list webhook hooks: %w", err)
	}

	hooks := result.Data
	if len(hooks) == 0 {
		fmt.Println("No webhook hooks found for this agent.")
		return nil
	}

	fmt.Printf("Found %d webhook hook(s):\n\n", len(hooks))
	for i, h := range hooks {
		fmt.Printf("%d. %s\n", i+1, h.Label)
		fmt.Printf("   ID:        %s\n", h.ID)
		fmt.Printf("   Enabled:   %v\n", h.Enabled)
		if h.RateLimitConfig != nil {
			fmt.Printf("   Rate Limit: %d req/min (burst: %d)\n", h.RateLimitConfig.RequestsPerMinute, h.RateLimitConfig.BurstSize)
		}
		fmt.Printf("   Created:   %s\n", h.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}

	return nil
}

func runCreateHook(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	agentID, err := resolveAgentArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	if hookLabel == "" {
		return fmt.Errorf("hook label is required. Use --label flag")
	}

	createReq := &agents.CreateWebhookHookRequest{
		Label: hookLabel,
	}

	if hookRateLimit > 0 || hookBurstSize > 0 {
		createReq.RateLimitConfig = &agents.RateLimitConfig{
			RequestsPerMinute: hookRateLimit,
			BurstSize:         hookBurstSize,
		}
	}

	result, err := c.SDK.Agents.CreateWebhookHook(context.Background(), agentID, createReq)
	if err != nil {
		return fmt.Errorf("failed to create webhook hook: %w", err)
	}

	h := result.Data
	fmt.Println("Webhook hook created successfully!")
	fmt.Printf("  ID:    %s\n", h.ID)
	fmt.Printf("  Label: %s\n", h.Label)
	if h.Token != nil {
		fmt.Printf("\n  Token: %s\n", *h.Token)
		fmt.Println("\n  WARNING: Save this token now. It will not be shown again.")
	}

	return nil
}

func runDeleteHook(cmd *cobra.Command, args []string) error {
	agentID := args[0]
	hookID := args[1]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	err = c.SDK.Agents.DeleteWebhookHook(context.Background(), agentID, hookID)
	if err != nil {
		return fmt.Errorf("failed to delete webhook hook: %w", err)
	}

	fmt.Printf("Webhook hook %s deleted successfully.\n", hookID)
	return nil
}

func runListQuestions(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	runID := args[0]
	projectID, err := resolveProjectContext(cmd, agentProjectID)
	if err != nil {
		return fmt.Errorf("failed to resolve project ID: %w", err)
	}

	result, err := c.SDK.Agents.GetRunQuestions(context.Background(), projectID, runID)
	if err != nil {
		return fmt.Errorf("failed to list questions: %w", err)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func runListProjectQuestions(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	projectID, err := resolveProjectContext(cmd, agentProjectID)
	if err != nil {
		return fmt.Errorf("failed to resolve project ID: %w", err)
	}

	result, err := c.SDK.Agents.ListProjectQuestions(context.Background(), projectID, questionStatus)
	if err != nil {
		return fmt.Errorf("failed to list project questions: %w", err)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func runRespondToQuestion(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	questionID := args[0]
	response := args[1]

	projectID, err := resolveProjectContext(cmd, agentProjectID)
	if err != nil {
		return fmt.Errorf("failed to resolve project ID: %w", err)
	}

	req := &agents.RespondToQuestionRequest{
		Response: response,
	}

	result, err := c.SDK.Agents.RespondToQuestion(context.Background(), projectID, questionID, req)
	if err != nil {
		return fmt.Errorf("failed to respond to question: %w", err)
	}

	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

func init() {
	// Persistent flags for all agent subcommands
	agentsCmd.PersistentFlags().StringVar(&agentProjectID, "project", "", "Project name or ID (auto-detected from config/env if not specified)")

	// Create agent flags
	createAgentCmd.Flags().StringVar(&agentName, "name", "", "Agent name (required)")
	createAgentCmd.Flags().StringVar(&agentStrategyType, "strategy-type", "", "Strategy type (e.g., graph_object_processor)")
	createAgentCmd.Flags().StringVar(&agentPrompt, "prompt", "", "Agent prompt")
	createAgentCmd.Flags().StringVar(&agentCronSchedule, "cron", "", "Cron schedule (e.g., '0 */5 * * * *')")
	createAgentCmd.Flags().StringVar(&agentEnabled, "enabled", "", "Enable agent (true/false)")
	createAgentCmd.Flags().StringVar(&agentTriggerType, "trigger-type", "", "Trigger type (manual, schedule, reaction, webhook)")
	createAgentCmd.Flags().StringVar(&agentExecutionMode, "execution-mode", "", "Execution mode")
	createAgentCmd.Flags().StringVar(&agentDescription, "description", "", "Agent description")
	createAgentCmd.Flags().StringVar(&agentReactionEvents, "reaction-events", "", "Comma-separated reaction event types (e.g., created,updated)")
	createAgentCmd.Flags().StringVar(&agentReactionObjTypes, "reaction-object-types", "", "Comma-separated reaction object types (e.g., document,chunk)")
	_ = createAgentCmd.MarkFlagRequired("name")

	// Update agent flags
	updateAgentCmd.Flags().StringVar(&agentName, "name", "", "New agent name")
	updateAgentCmd.Flags().StringVar(&agentPrompt, "prompt", "", "New agent prompt")
	updateAgentCmd.Flags().StringVar(&agentCronSchedule, "cron", "", "New cron schedule")
	updateAgentCmd.Flags().StringVar(&agentEnabled, "enabled", "", "Enable/disable (true/false)")
	updateAgentCmd.Flags().StringVar(&agentTriggerType, "trigger-type", "", "New trigger type")
	updateAgentCmd.Flags().StringVar(&agentExecutionMode, "execution-mode", "", "New execution mode")
	updateAgentCmd.Flags().StringVar(&agentDescription, "description", "", "New description")

	// List pagination flags
	listAgentsCmd.Flags().IntVar(&agentListLimit, "limit", 0, "Maximum number of agents to show (0 = all)")
	listAgentsCmd.Flags().IntVar(&agentListPage, "page", 1, "Page number (1-based, used with --limit)")

	// Runs limit flag
	runsAgentCmd.Flags().IntVar(&agentRunsLimit, "limit", 10, "Maximum number of runs to return")

	// get-run flags
	getRunCmd.Flags().BoolVar(&getRunJSONOutput, "json", false, "Output result as JSON")

	// Questions flags
	listProjectQuestionsCmd.Flags().StringVar(&questionStatus, "status", "", "Filter by status (pending, answered, cancelled, expired)")

	// Register questions subcommands
	questionsCmd.AddCommand(listQuestionsCmd)
	questionsCmd.AddCommand(listProjectQuestionsCmd)
	questionsCmd.AddCommand(respondToQuestionCmd)

	// Webhook hooks flags
	createHookCmd.Flags().StringVar(&hookLabel, "label", "", "Hook label (required)")
	createHookCmd.Flags().IntVar(&hookRateLimit, "rate-limit", 0, "Rate limit in requests per minute (0 = server default)")
	createHookCmd.Flags().IntVar(&hookBurstSize, "burst-size", 0, "Burst size for rate limiting (0 = server default)")
	_ = createHookCmd.MarkFlagRequired("label")

	// Register hooks subcommands
	hooksCmd.AddCommand(listHooksCmd, createHookCmd, deleteHookCmd)

	// Register subcommands
	agentsCmd.AddCommand(listAgentsCmd)
	agentsCmd.AddCommand(getAgentCmd)
	agentsCmd.AddCommand(createAgentCmd)
	agentsCmd.AddCommand(updateAgentCmd)
	agentsCmd.AddCommand(deleteAgentCmd)

	triggerAgentCmd.Flags().StringVar(&triggerInputFlag, "input", "", "Initial message to pass to the agent at trigger time")
	triggerAgentCmd.Flags().StringVar(&triggerModelFlag, "model", "", "Override the model for this single run (e.g. claude-sonnet-4.7)")
	triggerAgentCmd.Flags().StringArrayVar(&triggerEnvVarsFlag, "env", nil, "Environment variable to inject into sandbox (KEY=VALUE, repeatable)")
	agentsCmd.AddCommand(triggerAgentCmd)

	agentsCmd.AddCommand(runsAgentCmd)
	agentsCmd.AddCommand(getRunCmd)
	agentsCmd.AddCommand(questionsCmd)
	agentsCmd.AddCommand(hooksCmd)
	rootCmd.AddCommand(agentsCmd)
}
