package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/agentdefinitions"
	"github.com/emergent-company/emergent.memory/tools/cli/internal/client"
	"github.com/spf13/cobra"
)

var agentDefsCmd = &cobra.Command{
	Use:     "agent-definitions",
	Aliases: []string{"agent-defs", "defs"},
	Short:   "Manage agent definitions",
	Long:    "Commands for managing agent definitions (system prompts, tools, model config, flow type, visibility)",
	GroupID: "ai",
}

var listAgentDefsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all agent definitions",
	Long: `List all agent definitions for the current project.

Prints a numbered list with each definition's Name, ID, FlowType, Visibility,
IsDefault flag, Tool count, and Description (if set).`,
	RunE: runListAgentDefs,
}

var getAgentDefCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get agent definition details",
	Long: `Get full details for a specific agent definition by ID.

Prints Name, ID, ProjectID, FlowType, Visibility, IsDefault, Description (if
set), System Prompt (truncated to 200 characters), Model configuration (Name,
Temperature, MaxTokens), Tools list, MaxSteps, DefaultTimeout, ACP Config
(DisplayName, Description, Capabilities), CreatedAt and UpdatedAt timestamps,
and any extra Config JSON.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runGetAgentDef,
}

var createAgentDefCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent definition",
	Long: `Create a new agent definition.

Examples:
  memory agent-definitions create --name "my-def" --system-prompt "You are a helpful agent"
  memory defs create --name "extractor" --flow-type single --tools "search,graph_query" --visibility project`,
	RunE: runCreateAgentDef,
}

var updateAgentDefCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an agent definition",
	Long:  "Update an existing agent definition (partial update)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runUpdateAgentDef,
}

var deleteAgentDefCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete an agent definition",
	Long:  "Delete an agent definition by ID",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDeleteAgentDef,
}

// Flags for agent definitions
var (
	defName           string
	defDescription    string
	defSystemPrompt   string
	defModelName      string
	defTools          string
	defSkills         string
	defFlowType       string
	defVisibility     string
	defIsDefault      string
	defMaxSteps       int
	defDefaultTimeout int
	defListLimit      int
	defListPage       int
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

	total := len(result.Data)
	data := paginate(result.Data, defListLimit, defListPage)

	if compact {
		for _, d := range data {
			fmt.Printf("%-40s  %s\n", d.Name, d.ID)
		}
		return nil
	}

	if h := paginationHeader(total, defListLimit, defListPage); h != "" {
		fmt.Printf("%s:\n\n", h)
	} else {
		fmt.Printf("Found %d agent definition(s):\n\n", total)
	}
	for i, d := range data {
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

// resolveAgentDefArgOrPick resolves an agent-definition ID from args[0], or,
// when args is empty and stdin is a terminal, lists definitions and shows an
// interactive picker. Returns the resolved definition ID.
func resolveAgentDefArgOrPick(cmd *cobra.Command, c *client.Client, args []string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}

	if isNonInteractive() {
		return "", fmt.Errorf("agent definition ID is required — pass an ID or run interactively to pick from a list")
	}

	result, err := c.SDK.AgentDefinitions.List(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to list agent definitions: %w", err)
	}
	defList := result.Data
	if len(defList) == 0 {
		return "", fmt.Errorf("no agent definitions found in the current project")
	}

	items := make([]PickerItem, len(defList))
	for i, d := range defList {
		label := d.Name + "  [" + d.FlowType + "]"
		if d.IsDefault {
			label += " (default)"
		}
		items[i] = PickerItem{ID: d.ID, Name: label}
	}

	id, _, err := promptResourcePicker("Select an agent definition", items)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("agent definition ID is required")
	}
	return id, nil
}

func runGetAgentDef(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	defID, err := resolveAgentDefArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

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
	if len(d.Skills) > 0 {
		fmt.Printf("  Skills:          %s\n", strings.Join(d.Skills, ", "))
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
	if defSkills != "" {
		createReq.Skills = strings.Split(defSkills, ",")
		for i := range createReq.Skills {
			createReq.Skills[i] = strings.TrimSpace(createReq.Skills[i])
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
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	defID, err := resolveAgentDefArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

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
	if cmd.Flags().Changed("skills") {
		skillList := strings.Split(defSkills, ",")
		for i := range skillList {
			skillList[i] = strings.TrimSpace(skillList[i])
		}
		updateReq.Skills = skillList
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
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	defID, err := resolveAgentDefArgOrPick(cmd, c, args)
	if err != nil {
		return err
	}

	err = c.SDK.AgentDefinitions.Delete(context.Background(), defID)
	if err != nil {
		return fmt.Errorf("failed to delete agent definition: %w", err)
	}

	fmt.Printf("Agent definition %s deleted successfully.\n", defID)
	return nil
}

// --- Agent Override Commands ---

// Override flags (separate from definition flags to avoid conflicts)
var (
	overrideModel            string
	overrideTemperature      float32
	overrideMaxSteps         int
	overrideTools            string
	overrideSystemPrompt     string
	overrideSystemPromptFile string
	overrideClear            bool
	overrideSandboxEnabled   string // "true", "false", or "" (not set)
)

var overrideAgentCmd = &cobra.Command{
	Use:   "override [agentName]",
	Short: "View or set per-project agent overrides",
	Long: `View or set per-project configuration overrides for an agent definition.

