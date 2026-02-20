package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agents"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage runtime agents",
	Long:  "Commands for managing runtime agents (scheduling, triggers, execution state)",
}

var listAgentsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agents",
	Long:  "List all agents for the current project",
	RunE:  runListAgents,
}

var getAgentCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get agent details",
	Long:  "Get details for a specific agent by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetAgent,
}

var createAgentCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent",
	Long: `Create a new runtime agent for the current project.

Examples:
  emergent-cli agents create --name "my-agent" --project-id <id>
  emergent-cli agents create --name "cron-agent" --trigger-type schedule --cron "0 */5 * * * *"
  emergent-cli agents create --name "reaction-agent" --trigger-type reaction --reaction-events created,updated --reaction-object-types document`,
	RunE: runCreateAgent,
}

var updateAgentCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an agent",
	Long:  "Update an existing agent (partial update)",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdateAgent,
}

var deleteAgentCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete an agent",
	Long:  "Delete an agent by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeleteAgent,
}

var triggerAgentCmd = &cobra.Command{
	Use:   "trigger [id]",
	Short: "Trigger an agent run",
	Long:  "Trigger an immediate run of an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runTriggerAgent,
}

var runsAgentCmd = &cobra.Command{
	Use:   "runs [id]",
	Short: "List agent runs",
	Long:  "List recent runs for an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetAgentRuns,
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
)

func runListAgents(cmd *cobra.Command, args []string) error {
	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	result, err := c.SDK.Agents.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	if len(result.Data) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	fmt.Printf("Found %d agent(s):\n\n", len(result.Data))
	for i, a := range result.Data {
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
	agentID := args[0]

	c, err := getClient(cmd)
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

	projectID, err := resolveAgentProjectID(cmd)
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
	agentID := args[0]

	c, err := getClient(cmd)
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
	agentID := args[0]

	c, err := getClient(cmd)
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
	agentID := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	result, err := c.SDK.Agents.Trigger(context.Background(), agentID)
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
	agentID := args[0]

	c, err := getClient(cmd)
	if err != nil {
		return err
	}

	result, err := c.SDK.Agents.GetRuns(context.Background(), agentID, agentRunsLimit)
	if err != nil {
		return fmt.Errorf("failed to get agent runs: %w", err)
	}

	if len(result.Data) == 0 {
		fmt.Println("No runs found for this agent.")
		return nil
	}

	fmt.Printf("Found %d run(s):\n\n", len(result.Data))
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

var questionsCmd = &cobra.Command{
	Use:   "questions",
	Short: "Manage agent questions",
	Long:  "Commands for listing and responding to agent questions",
}

var listQuestionsCmd = &cobra.Command{
	Use:   "list [run-id]",
	Short: "List questions for a run",
	Long:  "List all questions for a specific agent run",
	Args:  cobra.ExactArgs(1),
	RunE:  runListQuestions,
}

var listProjectQuestionsCmd = &cobra.Command{
	Use:   "list-project",
	Short: "List questions for a project",
	Long:  "List all questions for a project with optional status filter",
	RunE:  runListProjectQuestions,
}

var respondToQuestionCmd = &cobra.Command{
	Use:   "respond [question-id] [response]",
	Short: "Respond to a question",
	Long:  "Respond to a pending agent question and resume the paused run",
	Args:  cobra.ExactArgs(2),
	RunE:  runRespondToQuestion,
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
	Long:  "List all webhook hooks configured for an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runListHooks,
}

var createHookCmd = &cobra.Command{
	Use:   "create [agent-id]",
	Short: "Create a webhook hook",
	Long: `Create a new webhook hook for an agent. The plaintext token is only shown once.

Examples:
  emergent-cli agents hooks create <agent-id> --label "CI/CD Pipeline"
  emergent-cli agents hooks create <agent-id> --label "Staging" --rate-limit 30 --burst-size 5`,
	Args: cobra.ExactArgs(1),
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
	agentID := args[0]

	c, err := getClient(cmd)
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
	agentID := args[0]

	if hookLabel == "" {
		return fmt.Errorf("hook label is required. Use --label flag")
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
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
	projectID, err := resolveAgentProjectID(cmd)
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

	projectID, err := resolveAgentProjectID(cmd)
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

	projectID, err := resolveAgentProjectID(cmd)
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

// resolveAgentProjectID resolves the project ID from --project-id flag or config.
// Accepts both project names and IDs.
func resolveAgentProjectID(cmd *cobra.Command) (string, error) {
	if agentProjectID != "" {
		if isUUID(agentProjectID) {
			return agentProjectID, nil
		}
		// Resolve name to ID
		c, err := getClient(cmd)
		if err != nil {
			return "", err
		}
		return resolveProjectNameOrID(c, agentProjectID)
	}
	return resolveProjectID(cmd)
}

func init() {
	// Persistent flags for all agent subcommands
	agentsCmd.PersistentFlags().StringVar(&agentProjectID, "project-id", "", "Project name or ID (auto-detected from config/env if not specified)")

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

	// Runs limit flag
	runsAgentCmd.Flags().IntVar(&agentRunsLimit, "limit", 10, "Maximum number of runs to return")

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
	agentsCmd.AddCommand(triggerAgentCmd)
	agentsCmd.AddCommand(runsAgentCmd)
	agentsCmd.AddCommand(questionsCmd)
	agentsCmd.AddCommand(hooksCmd)
	rootCmd.AddCommand(agentsCmd)
}
