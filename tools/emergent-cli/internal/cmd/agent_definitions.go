package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/agentdefinitions"
	"github.com/spf13/cobra"
)

var agentDefsCmd = &cobra.Command{
	Use:     "agent-definitions",
	Aliases: []string{"agent-defs", "defs"},
	Short:   "Manage agent definitions",
	Long:    "Commands for managing agent definitions (system prompts, tools, model config, flow type, visibility)",
}

var listAgentDefsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agent definitions",
	Long:  "List all agent definitions for the current project",
	RunE:  runListAgentDefs,
}

var getAgentDefCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get agent definition details",
	Long:  "Get details for a specific agent definition by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runGetAgentDef,
}

var createAgentDefCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent definition",
	Long: `Create a new agent definition.

Examples:
  emergent-cli agent-definitions create --name "my-def" --system-prompt "You are a helpful agent"
  emergent-cli defs create --name "extractor" --flow-type single --tools "search,graph_query" --visibility project`,
	RunE: runCreateAgentDef,
}

var updateAgentDefCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an agent definition",
	Long:  "Update an existing agent definition (partial update)",
	Args:  cobra.ExactArgs(1),
	RunE:  runUpdateAgentDef,
}

var deleteAgentDefCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete an agent definition",
	Long:  "Delete an agent definition by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runDeleteAgentDef,
}

// Flags for agent definitions
var (
	defName           string
	defDescription    string
	defSystemPrompt   string
	defModelName      string
	defTools          string
	defFlowType       string
	defVisibility     string
	defIsDefault      string
	defMaxSteps       int
	defDefaultTimeout int
)

func runListAgentDefs(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.AgentDefinitions.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list agent definitions: %w", err)
	}

	if len(result.Data) == 0 {
		fmt.Println("No agent definitions found.")
		return nil
	}

	fmt.Printf("Found %d agent definition(s):\n\n", len(result.Data))
	for i, d := range result.Data {
		fmt.Printf("%d. %s\n", i+1, d.Name)
		fmt.Printf("   ID:         %s\n", d.ID)
		fmt.Printf("   Flow Type:  %s\n", d.FlowType)
		fmt.Printf("   Visibility: %s\n", d.Visibility)
		fmt.Printf("   Default:    %v\n", d.IsDefault)
		fmt.Printf("   Tools:      %d\n", d.ToolCount)
		if d.Description != nil && *d.Description != "" {
			fmt.Printf("   Description: %s\n", *d.Description)
		}
		fmt.Println()
	}

	return nil
}