Without flags, shows the current override for the agent. With flags, sets
or updates the override. Overrides are merged on top of canonical defaults
each time the agent runs — non-overridden fields always get the latest code defaults.

Examples:
  memory defs override graph-query-agent                          # view current override
  memory defs override graph-query-agent --model gemini-2.5-pro   # override model
  memory defs override cli-assistant-agent --max-steps 30         # override max steps
  memory defs override graph-query-agent --model gemini-2.5-pro --temperature 0.2 --max-steps 20
  memory defs override graph-query-agent --system-prompt-file prompt.txt
  memory defs override graph-query-agent --sandbox-enabled false  # disable sandbox
  memory defs override graph-query-agent --clear                  # remove override`,
	Args: cobra.MaximumNArgs(1),
	RunE: runOverrideAgent,
}

var listOverridesCmd = &cobra.Command{
	Use:   "overrides",
	Short: "List all agent overrides for the project",
	Long:  "List all per-project agent configuration overrides.",
	RunE:  runListOverrides,
}

func runOverrideAgent(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	// Resolve agent name from args or interactive picker of known internal agents.
	agentName := ""
	if len(args) > 0 && args[0] != "" {
		agentName = args[0]
	}
	if agentName == "" {
		if isNonInteractive() {
			return fmt.Errorf("agent name is required — pass as argument (e.g., graph-query-agent)")
		}
		items := []PickerItem{
			{ID: "graph-query-agent", Name: "graph-query-agent"},
			{ID: "cli-assistant-agent", Name: "cli-assistant-agent"},
		}
		_, agentName, err = promptResourcePicker("Select an agent to override", items)
		if err != nil {
			return err
		}
		if agentName == "" {
			return fmt.Errorf("agent name is required")
		}
	}

	// Handle --clear
	if overrideClear {
		err := c.SDK.AgentDefinitions.DeleteOverride(context.Background(), agentName)
		if err != nil {
			return fmt.Errorf("failed to delete override: %w", err)
		}
		fmt.Printf("Override for %s removed — agent will use canonical defaults.\n", agentName)
		return nil
	}

	// Check if any override flags were set.
	hasOverrideFlag := cmd.Flags().Changed("model") ||
		cmd.Flags().Changed("temperature") ||
		cmd.Flags().Changed("max-steps") ||
		cmd.Flags().Changed("tools") ||
		cmd.Flags().Changed("system-prompt") ||
		cmd.Flags().Changed("system-prompt-file") ||
		cmd.Flags().Changed("sandbox-enabled")

	if !hasOverrideFlag {
		// View mode: show current override
		result, err := c.SDK.AgentDefinitions.GetOverride(context.Background(), agentName)
		if err != nil {
			// 404 means no override — show a helpful message
			fmt.Printf("No override set for %s — agent uses canonical defaults.\n", agentName)
			fmt.Printf("\nSet an override with:\n")
			fmt.Printf("  memory defs override %s --model gemini-2.5-pro\n", agentName)
			return nil
		}

		o := result.Data
		fmt.Printf("Override for %s:\n", agentName)
		if o.SystemPrompt != nil {
			prompt := *o.SystemPrompt
			if len(prompt) > 200 {
				prompt = prompt[:200] + "..."
			}
			fmt.Printf("  System Prompt: %s\n", prompt)
		}
		if o.Model != nil {
			if o.Model.Name != "" {
				fmt.Printf("  Model:         %s\n", o.Model.Name)
			}
			if o.Model.Temperature != nil {
				fmt.Printf("  Temperature:   %.2f\n", *o.Model.Temperature)
			}
		}
		if o.Tools != nil {
			fmt.Printf("  Tools:         %s\n", strings.Join(o.Tools, ", "))
		}
		if o.MaxSteps != nil {
			fmt.Printf("  Max Steps:     %d\n", *o.MaxSteps)
		}
		if o.SandboxConfig != nil {
			cfgJSON, _ := json.MarshalIndent(o.SandboxConfig, "  ", "  ")
			fmt.Printf("  Sandbox:       %s\n", string(cfgJSON))
		}
		return nil
	}

	// Set mode: build override from flags
	override := &agentdefinitions.AgentOverride{}

	if cmd.Flags().Changed("model") || cmd.Flags().Changed("temperature") {
		override.Model = &agentdefinitions.ModelConfig{}
		if cmd.Flags().Changed("model") {
			override.Model.Name = overrideModel
		}
		if cmd.Flags().Changed("temperature") {
			override.Model.Temperature = &overrideTemperature
		}
	}
	if cmd.Flags().Changed("max-steps") {
		override.MaxSteps = &overrideMaxSteps
	}
	if cmd.Flags().Changed("tools") {
		tools := strings.Split(overrideTools, ",")
		for i := range tools {
			tools[i] = strings.TrimSpace(tools[i])
		}
		override.Tools = tools
	}
	if cmd.Flags().Changed("system-prompt") {
		override.SystemPrompt = &overrideSystemPrompt
	}
	if cmd.Flags().Changed("system-prompt-file") {
		data, err := os.ReadFile(overrideSystemPromptFile)
		if err != nil {
			return fmt.Errorf("failed to read system prompt file: %w", err)
		}
		prompt := string(data)
		override.SystemPrompt = &prompt
	}
	if cmd.Flags().Changed("sandbox-enabled") {
		switch strings.ToLower(overrideSandboxEnabled) {
		case "true":
			override.SandboxConfig = map[string]any{"enabled": true}
		case "false":
			override.SandboxConfig = map[string]any{"enabled": false}
		default:
			return fmt.Errorf("--sandbox-enabled must be 'true' or 'false', got %q", overrideSandboxEnabled)
		}
	}

	_, err = c.SDK.AgentDefinitions.SetOverride(context.Background(), agentName, override)
	if err != nil {
		return fmt.Errorf("failed to set override: %w", err)
	}

	fmt.Printf("Override set for %s:\n", agentName)
	if override.Model != nil {
		if override.Model.Name != "" {
			fmt.Printf("  Model:       %s\n", override.Model.Name)
		}
		if override.Model.Temperature != nil {
			fmt.Printf("  Temperature: %.2f\n", *override.Model.Temperature)
		}
	}
	if override.MaxSteps != nil {
		fmt.Printf("  Max Steps:   %d\n", *override.MaxSteps)
	}
	if override.Tools != nil {
		fmt.Printf("  Tools:       %s\n", strings.Join(override.Tools, ", "))
	}
	if override.SystemPrompt != nil {
		prompt := *override.SystemPrompt
		if len(prompt) > 100 {
			prompt = prompt[:100] + "..."
		}
		fmt.Printf("  Prompt:      %s\n", prompt)
	}
	if override.SandboxConfig != nil {
		if enabled, ok := override.SandboxConfig["enabled"]; ok {
			fmt.Printf("  Sandbox:     enabled=%v\n", enabled)
		}
	}
	fmt.Printf("\nThe next ask/query call will use these overrides.\n")
	return nil
}

func runListOverrides(cmd *cobra.Command, args []string) error {
	projectID, err := resolveProjectContext(cmd, "")
	if err != nil {
		return err
	}

	c, err := getClient(cmd)
	if err != nil {
		return err
	}
	c.SetContext("", projectID)

	result, err := c.SDK.AgentDefinitions.ListOverrides(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list overrides: %w", err)
	}

	entries := result.Data
	if len(entries) == 0 {
		fmt.Println("No agent overrides set for this project.")
		fmt.Println("\nSet one with:")
		fmt.Println("  memory defs override graph-query-agent --model gemini-2.5-pro")
		return nil
	}

	fmt.Printf("Found %d agent override(s):\n\n", len(entries))
	for i, entry := range entries {
		fmt.Printf("%d. %s\n", i+1, entry.AgentName)
		overrideJSON, _ := json.MarshalIndent(entry.Override, "   ", "  ")
		fmt.Printf("   %s\n\n", string(overrideJSON))
	}
	return nil
}

func init() {
	// List pagination flags
	listAgentDefsCmd.Flags().IntVar(&defListLimit, "limit", 0, "Maximum number of definitions to show (0 = all)")
	listAgentDefsCmd.Flags().IntVar(&defListPage, "page", 1, "Page number (1-based, used with --limit)")

	// Create definition flags
	createAgentDefCmd.Flags().StringVar(&defName, "name", "", "Definition name (required)")
	createAgentDefCmd.Flags().StringVar(&defDescription, "description", "", "Description")
	createAgentDefCmd.Flags().StringVar(&defSystemPrompt, "system-prompt", "", "System prompt")
	createAgentDefCmd.Flags().StringVar(&defModelName, "model", "", "Model name (e.g., gemini-2.0-flash)")
	createAgentDefCmd.Flags().StringVar(&defTools, "tools", "", "Comma-separated tool names")
	createAgentDefCmd.Flags().StringVar(&defSkills, "skills", "", "Comma-separated skill names (e.g. \"code-review,*\")")
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
	updateAgentDefCmd.Flags().StringVar(&defSkills, "skills", "", "New comma-separated skill names")
	updateAgentDefCmd.Flags().StringVar(&defFlowType, "flow-type", "", "New flow type")
	updateAgentDefCmd.Flags().StringVar(&defVisibility, "visibility", "", "New visibility")
	updateAgentDefCmd.Flags().StringVar(&defIsDefault, "is-default", "", "Set as default (true/false)")
	updateAgentDefCmd.Flags().IntVar(&defMaxSteps, "max-steps", 0, "New max steps")
	updateAgentDefCmd.Flags().IntVar(&defDefaultTimeout, "default-timeout", 0, "New default timeout")

	// Override flags
	overrideAgentCmd.Flags().StringVar(&overrideModel, "model", "", "Override model name (e.g., gemini-2.5-pro)")
	overrideAgentCmd.Flags().Float32Var(&overrideTemperature, "temperature", -1, "Override temperature (0.0-2.0)")
	overrideAgentCmd.Flags().IntVar(&overrideMaxSteps, "max-steps", 0, "Override max steps")
	overrideAgentCmd.Flags().StringVar(&overrideTools, "tools", "", "Override tools (comma-separated)")
	overrideAgentCmd.Flags().StringVar(&overrideSystemPrompt, "system-prompt", "", "Override system prompt")
	overrideAgentCmd.Flags().StringVar(&overrideSystemPromptFile, "system-prompt-file", "", "Read system prompt from file")
	overrideAgentCmd.Flags().BoolVar(&overrideClear, "clear", false, "Remove override — revert to canonical defaults")
	overrideAgentCmd.Flags().StringVar(&overrideSandboxEnabled, "sandbox-enabled", "", "Override sandbox enabled state (true/false)")

	// Register subcommands
	agentDefsCmd.AddCommand(listAgentDefsCmd)
	agentDefsCmd.AddCommand(getAgentDefCmd)
	agentDefsCmd.AddCommand(createAgentDefCmd)
	agentDefsCmd.AddCommand(updateAgentDefCmd)
	agentDefsCmd.AddCommand(deleteAgentDefCmd)
	agentDefsCmd.AddCommand(overrideAgentCmd)
	agentDefsCmd.AddCommand(listOverridesCmd)
	rootCmd.AddCommand(agentDefsCmd)
}