func runGetAgentDef(cmd *cobra.Command, args []string) error {
	defID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.AgentDefinitions.Get(context.Background(), defID)
	if err != nil {
		return fmt.Errorf("failed to get agent definition: %w", err)
	}

	d := result.Data
	fmt.Printf("Agent Definition: %s\n", d.Name)
	fmt.Printf("  ID:              %s\n", d.ID)
	fmt.Printf("  Project ID:      %s\n", d.ProjectID)
	fmt.Printf("  Flow Type:       %s\n", d.FlowType)
	fmt.Printf("  Visibility:      %s\n", d.Visibility)
	fmt.Printf("  Default:         %v\n", d.IsDefault)
	if d.Description != nil && *d.Description != "" {
		fmt.Printf("  Description:     %s\n", *d.Description)
	}
	if d.SystemPrompt != nil && *d.SystemPrompt != "" {
		prompt := *d.SystemPrompt
		if len(prompt) > 200 {
			prompt = prompt[:200] + "..."
		}
		fmt.Printf("  System Prompt:   %s\n", prompt)
	}
	if d.Model != nil {
		fmt.Printf("  Model:\n")
		if d.Model.Name != "" {
			fmt.Printf("    Name:          %s\n", d.Model.Name)
		}
		if d.Model.Temperature != nil {
			fmt.Printf("    Temperature:   %.2f\n", *d.Model.Temperature)
		}
		if d.Model.MaxTokens != nil {
			fmt.Printf("    Max Tokens:    %d\n", *d.Model.MaxTokens)
		}
	}
	if len(d.Tools) > 0 {
		fmt.Printf("  Tools:           %s\n", strings.Join(d.Tools, ", "))
	}
	if d.MaxSteps != nil {
		fmt.Printf("  Max Steps:       %d\n", *d.MaxSteps)
	}
	if d.DefaultTimeout != nil {
		fmt.Printf("  Default Timeout: %d\n", *d.DefaultTimeout)
	}
	if d.ACPConfig != nil {
		fmt.Printf("  ACP Config:\n")
		if d.ACPConfig.DisplayName != "" {
			fmt.Printf("    Display Name:  %s\n", d.ACPConfig.DisplayName)
		}
		if d.ACPConfig.Description != "" {
			fmt.Printf("    Description:   %s\n", d.ACPConfig.Description)
		}
		if len(d.ACPConfig.Capabilities) > 0 {
			fmt.Printf("    Capabilities:  %s\n", strings.Join(d.ACPConfig.Capabilities, ", "))
		}
	}
	fmt.Printf("  Created At:      %s\n", d.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Updated At:      %s\n", d.UpdatedAt.Format("2006-01-02 15:04:05"))

	if len(d.Config) > 0 {
		configJSON, _ := json.MarshalIndent(d.Config, "  ", "  ")
		fmt.Printf("  Config:          %s\n", string(configJSON))
	}

	return nil
}

func runCreateAgentDef(cmd *cobra.Command, args []string) error {
	if defName == "" {
		return fmt.Errorf("definition name is required. Use --name flag")
	}

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	createReq := &agentdefinitions.CreateAgentDefinitionRequest{
		Name: defName,
	}

	if defDescription != "" {
		createReq.Description = &defDescription
	}
	if defSystemPrompt != "" {
		createReq.SystemPrompt = &defSystemPrompt
	}
	if defModelName != "" {
		createReq.Model = &agentdefinitions.ModelConfig{
			Name: defModelName,
		}
	}
	if defTools != "" {
		createReq.Tools = strings.Split(defTools, ",")
		for i := range createReq.Tools {
			createReq.Tools[i] = strings.TrimSpace(createReq.Tools[i])
		}
	}
	if defFlowType != "" {
		createReq.FlowType = defFlowType
	}
	if defVisibility != "" {
		createReq.Visibility = defVisibility
	}
	if defIsDefault != "" {
		val := defIsDefault == "true"
		createReq.IsDefault = &val
	}
	if cmd.Flags().Changed("max-steps") {
		createReq.MaxSteps = &defMaxSteps
	}
	if cmd.Flags().Changed("default-timeout") {
		createReq.DefaultTimeout = &defDefaultTimeout
	}

	result, err := c.SDK.AgentDefinitions.Create(context.Background(), createReq)
	if err != nil {
		return fmt.Errorf("failed to create agent definition: %w", err)
	}

	d := result.Data
	fmt.Println("Agent definition created successfully!")
	fmt.Printf("  ID:         %s\n", d.ID)
	fmt.Printf("  Name:       %s\n", d.Name)
	fmt.Printf("  Flow Type:  %s\n", d.FlowType)
	fmt.Printf("  Visibility: %s\n", d.Visibility)

	return nil
}

func runUpdateAgentDef(cmd *cobra.Command, args []string) error {
	defID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	updateReq := &agentdefinitions.UpdateAgentDefinitionRequest{}
	hasUpdate := false

	if cmd.Flags().Changed("name") {
		updateReq.Name = &defName
		hasUpdate = true
	}
	if cmd.Flags().Changed("description") {
		updateReq.Description = &defDescription
		hasUpdate = true
	}
	if cmd.Flags().Changed("system-prompt") {
		updateReq.SystemPrompt = &defSystemPrompt
		hasUpdate = true
	}
	if cmd.Flags().Changed("model") {
		updateReq.Model = &agentdefinitions.ModelConfig{
			Name: defModelName,
		}
		hasUpdate = true
	}
	if cmd.Flags().Changed("tools") {
		tools := strings.Split(defTools, ",")
		for i := range tools {
			tools[i] = strings.TrimSpace(tools[i])
		}
		updateReq.Tools = tools
		hasUpdate = true
	}
	if cmd.Flags().Changed("flow-type") {
		updateReq.FlowType = &defFlowType
		hasUpdate = true
	}
	if cmd.Flags().Changed("visibility") {
		updateReq.Visibility = &defVisibility
		hasUpdate = true
	}
	if cmd.Flags().Changed("is-default") {
		val := defIsDefault == "true"
		updateReq.IsDefault = &val
		hasUpdate = true
	}
	if cmd.Flags().Changed("max-steps") {
		updateReq.MaxSteps = &defMaxSteps
		hasUpdate = true
	}
	if cmd.Flags().Changed("default-timeout") {
		updateReq.DefaultTimeout = &defDefaultTimeout
		hasUpdate = true
	}

	if !hasUpdate {
		return fmt.Errorf("no update flags specified. Use --name, --system-prompt, --tools, --visibility, etc.")
	}

	result, err := c.SDK.AgentDefinitions.Update(context.Background(), defID, updateReq)
	if err != nil {
		return fmt.Errorf("failed to update agent definition: %w", err)
	}

	d := result.Data
	fmt.Println("Agent definition updated successfully!")
	fmt.Printf("  ID:         %s\n", d.ID)
	fmt.Printf("  Name:       %s\n", d.Name)
	fmt.Printf("  Visibility: %s\n", d.Visibility)

	return nil
}

func runDeleteAgentDef(cmd *cobra.Command, args []string) error {
	defID := args[0]

	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	err = c.SDK.AgentDefinitions.Delete(context.Background(), defID)
	if err != nil {
		return fmt.Errorf("failed to delete agent definition: %w", err)
	}

	fmt.Printf("Agent definition %s deleted successfully.\n", defID)
	return nil
}

func init() {
	// Create definition flags
	createAgentDefCmd.Flags().StringVar(&defName, "name", "", "Definition name (required)")
	createAgentDefCmd.Flags().StringVar(&defDescription, "description", "", "Description")
	createAgentDefCmd.Flags().StringVar(&defSystemPrompt, "system-prompt", "", "System prompt")
	createAgentDefCmd.Flags().StringVar(&defModelName, "model", "", "Model name (e.g., gemini-2.0-flash)")
	createAgentDefCmd.Flags().StringVar(&defTools, "tools", "", "Comma-separated tool names")
	createAgentDefCmd.Flags().StringVar(&defFlowType, "flow-type", "", "Flow type (single, multi, coordinator)")
	createAgentDefCmd.Flags().StringVar(&defVisibility, "visibility", "", "Visibility (external, project, internal)")
	createAgentDefCmd.Flags().StringVar(&defIsDefault, "is-default", "", "Set as default definition (true/false)")
	createAgentDefCmd.Flags().IntVar(&defMaxSteps, "max-steps", 0, "Maximum steps per run")
	createAgentDefCmd.Flags().IntVar(&defDefaultTimeout, "default-timeout", 0, "Default timeout in seconds")
	_ = createAgentDefCmd.MarkFlagRequired("name")

	// Update definition flags
	updateAgentDefCmd.Flags().StringVar(&defName, "name", "", "New name")
	updateAgentDefCmd.Flags().StringVar(&defDescription, "description", "", "New description")
	updateAgentDefCmd.Flags().StringVar(&defSystemPrompt, "system-prompt", "", "New system prompt")
	updateAgentDefCmd.Flags().StringVar(&defModelName, "model", "", "New model name")
	updateAgentDefCmd.Flags().StringVar(&defTools, "tools", "", "New comma-separated tool names")
	updateAgentDefCmd.Flags().StringVar(&defFlowType, "flow-type", "", "New flow type")
	updateAgentDefCmd.Flags().StringVar(&defVisibility, "visibility", "", "New visibility")
	updateAgentDefCmd.Flags().StringVar(&defIsDefault, "is-default", "", "Set as default (true/false)")
	updateAgentDefCmd.Flags().IntVar(&defMaxSteps, "max-steps", 0, "New max steps")
	updateAgentDefCmd.Flags().IntVar(&defDefaultTimeout, "default-timeout", 0, "New default timeout")

	// Register subcommands
	agentDefsCmd.AddCommand(listAgentDefsCmd)
	agentDefsCmd.AddCommand(getAgentDefCmd)
	agentDefsCmd.AddCommand(createAgentDefCmd)
	agentDefsCmd.AddCommand(updateAgentDefCmd)
	agentDefsCmd.AddCommand(deleteAgentDefCmd)
	rootCmd.AddCommand(agentDefsCmd)
}
